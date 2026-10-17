// Package service —— AI 智能助理(cluster-center-mcc 扩展:本体注册表注入 + 只读 SQL 工具)。
//
// 口径(《AI 原生应用技术架构设计文档》第 9 章 / 指导书第 7 章):
//   - 系统提示词由本体注册表动态构建:业务域概述、对象与字段、行为清单、规则清单、
//     数据库表结构(下划线真实表名/字段)、只读安全边界;
//   - LLM 优先(管理控制台配置启用时,OpenAI 兼容协议),失败/未配置降级为规则式应答
//     (待办/分类/包/实例/批次/一致性,全部走既有只读查询);
//   - 只读 SQL 工具(readonly_sql_query):仅 SELECT、表白名单、强制 LIMIT、禁多语句、
//     执行审计(audit_logs);AI 对话默认只读,写操作一律引导到固定页面。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"

	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/ontology"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
	"gitcode.com/opic-ontology/opic-techbase/pkg/llmcfg"
)

// AIQueryTable 只读 SQL 结果表(渲染协议 text/table 子集;列序固定,前端表头稳定)。
type AIQueryTable struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// AssistantReply 助理应答。
type AssistantReply struct {
	Answer string        `json:"answer"`
	Hints  []string      `json:"hints"`           // 建议追问
	Action string        `json:"action"`          // 建议跳转路径(可空)
	Table  *AIQueryTable `json:"table,omitempty"` // 只读 SQL 工具的结构化结果(前端表格渲染)
}

// sqlOnlyWhitelist 平台参数表(m1 之外本域代管,纳入白名单)。
var sqlOnlyWhitelist = []string{"clc_platform_param"}

// clcRealTables 本域物理表名(注册表别名为 Cluster/ClusterNode...,物理表统一带 clc_ 前缀;
// 白名单须同时收口物理表名,否则 AI 只读 SQL 永远拒绝本域表)。
var clcRealTables = []string{"clc_cluster", "clc_cluster_node", "clc_binding", "clc_param", "clc_snapshot", "clc_task", "clc_probe"}

// sqlForbidden 只读边界:出现即拒绝(架构文档 §9.11)。
var sqlForbidden = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|TRUNCATE|GRANT|REVOKE|COPY|VACUUM|CALL|DO|MERGE|REPLACE|EXECUTE|SET|VACUUM|REINDEX|CLUSTER|LOCK|LISTEN|NOTIFY|INTO)\b`)

// sqlLocking 行锁子句:只读工具不允许在任何表上取锁(FOR UPDATE/SHARE 系列)。
var sqlLocking = regexp.MustCompile(`(?i)\bFOR\s+(?:NO\s+KEY\s+|KEY\s+)?(?:UPDATE|SHARE)\b`)

// ReadOnlySQLLimit 只读查询强制上限。
const ReadOnlySQLLimit = 200

// WhitelistedTables 只读 SQL 白名单(本体注册表 DBWhitelist + 平台参数表)。
func WhitelistedTables() map[string]ontology.WhitelistEntry {
	out := map[string]ontology.WhitelistEntry{}
	for name, entry := range ontology.Load().DBWhitelist {
		out[strings.ToLower(name)] = entry
	}
	for _, t := range clcRealTables {
		if _, ok := out[t]; !ok {
			out[t] = ontology.WhitelistEntry{Table: t, Alias: t, Attributes: nil}
		}
	}
	for _, t := range sqlOnlyWhitelist {
		if _, ok := out[t]; !ok {
			out[t] = ontology.WhitelistEntry{Table: t, Alias: t, Attributes: nil}
		}
	}
	return out
}

// RunReadOnlySQL 只读 SQL 工具(readonly_sql_query):
// 仅单条 SELECT/WITH;表必须落在白名单;无 LIMIT 自动追加;执行写入审计日志。
// 返回 {columns, rows} 结构表与最终执行的 SQL(列名排序固定,前端表头稳定)。
func RunReadOnlySQL(uid int64, username, raw string) (*AIQueryTable, string, error) {
	sqlText := strings.TrimSpace(raw)
	sqlText = strings.TrimSuffix(sqlText, ";")
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return nil, "", Fail(errcode.ErrParam, "SQL 不能为空")
	}
	// 1) 仅 SELECT/WITH 单语句
	if strings.Contains(sqlText, ";") {
		return nil, "", Fail(errcode.ErrParam, "只读 SQL 工具禁止多语句")
	}
	lower := strings.ToLower(sqlText)
	if !strings.HasPrefix(lower, "select") && !strings.HasPrefix(lower, "with") {
		return nil, "", Fail(errcode.ErrParam, "只读 SQL 工具仅接受 SELECT/WITH 查询")
	}
	// 2) 写关键字一律拒绝
	if m := sqlForbidden.FindString(sqlText); m != "" {
		return nil, "", Fail(errcode.ErrParam, "只读 SQL 工具禁止写操作(命中关键字 %s):写数据请通过固定业务页面执行", strings.ToUpper(m))
	}
	if m := sqlLocking.FindString(sqlText); m != "" {
		return nil, "", Fail(errcode.ErrParam, "只读 SQL 工具禁止行锁子句(%s)", strings.ToUpper(m))
	}
	// 3) 表白名单(fail-closed):词法级解析 FROM/JOIN 关系项并逐一比对(assistant_sqlguard.go)
	if err := validateSQLTableRefs(sqlText, WhitelistedTables()); err != nil {
		return nil, "", err
	}
	// 4) 强制 LIMIT
	if !regexp.MustCompile(`(?i)\blimit\s+\d+\s*$`).MatchString(sqlText) {
		sqlText += fmt.Sprintf(" LIMIT %d", ReadOnlySQLLimit)
	}
	// 5) 执行(只读事务)并审计:即便词法过滤未来出现缺口,写语句也会被数据库拒绝
	rows, err := qListReadOnly(sqlText)
	WriteAudit(nil, uid, username, "AI_SQL_READONLY", fmt.Sprintf("只读 SQL 工具执行:%s(返回 %d 行)", sqlText, len(rows)))
	if err != nil {
		return nil, sqlText, Fail(errcode.ErrDB, "SQL 执行失败:%v", err)
	}
	t := &AIQueryTable{Columns: []string{}, Rows: [][]any{}}
	if len(rows) > 0 {
		for k := range rows[0] {
			t.Columns = append(t.Columns, k)
		}
		sort.Strings(t.Columns) // map 遍历无序 → 列序固定,前端表头稳定
	}
	for _, r := range rows {
		row := make([]any, 0, len(t.Columns))
		for _, c := range t.Columns {
			row = append(row, r[c])
		}
		t.Rows = append(t.Rows, row)
	}
	return t, sqlText, nil
}

func mapKeys(m map[string]ontology.WhitelistEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// buildOntologySystemPrompt 基于本体注册表动态构建系统提示词(架构文档 §9.4 四类语义缺一不可)。
func buildOntologySystemPrompt() string {
	reg := ontology.Load()
	var b strings.Builder
	b.WriteString("你是「OPIC 集群域(cluster-center-mcc)」的 AI 智能助理。\n")
	b.WriteString("职责:业务问答、功能导航、只读查询与统计。你只能给文字指引与只读查询,不能执行任何写操作;写操作一律提示用户到固定业务页面。\n\n")

	// 1) 软件需求概述
	b.WriteString("【业务域概述】集群域负责:集群台账(四步向导创建、信息维护、显式恢复)、节点管理(添加/移除/重绑定,资产占用拦截)、" +
		"交付任务(安装/续跑/升级/备份/恢复/启停/销毁审批)、引用与参数(中心引用双写、参数派生与覆写、待同步确认)、" +
		"快照与回滚(序号单调、现存性校验、放弃引用回滚)、健康探活(双源口径、降级显式恢复)。\n" +
		"集群生命周期:DRAFT → INSTALLING → RUNNING / INSTALL_FAILED(续跑)→ DESTROYING → DESTROYED(终态保留);销毁为单级审批(FLOW-PROC-APP-01,CLC-ROLE-02 审批,强制不免审批)。\n\n")

	// 2) 本体对象与字段
	b.WriteString("【业务对象与字段】\n")
	for _, entry := range whitelistSortedEntries(reg) {
		fmt.Fprintf(&b, "- %s(%s): %s\n", entry.Table, entry.Alias, strings.Join(entry.Attributes, ", "))
	}
	b.WriteString("- clc_platform_param(平台参数): param_key, param_value, param_group, description\n")
	b.WriteString("提示:以上别名的物理表统一为 clc_ 前缀(clc_cluster/clc_cluster_node/clc_binding/clc_param/clc_snapshot/clc_task/clc_probe),只读 SQL 请直接使用物理表名。\n")

	// 行为清单
	if n := len(reg.Behaviors); n > 0 {
		b.WriteString(fmt.Sprintf("\n【行为清单】共 %d 个行为(以 FUNC-xx 编号登记,界面入口按 PERM-FUNC-xx 一一把门):\n", n))
		i := 0
		for id, bh := range reg.Behaviors {
			i++
			name, _ := bh["name"].(string)
			bt, _ := bh["behaviorType"].(string)
			fmt.Fprintf(&b, "- %s %s(%s)", id, name, bt)
			if i%2 == 0 {
				b.WriteString("\n")
			}
		}
		if i%2 != 0 {
			b.WriteString("\n")
		}
	}
	// 规则清单
	if n := len(reg.Rules); n > 0 {
		var fmtN, invN, crossN int
		for _, r := range reg.Rules {
			switch rt, _ := r["ruleType"].(string); rt {
			case "FORMAT":
				fmtN++
			case "INVARIANT":
				invN++
			default:
				crossN++
			}
		}
		b.WriteString(fmt.Sprintf("\n【规则清单】共 %d 条:格式类 %d 条(REF-xx)、不变量 %d 条(INV-xx,如 INV-02 状态单向、INV-16 引用组合唯一)、跨对象 %d 条(R-xx,如 R-01 双写同步、R-02 资产占用拦截、R-04 参数派生保护、R-06 快照回滚校验、R-08 健康双源、R-10 销毁强制审批、R-11 显式恢复)。违反规则的动作会被拒绝并返回中文原因。\n", n, fmtN, invN, crossN))
	}
	// 字典
	if n := len(reg.DataDictionaries); n > 0 {
		b.WriteString(fmt.Sprintf("\n【数据字典】共 %d 个字典组(CLC_CLUSTER_STATUS/CLC_HEALTH/CLC_TASK_STATUS/CLC_TASK_KIND/CLC_NODE_ROLE/CLC_TARGET_CAPABILITY 等 20 组),经 /api/meta/dictionaries 取用。\n", n))
	}
	// 菜单导航（v1.3 载体置换后的形态：侧栏一级四叶 + 集群台五页签，MU application.menus / retiredMenus）
	b.WriteString("\n【页面导航】集群 /clc/cluster(列表,行操作「台」进集群台 /clc/console/:id,五页签 ?tab=config|install|lifecycle|node|log = 配置/安装/生命周期/节点/日志)、集群模板 /clc/template、交付任务 /clc/orch、运行观测 /clc/ops、审批中心 /workbench/todo。节点管理/装机向导/引用与参数/快照与回滚/健康探活/平台参数六叶已换成集群台页签,旧深链自动重定向。\n")

	// 3) 只读能力清单
	b.WriteString("\n【可用只读工具】\n")
	b.WriteString("- 固定查询接口:/api/clc/cluster、/api/clc/node、/api/clc/binding、/api/clc/param、/api/clc/snapshot、/api/clc/task、/api/clc/probe、/api/clc/health/aggregate、/api/clc/statistics(均分页,OPIC 信封返回)。\n")
	b.WriteString("- 只读 SQL 工具(readonly_sql_query):前端对包含 SELECT 的输入会调用 /api/assistant/sql;你可建议用户直接输入以 SELECT 开头的统计 SQL,例如:SELECT status, COUNT(*) AS cnt FROM clc_cluster GROUP BY status。\n")

	// 4) 安全边界与渲染协议
	b.WriteString("\n【安全边界】动态 SQL 严格只读:仅 SELECT、仅白名单表(上列业务表)、自动追加 LIMIT、禁多语句、执行留审计。\n")
	b.WriteString("【渲染约定】普通回答用文字;列表/统计结果建议用户输入 SELECT 语句以表格呈现;需要操作时给出页面路径(如「请到 交付任务 页面执行」)。\n")
	b.WriteString("与系统无关的问题请礼貌拒绝。用中文简洁回答。")
	return b.String()
}

func whitelistSortedEntries(reg *ontology.Registry) []ontology.WhitelistEntry {
	entries := make([]ontology.WhitelistEntry, 0, len(reg.DBWhitelist))
	for _, e := range reg.DBWhitelist {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Table < entries[j].Table })
	return entries
}

// AssistantChat 助理问答入口。accountID=0 → 首个启用账号;model 空 → 账号默认模型;
// temperature<=0 → 账号默认温度。LLM 失败/未配置一律降级规则应答。
func AssistantChat(uid int64, question string, accountID int64, model string, temperature float64) (*AssistantReply, error) {
	q := strings.TrimSpace(question)
	prompt := buildOntologySystemPrompt()
	// 审计用户名(只读 SQL 工具留痕用)
	username := ""
	if row, err := qOne(`SELECT username FROM sys_user WHERE id=?`, uid); err == nil && row != nil {
		username = Str(row["username"])
	}
	if username == "" {
		username = "ai-user"
	}

	// LLM 优先(多供应商账号;系统提示词注入本体注册表语义与只读边界)
	if acct := pickAccount(accountID); acct != nil {
		if reply, ok := clcLLMAnswer(uid, username, acct, model, temperature, prompt, q); ok {
			return reply, nil
		}
	} else if cfg := llmcfg.Current(); cfg != nil {
		// 兼容旧单行配置(未迁移成账号时;单轮,含 SQL 代码块兜底执行)
		answer, err := llmcfg.Chat(context.Background(), cfg, []llmcfg.Message{
			{Role: "system", Content: prompt},
			{Role: "user", Content: q},
		})
		if err == nil {
			reply := &AssistantReply{Answer: answer, Hints: clcHints}
			if sql := extractSQLBlock(answer); sql != "" {
				if table, _, qerr := RunReadOnlySQL(uid, username, sql); qerr == nil {
					reply.Table = table
					reply.Answer = plainClcAnswer(answer)
				}
			}
			return reply, nil
		}
		log.Printf("[assistant] LLM 调用失败,降级规则匹配: %v", err)
	}

	// 规则降级:用户直接输入 SELECT 语句 → 走只读 SQL 工具并表格呈现
	if sql := extractSQLBlock(q); sql != "" {
		return clcSQLReply(uid, username, sql), nil
	}
	if m := regexp.MustCompile(`(?is)^\s*(select|with)\b`).FindString(q); m != "" {
		return clcSQLReply(uid, username, q), nil
	}

	lq := strings.ToLower(q)
	switch {
	case strings.Contains(q, "待办") || strings.Contains(lq, "todo"):
		tasks, err := WorkbenchTodo(uid, 1, 10)
		if err != nil {
			return nil, err
		}
		var bd strings.Builder
		fmt.Fprintf(&bd, "您当前有 %d 条待办任务。", tasks.Total)
		for i, t := range tasks.List {
			if i >= 5 {
				break
			}
			fmt.Fprintf(&bd, "\n%d. %s(集群 %s %s)", i+1, t["activity_name"], t["cluster_no"], orDefaultStrAny(t["destroy_reason"], ""))
		}
		if tasks.Total > 5 {
			fmt.Fprintf(&bd, "\n……其余 %d 条请在「审批中心-我的待办」查看。", tasks.Total-5)
		}
		return &AssistantReply{Answer: bd.String(), Action: "/workbench/todo",
			Hints: []string{"集群台账有哪些？", "销毁审批怎么操作？"}}, nil
	case strings.Contains(q, "集群") || strings.Contains(q, "台账"):
		p, err := ListClusters(1, 100, nil)
		if err != nil {
			return nil, err
		}
		var bd strings.Builder
		fmt.Fprintf(&bd, "当前共 %d 个集群:", p.Total)
		for i, s := range p.List {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&bd, "\n%d. %s(%s,%s,健康 %s,节点 %d)", i+1, s["name"], s["cluster_no"], s["status"], s["health"], Int(s["node_count"]))
		}
		return &AssistantReply{Answer: bd.String(), Action: "/clc/cluster",
			Hints: []string{"节点清单有哪些？", "探活历史怎么查？"}}, nil
	case strings.Contains(q, "节点"):
		p, err := ListNodes(1, 100, nil)
		if err != nil {
			return nil, err
		}
		var bd strings.Builder
		fmt.Fprintf(&bd, "当前共 %d 个节点:", p.Total)
		for i, s := range p.List {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&bd, "\n%d. %s(%s@%s,%s,%s,%s)", i+1, s["node_no"], s["cluster_ref"], s["ip_address"], s["node_role"], s["node_status"], s["node_health"])
		}
		return &AssistantReply{Answer: bd.String(), Action: "/clc/node",
			Hints: []string{"资产占用拦截是什么？", "集群台账有哪些？"}}, nil
	case strings.Contains(q, "引用") || strings.Contains(q, "参数") || strings.Contains(q, "镜像"):
		p, err := ListBindings(1, 100, nil)
		if err != nil {
			return nil, err
		}
		var bd strings.Builder
		fmt.Fprintf(&bd, "当前共 %d 条引用台账:", p.Total)
		for i, s := range p.List {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&bd, "\n%d. %s(%s@%s,%s/%s,%s,来源 %s)", i+1, s["binding_no"], s["cluster_ref"], s["target_ref"], s["target_capability"], s["target_kind"], s["purpose"], s["binding_source"])
		}
		return &AssistantReply{Answer: bd.String(), Action: "/clc/ref",
			Hints: []string{"参数待同步怎么确认？", "R-01 双写是什么？"}}, nil
	case strings.Contains(q, "快照") || strings.Contains(q, "回滚"):
		p, err := ListSnapshots(1, 100, nil)
		if err != nil {
			return nil, err
		}
		var bd strings.Builder
		fmt.Fprintf(&bd, "当前共 %d 条快照:", p.Total)
		for i, s := range p.List {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&bd, "\n%d. %s(%s 序号 %d,%s,%s)", i+1, s["snapshot_no"], s["cluster_ref"], Int(s["revision_no"]), s["snapshot_type"], s["change_summary"])
		}
		return &AssistantReply{Answer: bd.String(), Action: "/clc/snap",
			Hints: []string{"回滚校验是什么？", "放弃引用回滚怎么用？"}}, nil
	case strings.Contains(q, "任务") || strings.Contains(q, "安装") || strings.Contains(q, "升级") || strings.Contains(q, "备份"):
		p, err := ListClusterTasks(1, 100, nil)
		if err != nil {
			return nil, err
		}
		var bd strings.Builder
		fmt.Fprintf(&bd, "当前共 %d 条任务台账:", p.Total)
		for i, s := range p.List {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&bd, "\n%d. %s(%s@%s,%s,%s,%s)", i+1, s["task_no"], s["cluster_ref"], s["task_kind"], s["execution_path"], s["task_status"], orDefaultStrAny(s["fail_reason"], "-"))
		}
		return &AssistantReply{Answer: bd.String(), Action: "/clc/orch",
			Hints: []string{"安装失败怎么续跑？", "我有哪些待办？"}}, nil
	case strings.Contains(q, "探活") || strings.Contains(q, "健康") || strings.Contains(q, "降级"):
		rows, err := AggregateHealth(nil)
		if err != nil {
			return nil, err
		}
		var bd strings.Builder
		fmt.Fprintf(&bd, "聚合健康(共 %d 个集群):", len(rows))
		n := 0
		for _, s := range rows {
			n++
			if n > 8 {
				break
			}
			fmt.Fprintf(&bd, "\n%d. %s(%s,健康 %s,降级原因 %s,就绪节点 %v/%v)", n, s["cluster_no"], s["status"], s["health"], orDefaultStrAny(s["degraded_reason"], "-"), s["healthy_nodes"], s["total_nodes"])
		}
		return &AssistantReply{Answer: bd.String(), Action: "/clc/health",
			Hints: []string{"显式恢复怎么操作？", "探活防抖阈值是多少？"}}, nil
	case strings.Contains(q, "销毁") || strings.Contains(q, "审批") || strings.Contains(q, "怎么操作") || strings.Contains(q, "如何"):
		return &AssistantReply{
			Answer: "集群销毁审批(单级)操作指引:① 集群管理员从集群列表行操作「台」进入集群台,在「生命周期」页签的动作卡点击「提交销毁申请」,弹层先清点影响面(引用台账 + 节点 + 证书清单,R-10)并强制展示;② 填写事由(必填,R-24)后提交,生成审批单,审批通过前集群状态不变;③ 平台运维管理员(CLC-ROLE-02)在「审批中心-我的待办」通过或驳回(强制不免审批);④ 通过后自动创建 DESTROY 任务并执行销毁编排(R-07):级联清理引用台账 → 状态置 DESTROYED(终态保留行)→ 发布销毁事件;不删他域资源。",
			Action: "/workbench/todo", Hints: []string{"我有哪些待办？", "集群台账有哪些？"},
		}, nil
	case strings.Contains(q, "恢复") || strings.Contains(q, "降级原因"):
		return &AssistantReply{
			Answer: "健康双源口径(R-08/INV-06):探活连续失败 ⇒ UNREACHABLE(阈值 REF-03,缺省 3 次);连续成功 ⇒ HEALTHY(防抖阈值 REF-11,缺省 3 次);探活不覆盖 DEGRADED;DEGRADED 只能被显式恢复清除(FUNC-16):重绑定模式须提供新的可用引用(双写 + 派生参数),确认模式直接清除降级并留痕。",
			Action: "/clc/health", Hints: []string{"聚合健康怎么查？", "探活历史有哪些？"},
		}, nil
	case strings.Contains(q, "管理台") || strings.Contains(q, "管理控制台") || strings.Contains(lq, "admin"):
		return &AssistantReply{
			Answer: "管理控制台(/admin)面向系统管理员:仪表盘、用户/角色/权限/资源管理、流程管理、日志审计与 AI 模型配置。工作台右上角头像菜单可直达。",
			Action: "/admin/dashboard", Hints: []string{"我有哪些待办？"},
		}, nil
	case strings.Contains(lq, "select") || strings.Contains(q, "统计"):
		return &AssistantReply{
			Answer: "您可以直接输入以 SELECT 开头的只读 SQL(白名单表:clc_cluster、clc_cluster_node、clc_binding、clc_param、clc_snapshot、clc_task、clc_probe、clc_platform_param),系统会以表格返回结果并自动追加 LIMIT。例如:\nSELECT status, COUNT(*) AS cnt FROM clc_cluster GROUP BY status",
			Hints:  []string{"SELECT cluster_no, name, status, health FROM clc_cluster LIMIT 20"},
		}, nil
	default:
		return &AssistantReply{
			Answer: "我是 OPIC 集群域助理,可以帮您:查待办审批、盘点集群/节点/引用/快照/任务台账、解释销毁审批与健康恢复规则、或执行只读统计 SQL(直接输入 SELECT 语句)。试试下方建议问题。",
			Hints:  clcHints,
		}, nil
	}
}

// extractSQLBlock 提取 sql 代码块。
func extractSQLBlock(text string) string {
	re := regexp.MustCompile("(?s)```sql\\s*(.*?)```")
	if m := re.FindStringSubmatch(text); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	re2 := regexp.MustCompile("(?s)```\\s*(SELECT.*?)```")
	if m := re2.FindStringSubmatch(text); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// clcSQLReply 只读 SQL 工具应答(守卫失败给中文提示;成功返回表格)。
func clcSQLReply(uid int64, username, sql string) *AssistantReply {
	table, _, err := RunReadOnlySQL(uid, username, sql)
	if err != nil {
		return &AssistantReply{Answer: "只读查询未执行:" + err.Error(), Hints: clcHints}
	}
	return &AssistantReply{
		Answer: fmt.Sprintf("已执行只读查询(自动追加 LIMIT %d),共返回 %d 行:", ReadOnlySQLLimit, len(table.Rows)),
		Table:  table, Hints: clcHints,
	}
}

// clcLLMAnswer LLM 两段式:①提问(可回 SQL 代码块)→ ②执行结果回灌 → 中文总结。
// 安全边界不变:SQL 仍经 RunReadOnlySQL(白名单 + LIMIT + 审计)。返回 ok=false 表示降级。
func clcLLMAnswer(uid int64, username string, acct *llmcfg.Account, model string, temperature float64, prompt, question string) (*AssistantReply, bool) {
	if model == "" {
		model = acct.DefaultModelResolve()
	}
	messages := []llmcfg.Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: question},
	}
	answer, err := llmcfg.ChatAccount(context.Background(), acct, model, temperature, messages)
	if err != nil {
		log.Printf("[assistant] LLM(%s/%s) 调用失败,降级规则匹配: %v", acct.Name, model, err)
		return nil, false
	}
	if sql := extractSQLBlock(answer); sql != "" {
		table, guarded, qerr := RunReadOnlySQL(uid, username, sql)
		if qerr != nil {
			return &AssistantReply{Answer: "查询未执行:" + qerr.Error() + "\n\n" + plainClcAnswer(answer), Hints: clcHints}, true
		}
		resultJSON, _ := json.Marshal(map[string]any{"sql": guarded, "rows": table.Rows, "row_count": len(table.Rows)})
		messages = append(messages,
			llmcfg.Message{Role: "assistant", Content: answer},
			llmcfg.Message{Role: "user", Content: "查询结果:\n" + string(resultJSON) + "\n请用中文总结。"})
		final, err2 := llmcfg.ChatAccount(context.Background(), acct, model, temperature, messages)
		if err2 != nil {
			return &AssistantReply{Answer: plainClcAnswer(answer), Table: table, Hints: clcHints}, true
		}
		return &AssistantReply{Answer: final, Table: table, Hints: clcHints}, true
	}
	return &AssistantReply{Answer: answer, Hints: clcHints}, true
}

// plainClcAnswer 去除 SQL 代码块后的纯文字(第二段总结失败时的兜底)。
func plainClcAnswer(s string) string {
	re := regexp.MustCompile("(?s)```sql.*?```")
	return strings.TrimSpace(re.ReplaceAllString(s, ""))
}

var clcHints = []string{"我有哪些待办？", "集群台账有哪些？", "任务台账有哪些？", "聚合健康怎么查？"}

func orDefaultStrAny(v any, def string) string {
	if s := Str(v); s != "" {
		return s
	}
	return def
}

// pickAccount 选定供应商账号(accountID=0 → 首个启用账号;指定 id 须已启用)。
func pickAccount(accountID int64) *llmcfg.Account {
	accts := llmcfg.EnabledAccounts()
	if len(accts) == 0 {
		return nil
	}
	if accountID == 0 {
		return &accts[0]
	}
	for i := range accts {
		if accts[i].ID == accountID {
			return &accts[i]
		}
	}
	return nil
}

// AssistantModels 登录用户可选的模型清单(启用账号 × 模型目录)。
type AssistantModel struct {
	AccountID    int64    `json:"account_id"`
	AccountName  string   `json:"account_name"`
	Provider     string   `json:"provider"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"default_model"`
}

// AssistantModelOptions 助理模型选择器数据源。
func AssistantModelOptions() []AssistantModel {
	accts := llmcfg.EnabledAccounts()
	out := make([]AssistantModel, 0, len(accts))
	for _, a := range accts {
		out = append(out, AssistantModel{
			AccountID: a.ID, AccountName: a.Name, Provider: a.Provider,
			Models: a.ModelList(), DefaultModel: a.DefaultModelResolve(),
		})
	}
	return out
}
