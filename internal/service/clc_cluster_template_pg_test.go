// clc_cluster_template_pg_test.go —— DEF-07 补齐的模板实例化腿（R-20 / FUNC-01 / INV-49）用例。
//
// 口径来源：spec 2.10「仅 ENABLED 可被向导实例化」+ R-20「一次性展开三个面（引用 / 拓扑 /
// 参数默认值），建成后不回写不跟随」+ INV-49（template_ref 只在实例化瞬间写一次）+
// INV-08（凭据值零出口，展开侧兜底）。
// 断言只匹配错误码 + 关键短词（中文判据字面量会润色，不抄长句）。
package service

import (
	"fmt"
	"strings"
	"testing"

	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// instFakeCred 假的凭据形态值（命中 delivery.MaskSecret 的 token: 赋值判据，内容不是真凭据）。
const instFakeCred = "token: ssh-key-FAKE-NOT-A-CRED"

// instTplEnabled 登记一张带版本引用 + 两组节点组 + 指定参数行的 ENABLED 模板（实例化数据源）。
func instTplEnabled(t *testing.T, tag, versionRef string, params []any) map[string]any {
	t.Helper()
	out, err := CreateClcTemplate(map[string]any{
		"name":        tplName(tag),
		"version_ref": versionRef,
		"node_groups": []any{
			map[string]any{"group_name": "主控组", "node_role": "MASTER", "expected_count": 3},
			map[string]any{"group_name": "工作组", "node_role": "WORKER", "expected_count": 5},
		},
		"params": params,
	}, tplUser)
	if err != nil {
		t.Fatalf("登记模板失败: %v", err)
	}
	no := Str(out["template_no"])
	if _, err := SetClcTemplateState(no, "enable", "实例化用例启用", tplUser); err != nil {
		t.Fatalf("启用模板失败: %v", err)
	}
	d, err := GetClcTemplate(no)
	if err != nil {
		t.Fatalf("回读模板失败: %v", err)
	}
	return d
}

// instWantErrCode 期望报错并核对错误码 + 关键短词（片段命中即可，不抄长句）。
func instWantErrCode(t *testing.T, err error, code errcode.ErrCode, frags ...string) string {
	t.Helper()
	if err == nil {
		t.Fatalf("期望报错，实为 nil")
	}
	be := AsBizErr(err)
	if be.Code != code {
		t.Fatalf("错误码应为 %s，实为 %s（%s）", code, be.Code, be.Msg)
	}
	for _, f := range frags {
		if !strings.Contains(be.Msg, f) {
			t.Fatalf("报错文案应含 %q，实际: %s", f, be.Msg)
		}
	}
	return be.Msg
}

// instNoWrite 拒绝路径的零写入断言：集群行与参数行总数不变（同事务回滚）。
func instNoWrite(t *testing.T, wantClusters, wantParams int64) {
	t.Helper()
	if got := countRows(t, `SELECT COUNT(*) FROM clc_cluster WHERE flag=1`); got != wantClusters {
		t.Fatalf("实例化被拒后不得新增集群行（事务内零写入）: 期望 %d，实为 %d", wantClusters, got)
	}
	if got := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE flag=1`); got != wantParams {
		t.Fatalf("实例化被拒后不得新增参数行（事务内零写入）: 期望 %d，实为 %d", wantParams, got)
	}
}

// ---------- 成功腿：template_ref 落库 + 版本引用展开 + 参数默认值 TEMPLATE ----------

func TestCreateClusterInstantiateTemplate(t *testing.T) {
	requireDB(t)
	versionRef := fmt.Sprintf("IGN-VER-INST-%d", nextSeq())
	tpl := instTplEnabled(t, "inst", versionRef, []any{
		map[string]any{"param_key": "net.cni", "param_value": "calico"},
		map[string]any{"param_key": "site.field", "param_value": ""}, // 空串 = 须现场填（合法配方值）
	})
	no := Str(tpl["template_no"])
	name := fmt.Sprintf("模板实例化-%d", nextSeq())
	// 请求刻意不带 version_ref：模板带 ⇒ 取模板的；同时证明引用展开发生在
	// checkVersionRef(REF-05)/checkRefConsistency(INV-04)之前（否则 MCC 无引用即被拒）。
	created, err := CreateCluster(map[string]any{
		"name": name, "cluster_dns_domain": "tpl-inst.test", "template_ref": no,
		"nodes": []any{map[string]any{"ip_address": "10.44.0.11", "node_role": "MASTER"}},
	}, testUser)
	if err != nil {
		t.Fatalf("按 ENABLED 模板实例化应成功: %v", err)
	}
	clusterNo := Str(created["cluster_no"])
	if Str(created["template_ref"]) != no {
		t.Fatalf("来源模板引用应落库（INV-49 实例化瞬间写一次），实为 %q", Str(created["template_ref"]))
	}
	if Str(created["version_ref"]) != versionRef {
		t.Fatalf("请求缺 version_ref 时应取模板值，期望 %q 实为 %q", versionRef, Str(created["version_ref"]))
	}
	if Str(created["status"]) != ClusterStatusDraft {
		t.Fatalf("创建落 DRAFT 口径不变，实为 %q", Str(created["status"]))
	}
	// 参数面展开：整组 TEMPLATE，空串值原样保留（不是缺失）
	rows := mustList(t, `SELECT param_key, param_value, param_source, sync_pending FROM clc_param
		WHERE cluster_ref=? AND flag=1 ORDER BY param_key`, clusterNo)
	byKey := map[string]map[string]any{}
	for _, r := range rows {
		byKey[Str(r["param_key"])] = r
	}
	if len(rows) != 2 {
		t.Fatalf("模板两条参数行应整组展开（无镜像源引用 ⇒ 无派生行），实际 %v", rows)
	}
	if Str(byKey["net.cni"]["param_value"]) != "calico" || Str(byKey["net.cni"]["param_source"]) != ParamSourceTemplate {
		t.Fatalf("net.cni 应为 TEMPLATE 来源的 calico，实为 %v", byKey["net.cni"])
	}
	site := byKey["site.field"]
	if Str(site["param_value"]) != "" || Str(site["param_source"]) != ParamSourceTemplate {
		t.Fatalf("空串默认值须原样保留为「须现场填」行（不得丢行），实为 %v", site)
	}
	// 拓扑面：模板节点组不在服务端造节点行（ip_address NOT NULL + spec 2.11 ⑤ 模板不含地址）
	if n := countRows(t, `SELECT COUNT(*) FROM clc_cluster_node WHERE cluster_ref=? AND flag=1`, clusterNo); n != 1 {
		t.Fatalf("节点行只能来自请求 nodes（模板只给向导第二步的预填配方），实为 %d", n)
	}
	// 审计出「来源模板」且不带凭据
	audit := mustOne(t, `SELECT detail FROM audit_logs WHERE action='FUNC-01' AND detail LIKE ? ORDER BY id DESC LIMIT 1`,
		"%"+clusterNo+"%")
	if d := Str(audit["detail"]); !strings.Contains(d, no) || !strings.Contains(d, "来源模板") {
		t.Fatalf("审计须登记来源模板: %s", d)
	}
}

// ---------- 请求同键优先 + 请求独有键 ----------

func TestCreateClusterTemplateRequestParamsWin(t *testing.T) {
	requireDB(t)
	tpl := instTplEnabled(t, "win", "", []any{
		map[string]any{"param_key": "net.cni", "param_value": "calico"},
		map[string]any{"param_key": "sched.pod", "param_value": "110"},
	})
	no := Str(tpl["template_no"])
	created, err := CreateCluster(map[string]any{
		"name": fmt.Sprintf("模板同键优先-%d", nextSeq()), "template_ref": no,
		"version_ref": "IGN-VER-FROM-REQ-0001", // 请求已带 ⇒ 不被模板覆盖（本模板也无版本引用）
		"params": []any{
			map[string]any{"param_key": "NET.CNI", "param_value": "cilium"}, // 大小写同键（uq 索引按 LOWER）
			map[string]any{"param_key": "site.extra", "param_value": "hand"},
		},
	}, testUser)
	if err != nil {
		t.Fatalf("实例化失败: %v", err)
	}
	clusterNo := Str(created["cluster_no"])
	if Str(created["version_ref"]) != "IGN-VER-FROM-REQ-0001" {
		t.Fatalf("请求已带版本引用时以请求为准，实为 %q", Str(created["version_ref"]))
	}
	rows := mustList(t, `SELECT param_key, param_value, param_source FROM clc_param WHERE cluster_ref=? AND flag=1`, clusterNo)
	if len(rows) != 3 {
		t.Fatalf("应恰好 3 行（模板补默认 sched.pod + 请求 2 行），实为 %v", rows)
	}
	for _, r := range rows {
		switch strings.ToLower(Str(r["param_key"])) {
		case "net.cni":
			if Str(r["param_value"]) != "cilium" || Str(r["param_source"]) != ParamSourceOverride {
				t.Fatalf("同键以请求为准（人工现场值），实为 %v", r)
			}
		case "sched.pod":
			if Str(r["param_value"]) != "110" || Str(r["param_source"]) != ParamSourceTemplate {
				t.Fatalf("模板默认值应补位，实为 %v", r)
			}
		case "site.extra":
			if Str(r["param_value"]) != "hand" || Str(r["param_source"]) != ParamSourceOverride {
				t.Fatalf("请求独有键应落库，实为 %v", r)
			}
		default:
			t.Fatalf("不该出现的参数键: %v", r)
		}
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE cluster_ref=? AND flag=1 AND LOWER(param_key)='net.cni'`,
		clusterNo); n != 1 {
		t.Fatalf("大小写同键只能一行（INV-20 / uq_param_cluster_key），实为 %d", n)
	}
	// 请求内同键重复即拒（不进事务，零写入）
	c0 := countRows(t, `SELECT COUNT(*) FROM clc_cluster WHERE flag=1`)
	p0 := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE flag=1`)
	_, err = CreateCluster(map[string]any{
		"name": fmt.Sprintf("请求键重复-%d", nextSeq()), "template_ref": no, "version_ref": "IGN-VER-DUP-0001",
		"params": []any{map[string]any{"param_key": "a.k", "param_value": "1"},
			map[string]any{"param_key": "A.K", "param_value": "2"}},
	}, testUser)
	instWantErrCode(t, err, errcode.ErrRule, "INV-20", "重复")
	instNoWrite(t, c0, p0)
}

// ---------- 与 R-04 派生同事务共存（不撞 uq_param_cluster_key）----------

func TestCreateClusterTemplateDeriveCoexist(t *testing.T) {
	requireDB(t)
	tpl := instTplEnabled(t, "coexist", "", []any{
		map[string]any{"param_key": "image.mirror.endpoint", "param_value": "harbor.old/stale"},
		map[string]any{"param_key": "net.cni", "param_value": "calico"},
	})
	created, err := CreateCluster(map[string]any{
		"name": fmt.Sprintf("模板派生共存-%d", nextSeq()), "template_ref": Str(tpl["template_no"]),
		"registry_ref": "HARBOR-INST-01", "registry_project_ref": "opic",
	}, testUser)
	if err != nil {
		t.Fatalf("模板展开与 R-04 派生同事务应共存: %v", err)
	}
	clusterNo := Str(created["cluster_no"])
	rows := mustList(t, `SELECT param_key, param_value, param_source, sync_pending, derive_note FROM clc_param
		WHERE cluster_ref=? AND flag=1 ORDER BY param_key`, clusterNo)
	seen := map[string]int{}
	for _, r := range rows {
		seen[strings.ToLower(Str(r["param_key"]))]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Fatalf("同键只能一行（uq_param_cluster_key）: %s 有 %d 行", k, n)
		}
	}
	// 派生固定映射（REF-06）以实际引用为权威：命中已有行就地更新值 + 置待同步，不新增行
	if n := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE cluster_ref=? AND flag=1 AND param_key=?
		AND param_value=? AND sync_pending=1`, clusterNo, "image.mirror.endpoint", "HARBOR-INST-01/opic"); n != 1 {
		t.Fatalf("镜像源端点应按既有派生口径落值（不新增行、置待同步），实得 %d 行 / %v", n, rows)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE cluster_ref=? AND flag=1
		AND param_source=? AND param_key=?`, clusterNo, ParamSourceTemplate, "net.cni"); n != 1 {
		t.Fatalf("非派生键仍按 TEMPLATE 展开: %d", n)
	}
	if countRows(t, `SELECT COUNT(*) FROM clc_param WHERE cluster_ref=? AND flag=1 AND sync_pending=1
		AND TRIM(COALESCE(derive_note,''))=''`, clusterNo) != 0 {
		t.Fatalf("INV-22：待同步行必须有派生注记")
	}
}

// ---------- 四种拒绝：DRAFT / DISABLED / 不存在 / 已软删，事务内零写入 ----------

func TestCreateClusterTemplateRejects(t *testing.T) {
	requireDB(t)
	base := tplSeed(t, "draft")
	baseNo := Str(base["template_no"])
	// 草稿未启用：先造一张停用态（从启用转到停用）
	off := instTplEnabled(t, "off", "", []any{map[string]any{"param_key": "a.b", "param_value": "c"}})
	offNo := Str(off["template_no"])
	if _, err := SetClcTemplateState(offNo, "disable", "实例化用例停用", tplUser); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	deleted := instTplEnabled(t, "deleted", "", []any{map[string]any{"param_key": "a.b", "param_value": "c"}})
	deletedNo := Str(deleted["template_no"])
	mustSQL(t, `UPDATE clc_cluster_template SET flag = 0 WHERE template_no = ?`, deletedNo)

	c0 := countRows(t, `SELECT COUNT(*) FROM clc_cluster WHERE flag=1`)
	p0 := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE flag=1`)
	missNo := fmt.Sprintf("TPL-19700101-%d", nextSeq())
	cases := []struct {
		name  string
		tpl   string
		code  errcode.ErrCode
		frags []string
	}{
		{"草稿", baseNo, errcode.ErrState, []string{"草稿"}},
		{"停用", offNo, errcode.ErrState, []string{"停用"}},
		{"不存在", missNo, errcode.ErrNotFound, []string{"不存在"}},
		{"已软删", deletedNo, errcode.ErrNotFound, []string{"不存在"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CreateCluster(map[string]any{
				"name": fmt.Sprintf("实例化拒绝-%s-%d", tc.name, nextSeq()), "template_ref": tc.tpl,
			}, testUser)
			msg := instWantErrCode(t, err, tc.code, tc.frags...)
			if !strings.Contains(msg, "模板") {
				t.Fatalf("拒绝理由要指明是模板腿的问题: %s", msg)
			}
			instNoWrite(t, c0, p0)
		})
	}
	// 停用态与草稿态都给「当前是什么态」的可读文案（spec 2.10 + INV-51 两态语义不同）
	_, err := CreateCluster(map[string]any{"name": fmt.Sprintf("实例化文案-%d", nextSeq()), "template_ref": baseNo}, testUser)
	if msg := instWantErrCode(t, err, errcode.ErrState, "仅启用", "ENABLED"); !strings.Contains(msg, "当前是") {
		t.Fatalf("文案要说明当前态: %s", msg)
	}
	// 未选模板（空值）⇒ 既有手填路径逐字不变：template_ref 为空、无 TEMPLATE 行
	created, err := CreateCluster(map[string]any{
		"name": fmt.Sprintf("无模板手填-%d", nextSeq()), "version_ref": "IGN-VER-MANUAL-0001",
	}, testUser)
	if err != nil {
		t.Fatalf("无模板路径应照旧成功: %v", err)
	}
	if v := Str(created["template_ref"]); v != "" {
		t.Fatalf("未走模板时 template_ref 应为空（INV-49「空 = 未走模板」），实为 %q", v)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE cluster_ref=? AND flag=1 AND param_source=?`,
		Str(created["cluster_no"]), ParamSourceTemplate); n != 0 {
		t.Fatalf("无模板路径不得展开 TEMPLATE 行: %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_cluster WHERE cluster_no=? AND template_ref IS NULL`,
		Str(created["cluster_no"])); n != 1 {
		t.Fatalf("未走模板应落 NULL: %d", n)
	}
	// 建成后无改绑口：FUNC-02 不写 template_ref（INV-49 只读）
	if _, err := UpdateCluster(Int(created["id"]), map[string]any{
		"name": Str(created["name"]), "description": "改绑试试", "template_ref": baseNo,
		"version_ref": "IGN-VER-MANUAL-0001",
	}, testUser); err != nil {
		t.Fatalf("信息修改应照旧成功: %v", err)
	}
	if v := Str(mustOne(t, `SELECT template_ref AS v FROM clc_cluster WHERE cluster_no=?`,
		Str(created["cluster_no"]))["v"]); v != "" {
		t.Fatalf("INV-49：建成后 template_ref 只读，不得被 FUNC-02 改绑，实为 %q", v)
	}
}

// ---------- INV-08 展开侧兜底：模板参数值命中凭据形态 ⇒ 整条拒绝、零出口 ----------

func TestCreateClusterTemplateCredentialShape(t *testing.T) {
	requireDB(t)
	tpl := instTplEnabled(t, "cred", "", []any{map[string]any{"param_key": "net.cni", "param_value": "calico"}})
	no := Str(tpl["template_no"])
	// 构造脏配方行（登记侧本会拒收，这里模拟库外直写的历史脏值，验证展开侧兜底）
	mustSQL(t, `INSERT INTO clc_template_param (template_ref, param_key, param_value, sort_no) VALUES (?, ?, ?, 99)`,
		no, "boot.token", instFakeCred)
	c0 := countRows(t, `SELECT COUNT(*) FROM clc_cluster WHERE flag=1`)
	p0 := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE flag=1`)
	_, err := CreateCluster(map[string]any{
		"name": fmt.Sprintf("模板凭据拒绝-%d", nextSeq()), "template_ref": no,
		"version_ref": "IGN-VER-CRED-0001", // 引用齐备，让请求走到展开腿（否则先撞 INV-04）
	}, testUser)
	msg := instWantErrCode(t, err, errcode.ErrRule, "INV-08", "凭据形态")
	if strings.Contains(msg, instFakeCred) || strings.Contains(msg, "ssh-key-FAKE") {
		t.Fatalf("报错文案不得带出凭据值（INV-08 零出口）: %s", msg)
	}
	if !strings.Contains(msg, "boot.token") {
		t.Fatalf("报错要指到命中的参数键，给得出下一步动作: %s", msg)
	}
	instNoWrite(t, c0, p0)
	if n := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE flag=1 AND param_value LIKE ?`, "%ssh-key-FAKE%"); n != 0 {
		t.Fatalf("凭据形态值不得顺着展开进集群参数: %d", n)
	}
}
