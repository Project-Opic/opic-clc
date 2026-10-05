// clc_snapshot.go —— AGG-CLC-05 版本快照行为实现(m2 FUNC-17/18/24)。
//
// 事务口径:序号单调递增(INV-24) → 内容/引用快照落库 → 只增不改(INV-25);
// 回滚:R-06 目标快照引用资源现存性校验,缺失即拒绝并列出;显式「放弃引用回滚」引用置空并留痕。
package service

import (
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/store"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// ListSnapshots FUNC-24 快照清单查询(REP-06)。
func ListSnapshots(page, size int, p map[string]any) (*Page, error) {
	where := "WHERE s.flag = 1"
	var args []any
	if v := Str(p["cluster_ref"]); v != "" {
		where += " AND s.cluster_ref = ?"
		args = append(args, v)
	}
	if v := Str(p["snapshot_type"]); v != "" {
		where += " AND s.snapshot_type = ?"
		args = append(args, v)
	}
	if kw := Str(p["keyword"]); kw != "" {
		where += " AND (s.snapshot_no LIKE ? OR s.change_summary LIKE ?)"
		like := "%" + kw + "%"
		args = append(args, like, like)
	}
	total, err := qCount(`SELECT COUNT(*) FROM clc_snapshot s `+where, args...)
	if err != nil {
		return nil, err
	}
	rows, err := qList(`
		SELECT s.*, c.name AS cluster_name
		FROM clc_snapshot s
		LEFT JOIN clc_cluster c ON c.cluster_no = s.cluster_ref AND c.flag = 1
		`+where+` ORDER BY s.cluster_ref ASC, s.revision_no DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	return &Page{List: rows, Total: total, PageV: page, Size: size}, nil
}

// snapshotContent 集群当前内容快照(登记字段 JSON;事务内调用)。
func snapshotContent(db *gorm.DB, clusterNo string) (string, error) {
	row, err := store.One(db, `SELECT cluster_no, name, description, cluster_dns_domain, version_ref, executor_ref,
		registry_ref, registry_project_ref, run_mode, status, health, master_endpoints
		FROM clc_cluster WHERE cluster_no=? AND flag=1`, clusterNo)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "{}", nil
	}
	return jsonMarshal(row), nil
}

// snapshotBindings 引用快照(事务内调用)。
func snapshotBindings(db *gorm.DB, clusterNo string) (string, error) {
	rows, err := store.List(db, `SELECT target_capability, target_kind, target_ref, purpose, binding_source
		FROM clc_binding WHERE cluster_ref=? AND flag=1 ORDER BY id`, clusterNo)
	if err != nil {
		return "", err
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	return jsonMarshal(rows), nil
}

// createSnapshotTx 快照创建(事务内;INV-24 序号单调递增)。
func createSnapshotTx(db *gorm.DB, clusterNo, summary, snapshotType string, user UserInfo) error {
	row, err := store.One(db, `SELECT COALESCE(MAX(revision_no),0) AS mx FROM clc_snapshot WHERE cluster_ref=? AND flag=1`, clusterNo)
	if err != nil {
		return err
	}
	rev := Int(row["mx"]) + 1
	content, err := snapshotContent(db, clusterNo)
	if err != nil {
		return err
	}
	bindings, err := snapshotBindings(db, clusterNo)
	if err != nil {
		return err
	}
	no, err := genBizNo(db, NoPrefixSnapshot, "clc_snapshot", "snapshot_no")
	if err != nil {
		return err
	}
	_, err = store.Exec(db, `
		INSERT INTO clc_snapshot (snapshot_no, cluster_ref, revision_no, change_summary, content_snapshot, binding_snapshot, snapshot_type, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		no, clusterNo, rev, summary, content, bindings, snapshotType, user.ID, user.ID)
	return err
}

// CreateSnapshot FUNC-17 创建版本快照(人工触发;INV-24/25)。
func CreateSnapshot(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	c, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	summary := Str(d["change_summary"])
	if summary == "" {
		summary = "人工快照"
	}
	sType := Str(d["snapshot_type"])
	if sType == "" {
		sType = SnapshotTypeChange
	}
	if sType != SnapshotTypeChange && sType != SnapshotTypeAutoDerived && sType != SnapshotTypeRollback {
		return nil, bizRuleFail("INV-25", "快照类型只能取 CHANGE/AUTO_DERIVED/ROLLBACK")
	}
	err = tx(func(db *gorm.DB) error {
		return createSnapshotTx(db, Str(c["cluster_no"]), summary, sType, user)
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-17", fmt.Sprintf("集群 %s 创建版本快照:%s", Str(c["cluster_no"]), summary))
	return GetCluster(clusterID)
}

// RollbackSnapshot FUNC-18 版本回滚(R-06 现存性校验;INV-25 回滚产生新快照)。
func RollbackSnapshot(id int64, d map[string]any, user UserInfo) (map[string]any, error) {
	snap, err := qOne(`SELECT * FROM clc_snapshot WHERE id = ? AND flag = 1`, id)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return nil, NotFound("快照")
	}
	clusterNo := Str(snap["cluster_ref"])
	abandon := truthy(d["abandon_refs"])
	// R-06:目标快照引用的资源现存性校验 —— 快照引用 vs 当前台账
	var bindings []map[string]any
	if err := json.Unmarshal([]byte(Str(snap["binding_snapshot"])), &bindings); err != nil {
		return nil, Fail(errcode.ErrParam, "快照引用数据损坏,无法回滚:%v", err)
	}
	missing := []string{}
	for _, b := range bindings {
		ref := fmt.Sprintf("%v", b["target_ref"])
		if ref == "" || ref == "<nil>" {
			continue
		}
		cur, err := qOne(`SELECT id FROM clc_binding WHERE cluster_ref=? AND flag=1 AND LOWER(target_ref)=LOWER(?)`, clusterNo, ref)
		if err != nil {
			return nil, err
		}
		if cur == nil {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 && !abandon {
		return nil, bizRuleFail("R-06", "目标快照引用的 %d 项资源当前不存在(%s):回滚被拒绝;请先恢复资源,或显式选择「放弃引用回滚」(引用置空、留痕)",
			len(missing), fmt.Sprintf("%v", missing))
	}
	err = tx(func(db *gorm.DB) error {
		// 内容回滚:快照登记字段写回集群
		var content map[string]any
		if err := json.Unmarshal([]byte(Str(snap["content_snapshot"])), &content); err != nil {
			return Fail(errcode.ErrParam, "快照内容数据损坏,无法回滚:%v", err)
		}
		if _, err := store.Exec(db, `
			UPDATE clc_cluster SET version_ref=?, executor_ref=?, registry_ref=?, registry_project_ref=?, updated_by=?,
				updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE cluster_no=? AND flag=1`,
			content["version_ref"], content["executor_ref"], content["registry_ref"], content["registry_project_ref"],
			user.ID, clusterNo); err != nil {
			return err
		}
		if abandon {
			// 显式放弃引用回滚:引用置空并留痕(INV-26/R-06)
			if _, err := store.Exec(db, `UPDATE clc_binding SET flag=0, reconcile_note='回滚放弃引用:目标快照引用资源已不存在',
				updated_by=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
				WHERE cluster_ref=? AND flag=1 AND target_ref = ANY(?)`,
				user.ID, clusterNo, pqArray(missing)); err != nil {
				return err
			}
		}
		if err := deriveParamsTx(db, clusterNo, inlineRefsOf(db, clusterNo), user); err != nil {
			return err
		}
		summary := fmt.Sprintf("回滚到快照 %s(序号 %d)", Str(snap["snapshot_no"]), Int(snap["revision_no"]))
		if abandon {
			summary += ";放弃引用回滚(引用置空留痕)"
		}
		// INV-25:回滚产生新快照、不覆盖旧快照
		return createSnapshotTx(db, clusterNo, summary, SnapshotTypeRollback, user)
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-18", fmt.Sprintf("集群 %s 回滚到快照 %s(缺失引用 %d 项,放弃=%v)",
		clusterNo, Str(snap["snapshot_no"]), len(missing), abandon))
	return GetClusterByNo(clusterNo)
}

// pqArray 字符串数组 → pg 数组字面量(空集合返回空数组,勿拼 IN ())。
func pqArray(items []string) string {
	out := "{"
	for i, s := range items {
		if i > 0 {
			out += ","
		}
		out += `"` + s + `"`
	}
	return out + "}"
}
