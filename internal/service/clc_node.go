// clc_node.go —— AGG-CLC-02 集群节点行为实现(m2 FUNC-06/07/08/21)。
//
// 事务口径:前置校验 → 规则校验(INV-10~15/R-02/R-03/R-12) → 状态变更 → 任务留痕 → 审计/事件。
// 节点登记是集群交付动作的落点:添加/移除均创建 ADD_NODE/DEL_NODE 任务(INV-15 PENDING 不得交付)。
package service

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/store"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// ListNodes FUNC-21 节点清单查询(REP-03)。
func ListNodes(page, size int, p map[string]any) (*Page, error) {
	where := "WHERE n.flag = 1"
	var args []any
	if v := Str(p["cluster_ref"]); v != "" {
		where += " AND n.cluster_ref = ?"
		args = append(args, v)
	}
	if v := Str(p["node_role"]); v != "" {
		where += " AND n.node_role = ?"
		args = append(args, v)
	}
	if v := Str(p["node_status"]); v != "" {
		where += " AND n.node_status = ?"
		args = append(args, v)
	}
	if v := Str(p["node_health"]); v != "" {
		where += " AND n.node_health = ?"
		args = append(args, v)
	}
	if kw := Str(p["keyword"]); kw != "" {
		where += " AND (n.node_no LIKE ? OR n.ip_address LIKE ? OR n.k8s_nodename LIKE ?)"
		like := "%" + kw + "%"
		args = append(args, like, like, like)
	}
	total, err := qCount(`SELECT COUNT(*) FROM clc_cluster_node n `+where, args...)
	if err != nil {
		return nil, err
	}
	rows, err := qList(`
		SELECT n.*, c.name AS cluster_name
		FROM clc_cluster_node n
		LEFT JOIN clc_cluster c ON c.cluster_no = n.cluster_ref AND c.flag = 1
		`+where+` ORDER BY n.id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	return &Page{List: rows, Total: total, PageV: page, Size: size}, nil
}

// createNodesTx 四步向导第二步节点登记(事务内;空集合直接返回,勿拼 IN ())。
func createNodesTx(db *gorm.DB, clusterNo string, raw any, user UserInfo) error {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		d := map[string]any{
			"ip_address": Str(m["ip_address"]), "node_role": Str(m["node_role"]),
			"asset_ref": Str(m["asset_ref"]), "remark": Str(m["remark"]),
		}
		if err := addNodeTx(db, clusterNo, d, user); err != nil {
			return fmt.Errorf("第 %d 个节点:%w", i+1, err)
		}
	}
	return nil
}

// addNodeTx 添加节点事务体(INV-10/11/R-02/R-12;登记即反查资产占用于其他活跃集群)。
func addNodeTx(db *gorm.DB, clusterNo string, d map[string]any, user UserInfo) error {
	if err := checkNodeInput(d); err != nil {
		return err
	}
	ip, role := strings.TrimSpace(Str(d["ip_address"])), Str(d["node_role"])
	assetRef := Str(d["asset_ref"])
	// INV-10:同一集群内 IP 唯一
	if dup, err := store.One(db, `SELECT id FROM clc_cluster_node WHERE cluster_ref=? AND flag=1 AND LOWER(ip_address)=LOWER(?)`, clusterNo, ip); err != nil {
		return err
	} else if dup != nil {
		return bizRuleFail("INV-10", "该集群内已存在 IP 「%s」的节点(IP 集群内唯一)", ip)
	}
	// R-02 资产占用拦截:一台资产同时只服务一个活跃集群(读其他集群的占用)
	if assetRef != "" {
		occupied, err := store.List(db, `
			SELECT DISTINCT n.cluster_ref, c.name FROM clc_cluster_node n
			JOIN clc_cluster c ON c.cluster_no = n.cluster_ref AND c.flag = 1 AND c.status <> ?
			WHERE n.flag=1 AND n.asset_ref=? AND n.node_status IN (?, ?) AND n.cluster_ref <> ?`,
			ClusterStatusDestroyed, assetRef, NodeStatusPending, NodeStatusAdded, clusterNo)
		if err != nil {
			return err
		}
		if len(occupied) > 0 {
			names := make([]string, 0, len(occupied))
			for _, o := range occupied {
				names = append(names, fmt.Sprintf("%s(%s)", Str(o["cluster_ref"]), Str(o["name"])))
			}
			return bizRuleFail("R-02", "资产 %s 已被 %d 个活跃集群占用(%s):一台资产同时只服务一个活跃集群,请先移除或更换资产", assetRef, len(occupied), strings.Join(names, "、"))
		}
	}
	no, err := genBizNo(db, NoPrefixNode, "clc_cluster_node", "node_no")
	if err != nil {
		return err
	}
	// 裸机模式(INV-12):资产引用置空时 IP 与登记值保留不清洗
	if _, err := store.Exec(db, `
		INSERT INTO clc_cluster_node (node_no, cluster_ref, asset_ref, node_role, ip_address, node_status, node_health, remark, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		no, clusterNo, assetRef, role, ip, NodeStatusPending, NodeHealthUnknown, Str(d["remark"]), user.ID, user.ID); err != nil {
		return err
	}
	return nil
}

// AddNode FUNC-06 添加节点(登记 PENDING + 创建 ADD_NODE 任务排队;INV-14 K8s 节点名在成功落点回填)。
func AddNode(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	c, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	clusterNo := Str(c["cluster_no"])
	if Str(c["status"]) == ClusterStatusDestroyed {
		return nil, bizRuleFail("INV-09", "集群已销毁,不可添加节点")
	}
	ip := strings.TrimSpace(Str(d["ip_address"]))
	execPath := Str(d["execution_path"])
	if execPath == "" {
		execPath = execPathOf(c) // O-IGN 侧 Q-71:加节点同样按集群登记事实推导链路
	}
	var created map[string]any
	var taskNo string
	err = tx(func(db *gorm.DB) error {
		if err := addNodeTx(db, clusterNo, d, user); err != nil {
			return err
		}
		row, err := store.One(db, `SELECT * FROM clc_cluster_node WHERE cluster_ref=? AND LOWER(ip_address)=LOWER(?) AND flag=1 ORDER BY id DESC LIMIT 1`,
			clusterNo, ip)
		if err != nil {
			return err
		}
		created = row
		// FLOW-PROC-02 A07:ADD_NODE 任务落 QUEUED,加入动作由交付内核执行(FLOW-PROC-06)
		task, err := createTaskTx(db, clusterNo, TaskKindAddNode, execPath, StepModeAuto, "", user)
		if err != nil {
			return err
		}
		taskNo = Str(task["task_no"])
		return nil
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-06", fmt.Sprintf("集群 %s 登记节点 IP %s(角色 %s),ADD_NODE 任务已排队(%s 链路)",
		clusterNo, ip, Str(d["node_role"]), execPath))
	enqueueTask(taskNo, DeliveryArgs{NodeIPs: []string{ip}, NodeName: strings.TrimSpace(Str(d["k8s_nodename"]))})
	return GetNode(Int(created["id"]))
}

// GetNode 节点详情。
func GetNode(id int64) (map[string]any, error) {
	row, err := qOne(`SELECT * FROM clc_cluster_node WHERE id = ? AND flag = 1`, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, NotFound("节点")
	}
	return row, nil
}

// RemoveNode FUNC-07 移除节点(置 REMOVING + DEL_NODE 任务排队;R-03 资产下架口径:引用置空由他域事件承载;
// 移除即终局 INV-13,终局行在成功落点写入)。
func RemoveNode(id int64, d map[string]any, user UserInfo) (map[string]any, error) {
	n, err := GetNode(id)
	if err != nil {
		return nil, err
	}
	if Str(n["node_status"]) == NodeStatusRemoved {
		return nil, bizRuleFail("INV-13", "REMOVED 为节点终局状态;重新加入请登记新行")
	}
	if Str(n["node_status"]) == NodeStatusRemoving {
		return nil, Fail(errcode.ErrState, "节点正在移除中")
	}
	clusterNo := Str(n["cluster_ref"])
	ip := Str(n["ip_address"])
	execPath := Str(d["execution_path"])
	if execPath == "" {
		execPath = execPathByClusterNo(clusterNo) // O-IGN 侧 Q-71:减节点同样按集群登记事实推导链路
	}
	var taskNo string
	err = tx(func(db *gorm.DB) error {
		if _, err := store.Exec(db, `UPDATE clc_cluster_node SET node_status=?, updated_by=?,
			updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
			NodeStatusRemoving, user.ID, id); err != nil {
			return err
		}
		// FLOW-PROC-02 A06/A09:DEL_NODE 任务落 QUEUED,摘除动作由交付内核执行(FLOW-PROC-06)
		task, err := createTaskTx(db, clusterNo, TaskKindDelNode, execPath, StepModeAuto, "", user)
		if err != nil {
			return err
		}
		taskNo = Str(task["task_no"])
		return nil
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-07", fmt.Sprintf("集群 %s 移除节点 %s(%s):已置 REMOVING,DEL_NODE 任务已排队(%s 链路)",
		clusterNo, Str(n["node_no"]), ip, execPath))
	enqueueTask(taskNo, DeliveryArgs{NodeIPs: []string{ip}})
	return GetNode(id)
}

// ---------- 节点交付动作成功落点(FLOW-PROC-02 → FLOW-PROC-06 内核触达) ----------
func init() {
	taskSuccessHooks[TaskKindAddNode] = addNodeSuccessLanding
	taskSuccessHooks[TaskKindDelNode] = delNodeSuccessLanding
}

// addNodeSuccessLanding INV-14:加入成功后回填 K8s 节点名(未声明目标版本名则按集群号 + 地址派生)。
func addNodeSuccessLanding(db *gorm.DB, task, _ map[string]any, args DeliveryArgs, user UserInfo) error {
	clusterNo := Str(task["cluster_ref"])
	done, err := markNodesAddedTx(db, clusterNo, args.NodeIPs, trimEmpty(args.NodeName), user)
	if err != nil {
		return err
	}
	return finishTaskTx(db, task, TaskStatusSucceeded, nil,
		fmt.Sprintf("节点加入完成:%d 个待加入节点已回填 K8s 节点名(INV-14)", done), user)
}

// deriveK8sNodeName INV-14 的节点名派生口径(集群号小写 + 地址短横):
// 安装族下发清单与成功落点回填必须同一条口径,否则台账与集群里的节点名两样。
func deriveK8sNodeName(clusterNo, ip string) string {
	return strings.ToLower(strings.TrimSpace(clusterNo)) + "-" +
		strings.ReplaceAll(strings.TrimSpace(ip), ".", "-")
}

// markNodesAddedTx 待加入节点置 ADDED 并回填 K8s 节点名(INV-14)。
// ADD_NODE 成功落点与安装类成功落点共用:ips 为空则取该集群全部 PENDING(向导登记的节点)。
func markNodesAddedTx(db *gorm.DB, clusterNo string, ips []string, nameOverride string, user UserInfo) (int, error) {
	if len(ips) == 0 {
		ips = deriveNodeIPs(TaskKindAddNode, clusterNo)
	}
	done := 0
	for _, ip := range ips {
		name := trimEmpty(nameOverride)
		if name == "" {
			name = deriveK8sNodeName(clusterNo, ip)
		}
		res, err := store.Exec(db, `
			UPDATE clc_cluster_node SET node_status=?, k8s_nodename=?, node_health=?,
				node_health_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS'), updated_by=?,
				updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
			WHERE cluster_ref=? AND flag=1 AND node_status=? AND LOWER(ip_address)=LOWER(?)`,
			NodeStatusAdded, name, NodeHealthHealthy, user.ID, clusterNo, NodeStatusPending, ip)
		if err != nil {
			return done, err
		}
		if res == 0 {
			continue
		}
		done++
		publishClcEvent("clc.node.added", map[string]any{"cluster_no": clusterNo, "ip": ip, "k8s_nodename": name})
	}
	return done, nil
}

// delNodeSuccessLanding INV-13:摘除成功后节点置 REMOVED(终局保留行,不物理删)。
func delNodeSuccessLanding(db *gorm.DB, task, _ map[string]any, args DeliveryArgs, user UserInfo) error {
	clusterNo := Str(task["cluster_ref"])
	ips := args.NodeIPs
	if len(ips) == 0 {
		ips = deriveNodeIPs(TaskKindDelNode, clusterNo)
	}
	done := 0
	for _, ip := range ips {
		res, err := store.Exec(db, `
			UPDATE clc_cluster_node SET node_status=?, updated_by=?,
				updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
			WHERE cluster_ref=? AND flag=1 AND node_status=? AND LOWER(ip_address)=LOWER(?)`,
			NodeStatusRemoved, user.ID, clusterNo, NodeStatusRemoving, ip)
		if err != nil {
			return err
		}
		if res == 0 {
			continue
		}
		done++
		publishClcEvent("clc.node.removed", map[string]any{"cluster_no": clusterNo, "ip": ip})
	}
	return finishTaskTx(db, task, TaskStatusSucceeded, nil,
		fmt.Sprintf("节点移除完成:%d 个移除中节点置 REMOVED(INV-13 终局保留行)", done), user)
}

// RebindNode FUNC-08 节点重绑定(R-12:新资产未被占用(R-02 同口径)且登记值可反查;通过后换绑并刷新登记值)。
func RebindNode(id int64, d map[string]any, user UserInfo) (map[string]any, error) {
	n, err := GetNode(id)
	if err != nil {
		return nil, err
	}
	newAsset := Str(d["new_asset_ref"])
	if strings.TrimSpace(newAsset) == "" {
		return nil, Fail(errcode.ErrParam, "重绑定必须提供新资产标识")
	}
	if Str(n["node_status"]) != NodeStatusAdded && Str(n["node_status"]) != NodeStatusPending {
		return nil, bizRuleFail("INV-13", "REMOVED 为节点终局,不可重绑定;重新加入请登记新行")
	}
	clusterNo := Str(n["cluster_ref"])
	err = tx(func(db *gorm.DB) error {
		// R-12/R-02 同口径:新资产未被其他活跃集群占用
		occupied, err := store.List(db, `
			SELECT DISTINCT n.cluster_ref, c.name FROM clc_cluster_node n
			JOIN clc_cluster c ON c.cluster_no = n.cluster_ref AND c.flag = 1 AND c.status <> ?
			WHERE n.flag=1 AND n.asset_ref=? AND n.node_status IN (?, ?) AND n.cluster_ref = ? AND n.id <> ?`,
			ClusterStatusDestroyed, newAsset, NodeStatusPending, NodeStatusAdded, clusterNo, id)
		if err != nil {
			return err
		}
		_ = occupied
		if len(occupied) > 0 {
			return bizRuleFail("R-12", "新资产 %s 已被占用:重绑定被拒绝(R-02 同口径)", newAsset)
		}
		// 同集群内同资产重复占用也拒绝
		dup, err := store.One(db, `SELECT node_no FROM clc_cluster_node WHERE cluster_ref=? AND flag=1 AND asset_ref=? AND id<>? AND node_status IN (?, ?)`,
			clusterNo, newAsset, id, NodeStatusPending, NodeStatusAdded)
		if err != nil {
			return err
		}
		if dup != nil {
			return bizRuleFail("R-12", "新资产 %s 已被本集群节点 %s 占用", newAsset, Str(dup["node_no"]))
		}
		_, err = store.Exec(db, `UPDATE clc_cluster_node SET asset_ref=?, updated_by=?,
			updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
			newAsset, user.ID, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-08", fmt.Sprintf("节点 %s 重绑定:资产 %s → %s(登记值 IP %s 可反查保留)",
		Str(n["node_no"]), Str(n["asset_ref"]), newAsset, Str(n["ip_address"])))
	return GetNode(id)
}
