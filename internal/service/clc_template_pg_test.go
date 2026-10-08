// clc_template_pg_test.go —— v1.2 集群模板（FUNC-36~43）服务入口语义用例（依赖临时 PG 装置）。
//
// 断言的错误文案只匹配关键片段（中文口径会润色，编号/规则号/键名才是可断言的事实）。
package service

import (
	"fmt"
	"strings"
	"testing"
)

var tplUser = UserInfo{ID: 7, Username: "tpl-tester"}

func tplName(tag string) string {
	return fmt.Sprintf("tpl-%s-%d", tag, nextSeq())
}

// tplWantErr 期望报错并核对关键片段；err 为 nil 时直接判失败（不许静默放行）。
func tplWantErr(t *testing.T, err error, frags ...string) string {
	t.Helper()
	if err == nil {
		t.Fatalf("期望报错，实为 nil")
	}
	msg := AsBizErr(err).Msg
	for _, f := range frags {
		if !strings.Contains(msg, f) {
			t.Fatalf("报错文案应含 %q，实际: %s", f, msg)
		}
	}
	return msg
}

// tplSeed 登记一条含 MASTER+WORKER 的草稿模板。
func tplSeed(t *testing.T, tag string) map[string]any {
	t.Helper()
	out, err := CreateClcTemplate(map[string]any{
		"name":        tplName(tag),
		"description": "用例模板 " + tag,
		"node_groups": []any{
			map[string]any{"group_name": "主控组", "node_role": "MASTER", "expected_count": 1},
			map[string]any{"group_name": "工作组", "node_role": "WORKER", "expected_count": 2},
		},
		"params": []any{map[string]any{"param_key": "net.cni", "param_value": "calico", "description": "CNI"}},
	}, tplUser)
	if err != nil {
		t.Fatalf("登记模板失败: %v", err)
	}
	return out
}

// tplSeedEnabled 登记并启用一条合法草稿（启用前置在 tplSeed 里已满足）。
func tplSeedEnabled(t *testing.T, tag string) map[string]any {
	t.Helper()
	out := tplSeed(t, tag)
	no := Str(out["template_no"])
	if _, err := SetClcTemplateState(no, "enable", "用例启用", tplUser); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	enabled, err := GetClcTemplate(no)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	return enabled
}

// ---------- FUNC-38 登记 + INV-51 名称唯一 + INV-08 凭据拒收 ----------

func TestClcTemplateCreate(t *testing.T) {
	requireDB(t)
	tpl := tplSeed(t, "c38")
	no := Str(tpl["template_no"])
	if !strings.HasPrefix(no, "TPL-") {
		t.Fatalf("编号应为 TPL-YYYYMMDD-NNNN 形态，实际 %q", no)
	}
	if Str(tpl["template_status"]) != TemplateStatusDraft || truthy(tpl["is_default"]) {
		t.Fatalf("登记后置草稿且默认位为假: %v/%v", tpl["template_status"], tpl["is_default"])
	}
	groups, _ := tpl["node_groups"].([]map[string]any)
	if len(groups) != 2 {
		t.Fatalf("节点组应同事务写入 2 行，实际 %d", len(groups))
	}
	// 重名即拒（未删除范围内唯一，不区分大小写）
	_, err := CreateClcTemplate(map[string]any{"name": Str(tpl["name"])}, tplUser)
	tplWantErr(t, err, "INV-51", "已存在")
	// 名称必填
	_, err = CreateClcTemplate(map[string]any{"name": ""}, tplUser)
	tplWantErr(t, err, "必填")
	// 同角色多行（INV-52 保存即校验）
	_, err = CreateClcTemplate(map[string]any{"name": tplName("duprole"), "node_groups": []any{
		map[string]any{"group_name": "a", "node_role": "MASTER", "expected_count": 1},
		map[string]any{"group_name": "b", "node_role": "MASTER", "expected_count": 2},
	}}, tplUser)
	tplWantErr(t, err, "同角色", "至多一行")
	// 组名重复
	_, err = CreateClcTemplate(map[string]any{"name": tplName("dupgroup"), "node_groups": []any{
		map[string]any{"group_name": "a", "node_role": "MASTER", "expected_count": 1},
		map[string]any{"group_name": "A", "node_role": "WORKER", "expected_count": 2},
	}}, tplUser)
	tplWantErr(t, err, "INV-52", "重复")
	// 期望数量 < 1
	_, err = CreateClcTemplate(map[string]any{"name": tplName("badcnt"), "node_groups": []any{
		map[string]any{"group_name": "a", "node_role": "MASTER", "expected_count": 0},
	}}, tplUser)
	tplWantErr(t, err, "INV-52", "不小于 1")
	// 凭据形态值不进配方（INV-08：拒收而不是掩码入库——掩码值会被当成默认值传染每次实例化）
	_, err = CreateClcTemplate(map[string]any{"name": tplName("cred"), "params": []any{
		map[string]any{"param_key": "boot.token", "param_value": "token: abcdefghijkl12345678"},
	}}, tplUser)
	tplWantErr(t, err, "INV-08", "凭据形态")
	// 空串参数值是合法口径（须现场填），不该被拒
	if _, err := CreateClcTemplate(map[string]any{"name": tplName("emptyval"), "params": []any{
		map[string]any{"param_key": "site.field", "param_value": ""},
	}}, tplUser); err != nil {
		t.Fatalf("空串值（现场填）不应被拒: %v", err)
	}
	// O-IGN 参数面引用只透传标识（R-21）：给什么存什么，不解析内容
	mark, err := CreateClcTemplate(map[string]any{"name": tplName("r21"), "param_template_ref": "TMP-20260101-0007"}, tplUser)
	if err != nil || Str(mark["param_template_ref"]) != "TMP-20260101-0007" {
		t.Fatalf("参数面引用应原样透传: %v / %v", err, mark)
	}
}

// ---------- FUNC-39 整组编辑（INV-52 整组替换，旧行 flag=0 保留） ----------

func TestClcTemplateGroupsWholeReplace(t *testing.T) {
	requireDB(t)
	tpl := tplSeed(t, "c39")
	no := Str(tpl["template_no"])
	// 缺事由即拒
	_, err := SaveClcTemplateGroups(no, map[string]any{"node_groups": []any{}, "params": []any{}}, tplUser)
	tplWantErr(t, err, "事由")

	out, err := SaveClcTemplateGroups(no, map[string]any{
		"reason": "拓扑调整：换网络插件键、砍工作组",
		"node_groups": []any{
			map[string]any{"group_name": "主控组", "node_role": "MASTER", "expected_count": 3},
		},
		"params": []any{
			map[string]any{"param_key": "net.kube_proxy", "param_value": "ipvs"},
		},
	}, tplUser)
	if err != nil {
		t.Fatalf("整组编辑失败: %v", err)
	}
	groups, _ := out["node_groups"].([]map[string]any)
	if len(groups) != 1 || paramInt(groups[0]["expected_count"]) != 3 {
		t.Fatalf("未提交的角色行即不存在（整组替换）: %v", groups)
	}
	params, _ := out["params"].([]map[string]any)
	if len(params) != 1 || Str(params[0]["param_key"]) != "net.kube_proxy" {
		t.Fatalf("参数行应整组替换: %v", params)
	}
	// 历史行以 flag=0 保留（INV-52 级联是逻辑删除）
	if n := mustOne(t, `SELECT COUNT(*) AS n FROM clc_template_param WHERE template_ref=? AND flag=0`, no); paramInt(n["n"]) < 1 {
		t.Fatalf("旧参数行应 flag=0 留档而不是物理删除")
	}
	// 模板头就地更新，状态不回草稿
	out, err = SaveClcTemplateGroups(no, map[string]any{
		"reason": "改版本引用", "version_ref": "IGN-VER-0000123",
		"node_groups": []any{map[string]any{"group_name": "主控组", "node_role": "MASTER", "expected_count": 3}},
	}, tplUser)
	if err != nil || Str(out["version_ref"]) != "IGN-VER-0000123" || Str(out["template_status"]) != TemplateStatusDraft {
		t.Fatalf("头字段应就地更新且草稿态不因编辑漂移: %v / %v", err, out)
	}
	// 编辑不触碰任何已建成集群（R-20）：集群参数表不因模板编辑而生灭
	before := mustOne(t, `SELECT COUNT(*) AS n FROM clc_param`)["n"]
	if _, err := SaveClcTemplateGroups(no, map[string]any{"reason": "再编辑", "params": []any{}}, tplUser); err != nil {
		t.Fatalf("编辑失败: %v", err)
	}
	if got := mustOne(t, `SELECT COUNT(*) AS n FROM clc_param`)["n"]; fmt.Sprint(got) != fmt.Sprint(before) {
		t.Fatalf("模板编辑不得改动集群参数（R-20 建成后不回写）")
	}
}

// ---------- FUNC-40 启用 / 停用 + FUNC-41 设默认（INV-51 两条腿） ----------

func TestClcTemplateStateAndDefault(t *testing.T) {
	requireDB(t)
	mustSQL(t, `UPDATE clc_cluster_template SET is_default = FALSE WHERE flag = 1`)

	// 启用前置：节点组非空
	bare, err := CreateClcTemplate(map[string]any{"name": tplName("bare"), "params": []any{
		map[string]any{"param_key": "k", "param_value": "v"}}}, tplUser)
	if err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	_, err = SetClcTemplateState(Str(bare["template_no"]), "enable", "启用空模板", tplUser)
	tplWantErr(t, err, "至少一个 MASTER")
	// 启用前置：至少一个 MASTER 组
	nomaster := tplSeedNoGroups(t)
	_, err = SetClcTemplateState(Str(nomaster["template_no"]), "enable", "启用无主控", tplUser)
	tplWantErr(t, err, "至少一个 MASTER")

	// 启用/停用事由必填（治理动作口径）
	_, err = SetClcTemplateState(Str(bare["template_no"]), "enable", "", tplUser)
	tplWantErr(t, err, "事由")

	a := tplSeed(t, "dftA")
	aNo := Str(a["template_no"])
	if _, err := SetClcTemplateState(aNo, "enable", "用例启用", tplUser); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	// 重复启用即拒（迁移合法域固定）
	_, err = SetClcTemplateState(aNo, "enable", "再启用", tplUser)
	tplWantErr(t, err, "已处于启用态")
	b := tplSeed(t, "dftB")
	bNo := Str(b["template_no"])
	if _, err := SetClcTemplateState(bNo, "enable", "用例启用", tplUser); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	// 草稿项不可设默认（INV-51：默认行必须处于启用态——服务层把关，库侧谓词只兜「至多一行」）
	draft := tplSeed(t, "dftDraft")
	_, err = SetClcTemplateDefault(Str(draft["template_no"]), "给草稿设默认", tplUser)
	tplWantErr(t, err, "须处于启用态", "INV-51")
	// 设默认必带事由
	_, err = SetClcTemplateDefault(aNo, "", tplUser)
	tplWantErr(t, err, "事由")
	if _, err := SetClcTemplateDefault(aNo, "A 为缺省配方", tplUser); err != nil {
		t.Fatalf("设默认失败: %v", err)
	}
	if _, err := SetClcTemplateDefault(bNo, "B 顶替 A", tplUser); err != nil {
		t.Fatalf("设默认失败: %v", err)
	}
	// 原子「撤原默认 + 置新默认」：全局存活默认行恒为一行（与 uq_template_default 谓词同一半边口径）
	row := mustOne(t, `SELECT COUNT(*) AS n FROM clc_cluster_template WHERE flag = 1 AND is_default = TRUE`)
	if paramInt(row["n"]) != 1 {
		t.Fatalf("默认行全局至多一条且当前应恰有一条，实际 %v", row["n"])
	}
	if got := Str(mustOne(t, `SELECT template_no FROM clc_cluster_template WHERE flag=1 AND is_default=TRUE`)["template_no"]); got != bNo {
		t.Fatalf("原默认应被自动摘除，当前默认应是 %s，实为 %s", bNo, got)
	}
	// 默认项不可直接停用（须先撤默认）
	_, err = SetClcTemplateState(bNo, "disable", "停用默认", tplUser)
	tplWantErr(t, err, "默认模板不可直接停用", "先")
	// 撤默认后停用走通（把默认移交 A，B 即可停用）
	if _, err := SetClcTemplateDefault(aNo, "默认改回 A", tplUser); err != nil {
		t.Fatalf("改回默认失败: %v", err)
	}
	if _, err := SetClcTemplateState(bNo, "disable", "停用 B", tplUser); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	// 停用态编辑后仍是停用态（不回草稿）
	out, err := SaveClcTemplateGroups(bNo, map[string]any{"reason": "停用态编辑",
		"node_groups": []any{map[string]any{"group_name": "主控组", "node_role": "MASTER", "expected_count": 1}}}, tplUser)
	if err != nil || Str(out["template_status"]) != TemplateStatusDisabled {
		t.Fatalf("停用态编辑应保持停用: %v / %v", err, out)
	}
	// 停用→启用 是合法迁移
	if out, err := SetClcTemplateState(bNo, "enable", "重新启用", tplUser); err != nil || Str(out["template_status"]) != TemplateStatusEnabled {
		t.Fatalf("停用→启用应成功: %v", err)
	}
	// 草稿不涉及停用（DRAFT→DISABLED 非法迁移）
	if _, err := SetClcTemplateState(Str(draft["template_no"]), "disable", "停用草稿", tplUser); err == nil {
		t.Fatalf("草稿停用应被拒绝（INV-51 迁移固定）")
	}
}

// tplSeedNoGroups 登记只有 WORKER 组的模板（供「无主控不可启用」用例）。
func tplSeedNoGroups(t *testing.T) map[string]any {
	t.Helper()
	out, err := CreateClcTemplate(map[string]any{"name": tplName("nomaster"), "node_groups": []any{
		map[string]any{"group_name": "工作组", "node_role": "WORKER", "expected_count": 2}}}, tplUser)
	if err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	return out
}

// ---------- FUNC-42 软删除：引用保护列出处 + 默认位前置 + 名称释放 ----------

func TestClcTemplateDeleteProtection(t *testing.T) {
	requireDB(t)
	mustSQL(t, `UPDATE clc_cluster_template SET is_default = FALSE WHERE flag = 1`)
	tpl := tplSeedEnabled(t, "c42")
	no := Str(tpl["template_no"])
	// 缺事由即拒
	_, err := DeleteClcTemplate(no, "", tplUser)
	tplWantErr(t, err, "事由")
	// 被未销毁集群引用 ⇒ 拒绝并列出全部出处（出处指到下一步动作的位置）
	c1 := seedCluster(t, ClusterStatusRunning)
	c2 := seedCluster(t, ClusterStatusInstalling)
	mustSQL(t, `UPDATE clc_cluster SET template_ref = ? WHERE cluster_no IN (?, ?)`, no, c1, c2)
	_, err = DeleteClcTemplate(no, "删了试试", tplUser)
	text := tplWantErr(t, err, "不可删除", "R-22")
	if !strings.Contains(text, c1) || !strings.Contains(text, c2) {
		t.Fatalf("拒绝理由应列出全部引用集群（%s、%s），实际: %s", c1, c2, text)
	}
	if !strings.Contains(text, "集群列表") {
		t.Fatalf("出处要指到下一步动作的位置（集群列表屏按来源模板筛）: %s", text)
	}
	// 已销毁集群不算引用（R-22 反查口径：未销毁）
	mustSQL(t, `UPDATE clc_cluster SET status = ? WHERE cluster_no IN (?, ?)`, ClusterStatusDestroyed, c1, c2)
	// 默认项须先撤默认（INV-51 第二句由服务层把关）
	if _, err := SetClcTemplateDefault(no, "先设为默认", tplUser); err != nil {
		t.Fatalf("设默认失败: %v", err)
	}
	_, err = DeleteClcTemplate(no, "删默认项", tplUser)
	tplWantErr(t, err, "撤销默认")
	// 撤默认的服务层路径：把默认移交给另一张启用态模板（FUNC-41 自动摘旧默认）
	other := tplSeedEnabled(t, "c42other")
	otherNo := Str(other["template_no"])
	if _, err := SetClcTemplateDefault(otherNo, "默认移交", tplUser); err != nil {
		t.Fatalf("默认移交失败: %v", err)
	}
	// 删除成功：行保留 flag=0、子实体级联 flag=0、名称随删除释放
	if _, err := DeleteClcTemplate(no, "确认配方不再复用", tplUser); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}
	if n := mustOne(t, `SELECT COUNT(*) AS n FROM clc_cluster_template WHERE template_no=? AND flag=0`, no); paramInt(n["n"]) != 1 {
		t.Fatalf("软删除应行保留（flag=0），不物理删除")
	}
	if n := mustOne(t, `SELECT COUNT(*) AS n FROM clc_template_node_group WHERE template_ref=? AND flag=1`, no); paramInt(n["n"]) != 0 {
		t.Fatalf("子实体应随聚合级联 flag=0")
	}
	// 既有集群引用不失效（INV-49）：集群行的 template_ref 保持原值
	if got := Str(mustOne(t, `SELECT template_ref AS r FROM clc_cluster WHERE cluster_no = ?`, c1)["r"]); got != no {
		t.Fatalf("模板软删不得清空既有集群的 template_ref（INV-49），实际 %q", got)
	}
	// 默认位守恒：全局存活默认仍恰一行（在 other 上）
	if got := Str(mustOne(t, `SELECT template_no FROM clc_cluster_template WHERE flag=1 AND is_default=TRUE`)["template_no"]); got != otherNo {
		t.Fatalf("删除默认位撤销后的模板不应扰动当前默认 %s，实为 %s", otherNo, got)
	}
	// 重名登记（软删已释放名称）应成功
	if _, err := CreateClcTemplate(map[string]any{"name": Str(tpl["name"])}, tplUser); err != nil {
		t.Fatalf("软删除应释放名称（INV-51 未删除范围内唯一）: %v", err)
	}
}

// ---------- FUNC-43 克隆：从集群（凭据剔除）+ 从模板 + 产物一律草稿 ----------

func TestClcTemplateClone(t *testing.T) {
	requireDB(t)
	// 从既有集群：拓扑取节点角色分布（PENDING+ADDED），参数取当前值，凭据形态值剔除
	cNo := seedCluster(t, ClusterStatusRunning)
	mustSQL(t, `INSERT INTO clc_cluster_node (node_no, cluster_ref, node_role, ip_address, node_status)
		VALUES (?, ?, 'MASTER', '10.77.0.11', 'ADDED'), (?, ?, 'WORKER', '10.77.0.12', 'PENDING'),
		(?, ?, 'WORKER', '10.77.0.13', 'ADDED'), (?, ?, 'WORKER', '10.77.0.14', 'REMOVED')`,
		"NOD-CLONE-M", cNo, "NOD-CLONE-W1", cNo, "NOD-CLONE-W2", cNo, "NOD-CLONE-W9", cNo)
	mustSQL(t, `INSERT INTO clc_param (cluster_ref, param_key, param_value, param_source)
		VALUES (?, 'net.cni', 'calico', 'MANUAL_OVERRIDE'),
		(?, 'boot.token', 'token: abcdefghijkl12345678', 'MANUAL_OVERRIDE'),
		(?, 'site.field', '', 'MANUAL_OVERRIDE')`, cNo, cNo, cNo)
	clone, err := CloneClcTemplate(map[string]any{
		"name": tplName("from-cluster"), "source_cluster_no": cNo,
	}, tplUser)
	if err != nil {
		t.Fatalf("从集群克隆失败: %v", err)
	}
	if Str(clone["template_status"]) != TemplateStatusDraft || Str(clone["source_cluster_ref"]) != cNo {
		t.Fatalf("克隆产物应一律草稿并写来源集群留痕: %v", clone)
	}
	if s := Str(clone["role_summary"]); !strings.Contains(s, "MASTER×1") || !strings.Contains(s, "WORKER×2") {
		t.Fatalf("拓扑应取在册节点（REMOVED 不计）: %s", s)
	}
	params, _ := clone["params"].([]map[string]any)
	byKey := map[string]map[string]any{}
	for _, p := range params {
		byKey[Str(p["param_key"])] = p
	}
	if Str(byKey["net.cni"]["param_value"]) != "calico" {
		t.Fatalf("普通参数应原样入配方: %v", byKey["net.cni"])
	}
	if v := Str(byKey["boot.token"]["param_value"]); v != "" {
		t.Fatalf("凭据形态值必须剔除为空（现场填），不得抄进模板: %q", v)
	}
	if d := strings.ToLower(fmt.Sprint(byKey["boot.token"]["description"])); !strings.Contains(d, "inv-08") {
		t.Fatalf("剔除动作要留可回放注记: %v", byKey["boot.token"])
	}
	blob := fmt.Sprint(clone)
	if strings.Contains(blob, "abcdefghijkl12345678") {
		t.Fatalf("响应与落库都不得出现被克隆的凭据明文")
	}
	if n := mustOne(t, `SELECT COUNT(*) AS c FROM clc_template_param WHERE param_value LIKE ?`, "%abcdefghijkl12345678%"); paramInt(n["c"]) != 0 {
		t.Fatalf("凭据明文不得进模板参数表（落库侧复核）")
	}
	// 来源二选一：都不给 / 都给都是歧义请求
	_, err = CloneClcTemplate(map[string]any{"name": tplName("bad")}, tplUser)
	tplWantErr(t, err, "必须且只能给一个")
	_, err = CloneClcTemplate(map[string]any{"name": tplName("bad"),
		"source_template_no": "TPL-X", "source_cluster_no": cNo}, tplUser)
	tplWantErr(t, err, "必须且只能给一个")
	// 无在册节点的集群：克隆不出拓扑即拒
	emptyCluster := seedCluster(t, ClusterStatusRunning)
	_, err = CloneClcTemplate(map[string]any{"name": tplName("noNodes"), "source_cluster_no": emptyCluster}, tplUser)
	tplWantErr(t, err, "没有在册节点")
	// 从既有模板：状态不回写克隆产物；子实体整组复制、参数面引用透传随行
	src := tplSeedEnabled(t, "clone-src")
	srcNo := Str(src["template_no"])
	mustSQL(t, `UPDATE clc_cluster_template SET param_template_ref = ? WHERE template_no = ?`, "TMP-20260101-0009", srcNo)
	cp, err := CloneClcTemplate(map[string]any{
		"name": tplName("from-template"), "source_template_no": srcNo,
	}, tplUser)
	if err != nil {
		t.Fatalf("从模板克隆失败: %v", err)
	}
	if Str(cp["template_status"]) != TemplateStatusDraft {
		t.Fatalf("克隆产物不复用来源状态: %v", cp["template_status"])
	}
	if Str(cp["param_template_ref"]) != "TMP-20260101-0009" {
		t.Fatalf("参数面引用应随克隆透传: %v", cp["param_template_ref"])
	}
	gs, _ := cp["node_groups"].([]map[string]any)
	if len(gs) != 2 {
		t.Fatalf("两组子实体应整组复制: %v", gs)
	}
	if n := mustOne(t, `SELECT COUNT(*) AS c FROM clc_cluster_template WHERE template_no=? AND flag=1`, Str(cp["template_no"])); paramInt(n["c"]) != 1 {
		t.Fatalf("克隆是新登记一份，不是覆写来源")
	}
	// 来源模板软删后不可作为克隆来源（留痕语义，NotFound 提示指向已删除行）
	mustSQL(t, `UPDATE clc_cluster_template SET flag = 0 WHERE template_no = ?`, srcNo)
	_, err = CloneClcTemplate(map[string]any{"name": tplName("from-deleted"), "source_template_no": srcNo}, tplUser)
	tplWantErr(t, err, "来源模板")
}

// ---------- FUNC-36/37 列表不展开参数行 + 详情主从 + 按需反查引用 ----------

func TestClcTemplateListNoParamLeakAndDetail(t *testing.T) {
	requireDB(t)
	tpl := tplSeed(t, "c36")
	no := Str(tpl["template_no"])
	if _, err := SaveClcTemplateGroups(no, map[string]any{"reason": "加一条将被列表排除的参数",
		"node_groups": []any{map[string]any{"group_name": "主控组", "node_role": "MASTER", "expected_count": 1}},
		"params": []any{
			map[string]any{"param_key": "net.cni", "param_value": "calico"},
			map[string]any{"param_key": "sensitive.install.key", "param_value": "very-private-default"},
		}}, tplUser); err != nil {
		t.Fatalf("整组编辑失败: %v", err)
	}
	p, err := ListClcTemplates(1, 50, map[string]any{"keyword": Str(tpl["name"])})
	if err != nil || len(p.List) != 1 {
		t.Fatalf("关键字应命中本用例模板: %v / %v", err, p)
	}
	row := p.List[0]
	for _, k := range []string{"params", "param_value", "param_key", "node_groups"} {
		if _, ok := row[k]; ok {
			t.Fatalf("列表页不得展开参数行/子实体（默认值可能含敏感项，REP-14 口径注记）: %s", k)
		}
	}
	if s := Str(row["role_summary"]); !strings.Contains(s, "MASTER×1") {
		t.Fatalf("列表行应带节点组角色分布摘要，实际 %q", s)
	}
	rowBlob := fmt.Sprint(row)
	if strings.Contains(rowBlob, "calico") || strings.Contains(rowBlob, "very-private-default") || strings.Contains(rowBlob, "net.cni") {
		t.Fatalf("参数键/值不得顺着行视图漏进列表页: %s", rowBlob)
	}
	// 详情：主从全量 + 「被哪些集群用过」按需反查
	cNo := seedCluster(t, ClusterStatusRunning)
	mustSQL(t, `UPDATE clc_cluster SET template_ref = ? WHERE cluster_no = ?`, no, cNo)
	d, err := GetClcTemplate(no)
	if err != nil {
		t.Fatalf("详情查询失败: %v", err)
	}
	params, _ := d["params"].([]map[string]any)
	if len(params) != 2 {
		t.Fatalf("详情应出参数行全量: %v", params)
	}
	used, _ := d["used_by_clusters"].([]map[string]any)
	if len(used) != 1 || Str(used[0]["cluster_no"]) != cNo {
		t.Fatalf("引用反查应与 R-22 删除保护同一口径: %v", used)
	}
	_, err = GetClcTemplate("TPL-19700101-9999")
	tplWantErr(t, err, "不存在")
	// 状态/默认位/软删筛选项组合可用（已软删默认可见可筛，口径 3 家族）
	if _, err := ListClcTemplates(1, 50, map[string]any{"status": "ENABLED", "deleted": "1"}); err != nil {
		t.Fatalf("组合筛选不得报错: %v", err)
	}
}
