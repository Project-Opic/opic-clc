package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/ontology"
)

// GetRoleByCode 按编码取角色。
func GetRoleByCode(code string) (map[string]any, error) {
	return qOne(`SELECT * FROM sys_role WHERE code = ?`, code)
}

// SyncUserRolesByCodes 按角色编码全量同步用户角色(ZITADEL claim → 本地角色)。
// 仅映射本地已存在的角色;返回实际生效的本地角色编码。
func SyncUserRolesByCodes(userID int64, roleCodes []string) error {
	var roleIDs []any
	applied := map[string]bool{}
	for _, code := range roleCodes {
		code = strings.TrimSpace(code)
		if code == "" || applied[code] {
			continue
		}
		role, err := GetRoleByCode(code)
		if err != nil || role == nil {
			continue
		}
		applied[code] = true
		roleIDs = append(roleIDs, Int(role["id"]))
	}
	if len(roleIDs) == 0 {
		return nil
	}
	return SetUserRoles(userID, roleIDs)
}

// ---------- 种子数据(cluster-center-mcc,m5-actor-model.yaml + mu-ui-model.yaml) ----------

// seedRoles 角色(m5 actors/roles;admin 为底座超管位)。
var seedRoles = []struct{ Code, Name, Desc string }{
	{"admin", "系统管理员", "系统内置超级管理员"},
	{"CLC-ROLE-01", "集群管理员", "集群台账/节点/交付编排/引用参数/快照/探活全量维护(创建与提交方)"},
	{"CLC-ROLE-02", "平台运维管理员", "运行观测与平台参数维护;集群销毁审批人(单级审批,强制不免审批)"},
	{"CLC-ROLE-03", "集群审计员", "只读审计:集群/节点/引用/参数/快照/任务台账"},
	{"CLC-ROLE-04", "集群只读用户", "消费方只读:集群列表/详情/节点/探活/统计"},
	{"CLC-ROLE-05", "下游能力中心", "服务身份,非岗位(m5 actorType=SYSTEM;仅跨中心集群查询口径)"},
}

// permNames PERM-FUNC-01..32 → m2 行为名(权限种子列序与值序一致)。
var permNames = map[int]string{
	1:  "创建集群(四步向导提交)",
	2:  "集群信息修改",
	3:  "发起安装",
	4:  "安装续跑",
	5:  "集群销毁 · 提交申请",
	6:  "添加节点",
	7:  "移除节点",
	8:  "节点重绑定",
	9:  "发起升级",
	10: "发起备份",
	11: "发起恢复",
	12: "集群启停",
	13: "调整中心引用",
	14: "参数人工覆写",
	15: "触发参数同步(确认执行)",
	16: "显式恢复(重绑定 / 确认)",
	17: "创建版本快照",
	18: "版本回滚",
	19: "集群列表查询",
	20: "集群详情查询",
	21: "节点清单查询",
	22: "引用台账查询",
	23: "参数清单查询",
	24: "快照清单查询",
	25: "任务台账查询",
	26: "探活历史查询",
	27: "聚合健康查询",
	28: "跨中心集群查询(多用途)",
	29: "统计总览查询",
	30: "运行参数维护",
	31: "集群销毁 · 通过",
	32: "集群销毁 · 驳回",
}

// buildSeedPermissions 由 permNames 展开(m5 permissions 全集,严格与 m2 requiredPermissions 对齐)。
func buildSeedPermissions() []struct{ Code, Name, TargetType, TargetRef string } {
	out := make([]struct{ Code, Name, TargetType, TargetRef string }, 0, len(permNames))
	for i := 1; i <= 32; i++ {
		out = append(out, struct{ Code, Name, TargetType, TargetRef string }{
			Code: fmt.Sprintf("PERM-FUNC-%02d", i), Name: permNames[i], TargetType: "BEHAVIOR", TargetRef: fmt.Sprintf("FUNC-%02d", i),
		})
	}
	return out
}

// seedResources 菜单资源(mu-ui-model application.menus + 工作台)。
// clc-* 菜单 path 对应前端路由,permission_code 控制可见性(m5 权限矩阵)。
var seedResources = []struct {
	Parent, Name, Code, PermCode, Type, Path, Icon string
	Sort                                           int
}{
	// 集群管理(menu-l1-01)
	{"", "集群管理", "menu-l1-01", "", "DIRECTORY", "", "Cluster", 10},
	{"menu-l1-01", "集群台账", "clc-cluster", "PERM-FUNC-19", "MENU", "/clc/cluster", "Database", 11},
	{"menu-l1-01", "节点管理", "clc-node", "PERM-FUNC-21", "MENU", "/clc/node", "Hdd", 12},
	{"menu-l1-01", "交付编排", "clc-orch", "PERM-FUNC-25", "MENU", "/clc/orch", "DeploymentUnit", 13},
	{"menu-l1-01", "引用与参数", "clc-ref", "PERM-FUNC-22", "MENU", "/clc/ref", "Api", 14},
	{"menu-l1-01", "快照与回滚", "clc-snap", "PERM-FUNC-24", "MENU", "/clc/snap", "History", 15},
	{"menu-l1-01", "健康探活", "clc-health", "PERM-FUNC-26", "MENU", "/clc/health", "Heartbeat", 16},
	// 运行观测(menu-l1-02)
	{"", "运行观测", "menu-l1-02", "", "DIRECTORY", "", "Dashboard", 20},
	{"menu-l1-02", "运行观测", "clc-ops", "PERM-FUNC-29", "MENU", "/clc/ops", "FundView", 21},
	{"menu-l1-02", "平台参数", "clc-params", "", "MENU", "/clc/params", "Setting", 22},
	// 审批中心(工作台)
	{"", "审批中心", "menu-workbench", "", "DIRECTORY", "", "ClipboardList", 30},
	{"menu-workbench", "我的待办", "menu-workbench-todo", "", "MENU", "/workbench/todo", "Inbox", 31},
	{"menu-workbench", "我的已办", "menu-workbench-done", "", "MENU", "/workbench/done", "CheckSquare", 32},
	{"menu-workbench", "我的申请", "menu-workbench-requested", "", "MENU", "/workbench/requested", "FileText", 33},
}

// seedUsers 用户(admin + 四岗位各一,便于销毁审批全链路演示;CLC-ROLE-05 为服务身份不建账号)。
var seedUsers = []struct{ Username, Password, RealName, RoleCode string }{
	{"admin", "admin123", "管理员", "admin"},
	{"clcadmin", "admin123", "集群管理员", "CLC-ROLE-01"},
	{"opsadmin", "admin123", "严运维(平台运维管理员)", "CLC-ROLE-02"},
	{"auditor", "admin123", "时审计", "CLC-ROLE-03"},
	{"viewuser", "admin123", "只读用户", "CLC-ROLE-04"},
}

// seedRolePermissions m5 roles[].permissions 全量映射(严格照抄模型;admin 超管位代码层注入)。
var seedRolePermissions = map[string][]string{
	"CLC-ROLE-01": {
		"PERM-FUNC-01", "PERM-FUNC-02", "PERM-FUNC-03", "PERM-FUNC-04", "PERM-FUNC-05",
		"PERM-FUNC-06", "PERM-FUNC-07", "PERM-FUNC-08", "PERM-FUNC-09", "PERM-FUNC-10",
		"PERM-FUNC-11", "PERM-FUNC-12", "PERM-FUNC-13", "PERM-FUNC-14", "PERM-FUNC-15",
		"PERM-FUNC-16", "PERM-FUNC-17", "PERM-FUNC-18", "PERM-FUNC-19", "PERM-FUNC-20",
		"PERM-FUNC-21", "PERM-FUNC-22", "PERM-FUNC-23", "PERM-FUNC-24", "PERM-FUNC-25",
		"PERM-FUNC-26", "PERM-FUNC-27", "PERM-FUNC-29",
	},
	"CLC-ROLE-02": {
		"PERM-FUNC-19", "PERM-FUNC-20", "PERM-FUNC-21", "PERM-FUNC-22", "PERM-FUNC-23",
		"PERM-FUNC-24", "PERM-FUNC-25", "PERM-FUNC-26", "PERM-FUNC-27", "PERM-FUNC-29",
		"PERM-FUNC-30", "PERM-FUNC-31", "PERM-FUNC-32",
	},
	"CLC-ROLE-03": {
		"PERM-FUNC-19", "PERM-FUNC-20", "PERM-FUNC-21", "PERM-FUNC-22", "PERM-FUNC-23",
		"PERM-FUNC-24", "PERM-FUNC-25", "PERM-FUNC-26", "PERM-FUNC-27", "PERM-FUNC-29",
	},
	"CLC-ROLE-04": {
		"PERM-FUNC-19", "PERM-FUNC-20", "PERM-FUNC-21", "PERM-FUNC-26", "PERM-FUNC-27", "PERM-FUNC-29",
	},
	"CLC-ROLE-05": {
		"PERM-FUNC-28",
	},
}

// EnsureSeed 种子(幂等;users 非空则仅确保流程定义)。
func EnsureSeed() error {
	count, err := qCount(`SELECT COUNT(*) FROM sys_user`)
	if err != nil {
		return err
	}
	if count > 0 {
		return ensureFlowDefinitions()
	}
	// 角色
	roleIDs := map[string]int64{}
	for _, r := range seedRoles {
		if _, err := qExec(`INSERT INTO sys_role (name, code, parent_id, description, status) VALUES (?, ?, 0, ?, 1)`,
			r.Name, r.Code, r.Desc); err != nil {
			return err
		}
		row, err := qOne(`SELECT id FROM sys_role WHERE code = ?`, r.Code)
		if err != nil {
			return err
		}
		roleIDs[r.Code] = toInt(row["id"])
	}
	// 权限
	for _, p := range buildSeedPermissions() {
		if _, err := qExec(`
			INSERT INTO sys_permission (code, name, target_type, target_ref, data_scope, status)
			VALUES (?, ?, ?, ?, 'ALL', 1)`, p.Code, p.Name, p.TargetType, p.TargetRef); err != nil {
			return err
		}
	}
	perms, err := qList(`SELECT id, code FROM sys_permission`)
	if err != nil {
		return err
	}
	permIDByCode := map[string]any{}
	for _, p := range perms {
		permIDByCode[Str(p["code"])] = p["id"]
	}
	// 资源
	resIDs := map[string]int64{}
	for _, r := range seedResources {
		parentID := resIDs[r.Parent]
		if _, err := qExec(`
			INSERT INTO sys_resource (parent_id, name, code, permission_code, type, path, component, icon, sort_order, status)
			VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, 1)`,
			parentID, r.Name, r.Code, r.PermCode, r.Type, r.Path, r.Icon, r.Sort); err != nil {
			return err
		}
		row, err := qOne(`SELECT id FROM sys_resource WHERE code = ?`, r.Code)
		if err != nil {
			return err
		}
		resIDs[r.Code] = toInt(row["id"])
	}
	// 用户
	for _, u := range seedUsers {
		if _, err := qExec(`
			INSERT INTO sys_user (username, password, real_name, actor_type, status)
			VALUES (?, ?, ?, 'HUMAN', 1)`, u.Username, HashPassword(u.Password), u.RealName); err != nil {
			return err
		}
		row, err := qOne(`SELECT id FROM sys_user WHERE username = ?`, u.Username)
		if err != nil {
			return err
		}
		uid := toInt(row["id"])
		if u.RoleCode != "" && roleIDs[u.RoleCode] > 0 {
			if _, err := qExec(`INSERT INTO sys_user_role (user_id, role_id) VALUES (?, ?)`, uid, roleIDs[u.RoleCode]); err != nil {
				return err
			}
		}
	}
	// 角色权限(admin 超管位由 GetPermissionCodes 代码层注入,不落数据)
	for roleCode, permCodes := range seedRolePermissions {
		roleID := roleIDs[roleCode]
		for _, pc := range permCodes {
			if _, err := qExec(`INSERT INTO sys_role_permission (role_id, permission_id) VALUES (?, ?)`,
				roleID, permIDByCode[pc]); err != nil {
				return err
			}
		}
	}
	// ensureAdminRole 幂等:超管位角色与 admin 账号绑定兜底(缺角色行则补建)
	if err := ensureAdminRole(); err != nil {
		return err
	}
	return ensureFlowDefinitions()
}

// ensureAdminRole 幂等确保 admin 超管角色存在且绑定到 admin 账号。
func ensureAdminRole() error {
	role, err := qOne(`SELECT id FROM sys_role WHERE code = 'admin'`)
	if err != nil {
		return err
	}
	if role == nil {
		if _, err := qExec(`INSERT INTO sys_role (name, code, parent_id, description, status)
			VALUES ('系统管理员', 'admin', 0, '系统内置超级管理员', 1)`); err != nil {
			return err
		}
		role, err = qOne(`SELECT id FROM sys_role WHERE code = 'admin'`)
		if err != nil {
			return err
		}
	}
	user, err := qOne(`SELECT id FROM sys_user WHERE username = 'admin'`)
	if err != nil || user == nil {
		return err
	}
	bound, err := qOne(`SELECT user_id FROM sys_user_role WHERE user_id = ? AND role_id = ?`,
		toInt(user["id"]), toInt(role["id"]))
	if err != nil {
		return err
	}
	if bound == nil {
		_, err = qExec(`INSERT INTO sys_user_role (user_id, role_id) VALUES (?, ?)`,
			toInt(user["id"]), toInt(role["id"]))
		return err
	}
	return nil
}

// ---------- M6 → flow_definition(节点图转换,指导书 §5.2) ----------

// ensureFlowDefinitions 从本体注册表 M6 流程定义导入(幂等),node_graph 缺省时
// 由 activities+nextActivities 自动转换;FLOW-PROC-APP-01 销毁审批带显式可执行
// nodeGraph(含 edges 与 CLC-ROLE-02 审批角色,审批角色须有用户绑定)。
func ensureFlowDefinitions() error {
	reg := ontology.Load()
	for code, flow := range reg.Flows {
		if exists, err := qOne(`SELECT id FROM flow_definition WHERE code = ?`, code); err != nil {
			return err
		} else if exists != nil {
			continue
		}
		name, _ := flow["name"].(string)
		if name == "" || name == "|" {
			name = code
		}
		flowType, _ := flow["flowType"].(string)
		if flowType == "" {
			flowType = "COLLABORATION"
		}
		triggerType := "MANUAL"
		triggerBehavior := ""
		if trigger, ok := flow["trigger"].(map[string]any); ok {
			if v, ok := trigger["triggerType"].(string); ok && v != "" {
				triggerType = v
			}
			if v, ok := trigger["behaviorRef"].(string); ok {
				triggerBehavior = v
			}
		}
		desc, _ := flow["description"].(string)
		nodeGraph := "null"
		if ng, ok := flow["nodeGraph"]; ok && ng != nil {
			raw, err := json.Marshal(ng)
			if err == nil {
				nodeGraph = string(raw)
			}
		} else {
			nodeGraph = MarshalJSONVal(buildNodeGraph(code, flow))
		}
		if _, err := qExec(`
			INSERT INTO flow_definition (code, name, flow_type, trigger_type, trigger_behavior, description, node_graph, version, status, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1, 1, 1)`,
			code, name, flowType, triggerType, triggerBehavior, desc, nodeGraph); err != nil {
			return err
		}
	}
	return nil
}

// buildNodeGraph M6 activities+nextActivities → {nodes, edges}(指导书 §2.5 映射)。
// FLOW-PROC-APP-01(销毁审批)显式图在 YAML 中给出;其余协同流为可视化定义。
func buildNodeGraph(code string, flow map[string]any) map[string]any {
	activities, _ := flow["activities"].([]any)
	var nodes []any
	var edges []any
	for _, a := range activities {
		am, ok := a.(map[string]any)
		if !ok {
			continue
		}
		id, _ := am["activityId"].(string)
		name, _ := am["name"].(string)
		at, _ := am["activityType"].(string)
		roleRef, _ := am["roleRef"].(string)
		behaviorRef, _ := am["behaviorRef"].(string)
		node := map[string]any{"id": id, "name": name}
		switch at {
		case "START":
			node["type"] = "start"
		case "END":
			node["type"] = "end"
			if strings.Contains(name, "驳回") || strings.Contains(name, "拒绝") {
				node["result"] = "REJECTED"
			} else {
				node["result"] = "APPROVED"
			}
		default:
			switch {
			case roleRef != "":
				node["type"] = "approval_task"
				node["role_ref"] = roleRef
				node["outcomes"] = []any{"APPROVE", "REJECT"}
			case behaviorRef != "":
				node["type"] = "behavior_call"
				node["behavior_ref"] = behaviorRef
			default:
				node["type"] = "system_task"
			}
		}
		nodes = append(nodes, node)
		nexts, _ := am["nextActivities"].([]any)
		for _, nx := range nexts {
			if t, ok := nx.(string); ok && t != "" {
				edges = append(edges, map[string]any{"source": id, "target": t})
			}
		}
	}
	if nodes == nil {
		nodes = []any{}
	}
	if edges == nil {
		edges = []any{}
	}
	return map[string]any{"nodes": nodes, "edges": edges}
}
