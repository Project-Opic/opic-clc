package service

import (
	"encoding/json"
	"fmt"
	"sort"
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

// seedRoleDesc m5 roles[].roleId → 角色说明列(m5 只给 roleId/name/permissions，说明是展示文案)。
var seedRoleDesc = map[string]string{
	"admin":       "系统内置超级管理员",
	"CLC-ROLE-01": "集群台账/节点/交付编排/引用参数/快照/探活全量维护(创建与提交方)",
	"CLC-ROLE-02": "运行观测与平台参数维护;集群销毁审批人(单级审批,强制不免审批)",
	"CLC-ROLE-03": "只读审计:集群/节点/引用/参数/快照/任务台账",
	"CLC-ROLE-04": "消费方只读:集群列表/详情/节点/探活/统计",
	"CLC-ROLE-05": "服务身份,非岗位(m5 actorType=SYSTEM;仅跨中心集群查询口径)",
}

// adminRoleCode 底座超管位角色码(不在 m5 模型内，由代码注入)。
const adminRoleCode = "admin"

// sortedKeys map 键升序切片(种子入库顺序稳定，结果可复算)。
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// seedPerm m5 permissions[] 对应的一行 sys_permission。
type seedPerm struct {
	Code, Name, TargetType, TargetRef, DataScope string
}

// buildSeedPermissions 由本体注册表展开 m5 permissions 全集，权限名取 m2 行为名。
// 模型是唯一语义来源:m5 增删权限码只改 YAML。此前此处硬编码 35 条权限名，
// 与 m5 的条数/口径各改各的，是种子漂移的源头。
func buildSeedPermissions() []seedPerm {
	reg := ontology.Load()
	out := make([]seedPerm, 0, len(reg.Permissions))
	for _, code := range sortedKeys(reg.Permissions) {
		p := reg.Permissions[code]
		ref := Str(p["targetRef"])
		name := ref
		if b, ok := reg.Behaviors[ref]; ok {
			name = Str(b["name"])
		}
		tt := Str(p["targetType"])
		if tt == "" {
			tt = "BEHAVIOR"
		}
		scope := Str(p["dataScope"])
		if scope == "" {
			scope = "ALL"
		}
		out = append(out, seedPerm{Code: code, Name: name, TargetType: tt, TargetRef: ref, DataScope: scope})
	}
	return out
}

// m5RoleNames m5 roles[].roleId → name，并补 admin 超管位。
func m5RoleNames() map[string]string {
	reg := ontology.Load()
	out := make(map[string]string, len(reg.Roles)+1)
	for id, role := range reg.Roles {
		out[id] = Str(role["name"])
	}
	out[adminRoleCode] = "系统管理员"
	return out
}

// buildSeedRolePermissions m5 roles[].permissions 全量映射(admin 无模型行，不参与绑定)。
func buildSeedRolePermissions() map[string][]string {
	reg := ontology.Load()
	out := make(map[string][]string, len(reg.Roles))
	for id, role := range reg.Roles {
		var codes []string
		if list, ok := role["permissions"].([]any); ok {
			for _, v := range list {
				if c := Str(v); c != "" {
					codes = append(codes, c)
				}
			}
		}
		sort.Strings(codes)
		out[id] = codes
	}
	return out
}

// ensureRoleID 幂等取角色 id:先按 code 命中，其次按 name 命中并把 code 补成模型口径。
// goose 的 m5 种子迁移与 EnsureSeed 写的是同一组 (code, name)，两条路径谁先跑都不能撞
// sys_role 的 name/code UNIQUE。
func ensureRoleID(code, name, desc string) (int64, error) {
	row, err := qOne(`SELECT id FROM sys_role WHERE code = ?`, code)
	if err != nil {
		return 0, err
	}
	if row != nil {
		return toInt(row["id"]), nil
	}
	row, err = qOne(`SELECT id FROM sys_role WHERE name = ?`, name)
	if err != nil {
		return 0, err
	}
	if row != nil {
		id := toInt(row["id"])
		if _, err := qExec(`UPDATE sys_role SET code = ? WHERE id = ?`, code, id); err != nil {
			return 0, err
		}
		return id, nil
	}
	if _, err := qExec(`INSERT INTO sys_role (name, code, parent_id, description, status) VALUES (?, ?, 0, ?, 1)`,
		name, code, desc); err != nil {
		return 0, err
	}
	row, err = qOne(`SELECT id FROM sys_role WHERE code = ?`, code)
	if err != nil {
		return 0, err
	}
	if row == nil {
		return 0, fmt.Errorf("角色入库后未取到 id: %s", code)
	}
	return toInt(row["id"]), nil
}

// ensurePermissionID 幂等取权限 id，并把已有行校正回 m5/m2 口径。
// 校正不可省:goose 的 m5 种子迁移曾把 target_ref 误写成权限码本身(FUNC-nn 才是 m5 的 targetRef)，
// 老代码里的半角括号权限名也与 m2 行为名不一致;只 INSERT 不校正，存量库会一直带着错值。
func ensurePermissionID(p seedPerm) (int64, error) {
	row, err := qOne(`SELECT id, name, target_type, target_ref, data_scope FROM sys_permission WHERE code = ?`, p.Code)
	if err != nil {
		return 0, err
	}
	if row != nil {
		id := toInt(row["id"])
		if Str(row["target_ref"]) == p.TargetRef && Str(row["target_type"]) == p.TargetType &&
			Str(row["name"]) == p.Name && Str(row["data_scope"]) == p.DataScope {
			return id, nil
		}
		if _, err := qExec(`
			UPDATE sys_permission SET name = ?, target_type = ?, target_ref = ?, data_scope = ? WHERE id = ?`,
			p.Name, p.TargetType, p.TargetRef, p.DataScope, id); err != nil {
			return 0, err
		}
		return id, nil
	}
	if _, err := qExec(`
		INSERT INTO sys_permission (code, name, target_type, target_ref, data_scope, status)
		VALUES (?, ?, ?, ?, ?, 1)`, p.Code, p.Name, p.TargetType, p.TargetRef, p.DataScope); err != nil {
		return 0, err
	}
	row, err = qOne(`SELECT id FROM sys_permission WHERE code = ?`, p.Code)
	if err != nil {
		return 0, err
	}
	if row == nil {
		return 0, fmt.Errorf("权限入库后未取到 id: %s", p.Code)
	}
	return toInt(row["id"]), nil
}

// ensureRolePermission 幂等绑定角色-权限。
func ensureRolePermission(roleID, permID int64) error {
	row, err := qOne(`SELECT role_id FROM sys_role_permission WHERE role_id = ? AND permission_id = ?`, roleID, permID)
	if err != nil || row != nil {
		return err
	}
	_, err = qExec(`INSERT INTO sys_role_permission (role_id, permission_id) VALUES (?, ?)`, roleID, permID)
	return err
}

// syncM5Authorization m5 角色/权限/角色权限绑定入库(幂等)，返回角色码 → id。
// 空库首建与存量库回填共用这一条路径:m5 加行为发牌只改 YAML，种子自己跟上，
// 与 goose 的 m5 种子迁移互为重复执行。
func syncM5Authorization() (map[string]int64, error) {
	names := m5RoleNames()
	roleIDs := map[string]int64{}
	for _, code := range sortedKeys(names) {
		id, err := ensureRoleID(code, names[code], seedRoleDesc[code])
		if err != nil {
			return nil, err
		}
		roleIDs[code] = id
	}
	permIDs := map[string]int64{}
	for _, p := range buildSeedPermissions() {
		id, err := ensurePermissionID(p)
		if err != nil {
			return nil, err
		}
		permIDs[p.Code] = id
	}
	bindings := buildSeedRolePermissions()
	for _, roleCode := range sortedKeys(bindings) {
		roleID, ok := roleIDs[roleCode]
		if !ok {
			continue
		}
		for _, pc := range bindings[roleCode] {
			permID, ok := permIDs[pc]
			if !ok {
				return nil, fmt.Errorf("角色 %s 绑定的 %s 不在 m5 permissions 全集", roleCode, pc)
			}
			if err := ensureRolePermission(roleID, permID); err != nil {
				return nil, err
			}
		}
	}
	return roleIDs, nil
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

// EnsureSeed 种子(幂等)。m5 角色/权限/绑定两条分支都要跟上(存量库补新发牌是接口层
// 403 的直接成因)，空库首建再补资源、账号与流程定义。
func EnsureSeed() error {
	count, err := qCount(`SELECT COUNT(*) FROM sys_user`)
	if err != nil {
		return err
	}
	roleIDs, err := syncM5Authorization()
	if err != nil {
		return err
	}
	if count > 0 {
		return ensureFlowDefinitions()
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

// buildNodeGraph M6 activities+nextActivities/branches → {nodes, edges}(指导书 §2.5 映射)。
// FLOW-PROC-APP-01(销毁审批)显式图在 YAML 中给出;其余协同流为可视化定义。
// GATEWAY 分支双落点:引擎(flow_engine.selectGatewayTargets)读节点上的 branches,
// 设计器(FlowDesigner)读 edges 连线 —— branches 与条件 edge 必须同时生成,否则分支丢失。
func buildNodeGraph(code string, flow map[string]any) map[string]any {
	activities, _ := flow["activities"].([]any)
	var nodes []any
	var edges []any
	addEdge := func(source, target string, extra map[string]any) {
		e := map[string]any{"source": source, "target": target}
		for k, v := range extra {
			e[k] = v
		}
		edges = append(edges, e)
	}
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
		subFlowRef, _ := am["subFlowRef"].(string)
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
		case "GATEWAY":
			node["type"] = "gateway"
			branches, _ := am["branches"].([]any)
			nodeBranches := make([]any, 0, len(branches))
			for _, b := range branches {
				bm, ok := b.(map[string]any)
				if !ok {
					continue
				}
				branchName, _ := bm["branchName"].(string)
				ruleRef, _ := bm["ruleRef"].(string)
				condition, _ := bm["conditionExpression"].(string)
				approvalOutcome, _ := bm["approvalOutcome"].(string)
				target, _ := bm["targetActivity"].(string)
				isDefault, _ := bm["isDefault"].(bool)
				nb := map[string]any{"target": target}
				if branchName != "" {
					nb["name"] = branchName
				}
				if ruleRef != "" {
					nb["rule_ref"] = ruleRef
				}
				if condition != "" {
					nb["condition"] = condition
				}
				if approvalOutcome != "" {
					nb["approval_outcome"] = approvalOutcome
				}
				if isDefault {
					nb["is_default"] = true
				}
				nodeBranches = append(nodeBranches, nb)
				if target != "" {
					extra := map[string]any{}
					if ruleRef != "" {
						extra["rule_ref"] = ruleRef
					}
					if condition != "" {
						extra["condition"] = condition
					}
					if approvalOutcome != "" {
						extra["approval_outcome"] = approvalOutcome
					}
					if isDefault {
						extra["is_default"] = true
					}
					addEdge(id, target, extra)
				}
			}
			node["branches"] = nodeBranches
		case "USER_TASK":
			node["type"] = "user_task"
			node["role_ref"] = roleRef
			if behaviorRef != "" {
				node["behavior_ref"] = behaviorRef
			}
		case "SUB_FLOW_CALL":
			node["type"] = "sub_flow_call"
			if subFlowRef != "" {
				node["sub_flow_ref"] = subFlowRef
			}
		case "BEHAVIOR_CALL":
			node["type"] = "behavior_call"
			node["behavior_ref"] = behaviorRef
		case "SYSTEM_TASK":
			node["type"] = "system_task"
			if behaviorRef != "" {
				node["behavior_ref"] = behaviorRef
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
		if at == "GATEWAY" {
			continue // 分支 edge 已生成,GATEWAY 无 nextActivities
		}
		nexts, _ := am["nextActivities"].([]any)
		for _, nx := range nexts {
			if t, ok := nx.(string); ok && t != "" {
				addEdge(id, t, nil)
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
