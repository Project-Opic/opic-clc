// clc_probe.go —— AGG-CLC-07 探活记录行为实现(m2 FUNC-26/27)。
//
// 纪律(INV-36):探活记录只读探测、探测过程不得向集群写入任何内容;
// 健康写回由 R-08 双源口径独立承载(连续失败 ⇒ UNREACHABLE;连续成功 ⇒ HEALTHY;不覆盖 DEGRADED)。
package service

import (
	"fmt"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/store"
)

// ListProbes FUNC-26 探活历史查询(REP-08)。
func ListProbes(page, size int, p map[string]any) (*Page, error) {
	where := "WHERE p.flag = 1"
	var args []any
	if v := Str(p["cluster_ref"]); v != "" {
		where += " AND p.cluster_ref = ?"
		args = append(args, v)
	}
	if v := Str(p["probe_result"]); v != "" {
		where += " AND p.probe_result = ?"
		args = append(args, v)
	}
	if v := Str(p["from"]); v != "" {
		where += " AND p.probed_at >= ?"
		args = append(args, v)
	}
	if v := Str(p["to"]); v != "" {
		where += " AND p.probed_at <= ?"
		args = append(args, v)
	}
	if kw := Str(p["keyword"]); kw != "" {
		where += " AND (p.probe_no LIKE ?)"
		args = append(args, "%"+kw+"%")
	}
	total, err := qCount(`SELECT COUNT(*) FROM clc_probe p `+where, args...)
	if err != nil {
		return nil, err
	}
	rows, err := qList(`
		SELECT p.*, c.name AS cluster_name
		FROM clc_probe p
		LEFT JOIN clc_cluster c ON c.cluster_no = p.cluster_ref AND c.flag = 1
		`+where+` ORDER BY p.id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	return &Page{List: rows, Total: total, PageV: page, Size: size}, nil
}

// AggregateHealth FUNC-27 聚合健康查询(REP-09)。
// 集群自身健康|交付执行机状态|基础设施证书剩余天数|镜像仓库状态|节点资产健康。
// 跨域权威在他域(O-IGN/O-CRT/O-REG/O-AST):本域落出口径=引用台账登记 + 集群健康,缓存 REF-07。
func AggregateHealth(p map[string]any) ([]map[string]any, error) {
	where := "WHERE c.flag = 1"
	var args []any
	if v := Str(p["cluster_ref"]); v != "" {
		where += " AND c.cluster_no = ?"
		args = append(args, v)
	}
	if v := Str(p["status"]); v != "" {
		where += " AND c.status = ?"
		args = append(args, v)
	}
	rows, err := qList(`
		SELECT c.cluster_no, c.name AS cluster_name, c.status, c.health, c.degraded_reason, c.last_probe_at,
			COALESCE((SELECT b.target_ref FROM clc_binding b WHERE b.cluster_ref=c.cluster_no AND b.flag=1
				AND b.target_capability='IGNITION' AND b.target_kind='EXECUTOR' LIMIT 1),'-') AS executor_ref,
			COALESCE((SELECT COUNT(*) FROM clc_binding b WHERE b.cluster_ref=c.cluster_no AND b.flag=1
				AND b.target_capability='CERT'),0) AS cert_count,
			COALESCE((SELECT b.target_ref FROM clc_binding b WHERE b.cluster_ref=c.cluster_no AND b.flag=1
				AND b.target_capability='REGISTRY' AND b.target_kind='INSTANCE' LIMIT 1),'-') AS registry_ref,
			COALESCE((SELECT COUNT(*) FROM clc_cluster_node n WHERE n.cluster_ref=c.cluster_no AND n.flag=1
				AND n.node_status='ADDED' AND n.node_health='HEALTHY'),0) AS healthy_nodes,
			COALESCE((SELECT COUNT(*) FROM clc_cluster_node n WHERE n.cluster_ref=c.cluster_no AND n.flag=1
				AND n.node_status='ADDED'),0) AS total_nodes
		FROM clc_cluster c `+where+` ORDER BY c.cluster_no ASC LIMIT 100`,
		args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// RunProbe 人工探活登记(MANUAL 触发;INV-34 校验;R-08 健康写回)。
// INV-36:探测本身只读 —— 集群行的唯一写入是 R-08 的健康结论与最近探活时间(写权限由 R-08 承载)。
func RunProbe(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	c, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	clusterNo := Str(c["cluster_no"])
	apiserverOK := truthy(d["apiserver_reachable"])
	tlsOK := truthy(d["tls_handshake_ok"])
	ready, total := Int(d["ready_nodes"]), Int(d["total_nodes"])
	if total == 0 {
		// 缺省口径:总节点数取当前 ADDED 节点数
		total, _ = qCount(`SELECT COUNT(*) FROM clc_cluster_node WHERE cluster_ref=? AND flag=1 AND node_status=?`, clusterNo, NodeStatusAdded)
		ready = total
	}
	result := ProbeResultHealthy
	if !apiserverOK || !tlsOK || ready < total {
		result = ProbeResultFailed
	}
	failReason := Str(d["fail_reason"])
	if err := checkProbeInput(result, failReason, ready, total); err != nil {
		return nil, err
	}
	err = tx(func(db *gorm.DB) error {
		no, err := genBizNo(db, NoPrefixProbe, "clc_probe", "probe_no")
		if err != nil {
			return err
		}
		if _, err := store.Exec(db, `
			INSERT INTO clc_probe (probe_no, cluster_ref, probed_at, trigger_source, apiserver_reachable, tls_handshake_ok,
				ready_nodes, total_nodes, probe_result, fail_reason, created_by, updated_by)
			VALUES (?, ?, to_char(now(),'YYYY-MM-DD HH24:MI:SS'), ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			no, clusterNo, ProbeTriggerManual, boolSmallInt(apiserverOK), boolSmallInt(tlsOK),
			ready, total, result, failReason, user.ID, user.ID); err != nil {
			return err
		}
		// R-08 防抖窗口:连续成功/失败计数(阈值 REF-03/REF-11)
		okStreak, failStreak, err := probeStreaks(db, clusterNo)
		if err != nil {
			return err
		}
		newHealth, changed := healthTransitionByProbe(Str(c["health"]), okStreak, failStreak)
		if changed || result == ProbeResultHealthy || result == ProbeResultFailed {
			if _, err := store.Exec(db, `
				UPDATE clc_cluster SET health=?, last_probe_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS'),
					updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE cluster_no=? AND flag=1`,
				newHealth, clusterNo); err != nil {
				return err
			}
		} else {
			if _, err := store.Exec(db, `
				UPDATE clc_cluster SET last_probe_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE cluster_no=? AND flag=1`, clusterNo); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-26", fmt.Sprintf("集群 %s 人工探活:结论 %s(控制面=%v TLS=%v 就绪 %d/%d)",
		clusterNo, result, apiserverOK, tlsOK, ready, total))
	return GetCluster(clusterID)
}

// probeStreaks R-08 防抖计数:从最近一条记录倒序数连续成功/失败(事务内调用)。
func probeStreaks(db *gorm.DB, clusterNo string) (okStreak, failStreak int, err error) {
	rows, err := store.List(db, `SELECT probe_result FROM clc_probe WHERE cluster_ref=? AND flag=1 ORDER BY id DESC LIMIT 50`, clusterNo)
	if err != nil {
		return 0, 0, err
	}
	if len(rows) == 0 {
		return 0, 0, nil
	}
	last := Str(rows[0]["probe_result"])
	for _, r := range rows {
		if Str(r["probe_result"]) != last {
			break
		}
		if last == ProbeResultHealthy {
			okStreak++
		} else {
			failStreak++
		}
	}
	return okStreak, failStreak, nil
}
