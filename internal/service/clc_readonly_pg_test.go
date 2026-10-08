// clc_readonly_pg_test.go —— v1.2 只读代理与交付前置预检（FUNC-47 / 48 / 49 / 50）语义用例。
//
// 覆盖口径（m2 :1027-1101 / m3 / m7 REP-16~18）：
//
//	FUNC-47 REF-18 拼接串口径与平台全局变量取源、REMOVED 节点不入列、R-03 接管注记、
//	        零凭据出口（INV-08 输出面延伸，对返回的 items 数据面做反向探针）、复制不留痕；
//	FUNC-48 对端 flat JSON 原样透传、参数上行口径、本域不写业务表也不留痕（R-25）、
//	        不可达三档（未配置 / 传输层 / 对端已答拒绝）一律 200 + 显式结论 + 跳转入口，不缓存冒充（REF-12）；
//	FUNC-49 file_face 必带且一次一面、逐次留痕（含失败尝试）、审计明细不回写原文、
//	        PERM-FUNC-49 是独立权限码（不随 48 附带）；
//	FUNC-50 五档判定随本域事实翻转、不拦截（返回成功）、不写本域对象不发事件，
//	        且模型侧 FUNC-50 无报表（M7 唯一显式例外），FUNC-47/48/49 与 REP-16/17/18 双向一对一。
package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gitcode.com/opic-ontology/opic-techbase/internal/config"
	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/ontology"
	"gitcode.com/opic-ontology/opic-techbase/internal/service/delivery"
)

// ---------- 点火中心只读通道替身（FUNC-48/49 的对端） ----------

type roCall struct {
	path  string
	query map[string]string
	auth  string
}

// roPeer 只服务 defaultConfigPath 与其 /raw 后缀两条路径，按 mode 决定应答形态。
type roPeer struct {
	mu    sync.Mutex
	calls []roCall
	srv   *httptest.Server
	mode  string // "ok" | "reject"
}

func newROPeer() *roPeer {
	p := &roPeer{mode: "ok"}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ign/delivery/config/", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		q := map[string]string{}
		for k, vs := range r.URL.Query() {
			if len(vs) > 0 {
				q[k] = vs[0]
			}
		}
		p.calls = append(p.calls, roCall{path: r.URL.Path, query: q, auth: r.Header.Get("Authorization")})
		if p.mode == "reject" {
			// 对端门面组的失败形态是 flat {return_code,error_msg}（与登录组信封不同轴）。
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"return_code": "IGN_STATE_0002", "error_msg": "集群在对端尚未受理交付"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/raw") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_face":        q["file_face"],
				"cluster_ref":      strings.TrimPrefix(r.URL.Path, "/api/v1/ign/delivery/config/"),
				"lines":            []map[string]any{{"no": 1, "text": "[all:vars]", "masked": false}},
				"content_sha256":   "aabbccddeeff0011",
				"credentials_note": "凭据形态的行已在流出前整行掩码（INV-08）",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"cluster_ref": strings.TrimPrefix(r.URL.Path, "/api/v1/ign/delivery/config/"),
			"file_face":   q["file_face"],
			"fields": []map[string]any{
				{"field_key": "net.service_cidr", "effective_value": "10.64.0.0/16", "source_layer": "集群覆写"},
				{"field_key": "k8s.version", "effective_value": "v1.31.6", "source_layer": "交付版本派生"},
			},
			"face_caliber": "四层取值结论由对端 R-20 唯一实现给出",
		})
	})
	p.srv = httptest.NewServer(mux)
	return p
}

func (p *roPeer) setMode(m string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mode = m
}

func (p *roPeer) received() []roCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]roCall, len(p.calls))
	copy(out, p.calls)
	return out
}

// attachROPeer 把替身接到运行期单例上：同一令牌口径（R-25 复用部署期注入的令牌，不新建链路）。
func attachROPeer(t *testing.T, rt *deliveryRuntime, p *roPeer) {
	t.Helper()
	prevBase, prevToken := config.Config.Delivery.IgnitionBaseURL, config.Config.Delivery.IgnitionToken
	config.Config.Delivery.IgnitionBaseURL = p.srv.URL
	config.Config.Delivery.IgnitionToken = "peer-token-42"
	rt.remote = delivery.NewIgnition(delivery.IgnitionConfig{
		BaseURL: p.srv.URL, Token: "peer-token-42", PollInterval: 10 * time.Millisecond})
	t.Cleanup(func() {
		config.Config.Delivery.IgnitionBaseURL, config.Config.Delivery.IgnitionToken = prevBase, prevToken
		p.srv.Close()
	})
}

// ---------- 用例助手 ----------

func roClusterID(t *testing.T, clusterNo string) int64 {
	t.Helper()
	return Int(mustOne(t, `SELECT id FROM clc_cluster WHERE cluster_no=? AND flag=1`, clusterNo)["id"])
}

func auditCount(t *testing.T, action string) int64 {
	t.Helper()
	if action == "" {
		return countRows(t, `SELECT COUNT(*) FROM audit_logs`)
	}
	return countRows(t, `SELECT COUNT(*) FROM audit_logs WHERE action=?`, action)
}

// auditCountFor 按集群限定计数（留痕断言只关心本用例播下的夹具，不受同库其他用例次序影响）。
func auditCountFor(t *testing.T, action, clusterNo string) int64 {
	t.Helper()
	return countRows(t, `SELECT COUNT(*) FROM audit_logs WHERE action=? AND detail LIKE ?`,
		action, "%"+clusterNo+"%")
}

// setPlatformParam 幂等置平台参数（夹具直写，不经 FUNC-30 白名单，避免把用例耦合到保存口校验）。
func setPlatformParam(t *testing.T, key, value string) {
	t.Helper()
	mustSQL(t, `INSERT INTO clc_platform_param (param_key, param_value, param_group, description)
		VALUES (?, ?, 'ssh', '测试夹具') ON CONFLICT (param_key) DO UPDATE SET param_value = EXCLUDED.param_value`,
		key, value)
}

func payloadJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	return string(raw)
}

// rowsOf 把「Go 侧构造的切片」与「JSON 解码出的 []any」两形态统一成行视图。
func rowsOf(v any) []map[string]any {
	out := []map[string]any{}
	switch rows := v.(type) {
	case []map[string]any:
		out = append(out, rows...)
	case []any:
		for _, item := range rows {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func checkItemOf(t *testing.T, out map[string]any, id string) map[string]any {
	t.Helper()
	for _, it := range rowsOf(out["checks"]) {
		if Str(it["id"]) == id {
			return it
		}
	}
	t.Fatalf("判定项 %s 缺失（五档须齐全）: %+v", id, out["checks"])
	return nil
}

// ---------- FUNC-47 节点 SSH 登录标识（REP-16 / REF-18 / INV-08 / R-03） ----------

func TestFunc47LoginIdentityCaliberAndZeroCredential(t *testing.T) {
	requireDB(t)
	clusterNo := seedCluster(t, ClusterStatusRunning)
	mustSQL(t, `INSERT INTO clc_cluster_node (node_no, cluster_ref, node_role, ip_address, asset_ref, node_status)
		VALUES ('NOD-RO-M1', ?, 'MASTER', '10.77.7.11', 'AST-RO-01', 'ADDED'),
		       ('NOD-RO-W1', ?, 'WORKER', '10.77.7.12', '', 'PENDING'),
		       ('NOD-RO-W9', ?, 'WORKER', '10.77.7.13', 'AST-RO-09', 'REMOVED')`,
		clusterNo, clusterNo, clusterNo)
	// 登录用户取平台全局变量口径（与 FUNC-01 第二步裸机模式同源），不是节点属性。
	setPlatformParam(t, "clc.ssh_login_user", "opicops")
	defer setPlatformParam(t, "clc.ssh_login_user", "root")

	before := auditCount(t, "")
	out, err := ClusterNodeLoginIdentities(roClusterID(t, clusterNo))
	if err != nil {
		t.Fatalf("FUNC-47 读取失败: %v", err)
	}
	if got := Str(out["ssh_login_user"]); got != "opicops" {
		t.Fatalf("登录用户应取平台全局变量口径,实际 %q", got)
	}
	items, _ := out["items"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("REMOVED 节点不得入列（在册非 REMOVED 才有登录标识）: %+v", items)
	}
	if got := Str(items[0]["login_identity"]); got != "opicops@10.77.7.11" {
		t.Fatalf("REF-18 拼接串口径应为「登录用户@节点地址」,实际 %q", got)
	}
	if truthy(items[0]["takeover"]) {
		t.Fatalf("资产在册节点不该带接管注记: %+v", items[0])
	}
	if !truthy(items[1]["takeover"]) || !strings.Contains(Str(items[1]["takeover_note"]), "R-03") {
		t.Fatalf("资产引用已置空的节点须显式标 R-03 人工接管: %+v", items[1])
	}

	// 零凭据出口反向探针：只扫数据面（items + 全局登录用户），不扫口径说明文本
	// —— caliber_note 里的规则名是给人读的中文,把判据字面量混进探针会自撞。
	probe := payloadJSON(t, items) + Str(out["ssh_login_user"])
	for _, marker := range []string{"sshpass", "passwd", "password", "passphrase", "PRIVATE KEY",
		"id_rsa", "id_ed25519", "-i ", "strict_host", "authorized_keys", "BEGIN OPENSSH"} {
		if strings.Contains(strings.ToLower(probe), strings.ToLower(marker)) {
			t.Fatalf("INV-08 输出面延伸:凭据类标记 %q 出现在响应数据面: %s", marker, probe)
		}
	}
	// 复制动作 = 纯前端剪贴板写入:后端不产生业务写入、不发事件、不留痕（与 FUNC-49 刻意分档）。
	if n := auditCount(t, ""); n != before {
		t.Fatalf("FUNC-47 不得留痕:审计行数 %d → %d", before, n)
	}
	if n := auditCount(t, "FUNC-47"); n != 0 {
		t.Fatalf("FUNC-47 复制行为不留痕,却有 %d 行审计", n)
	}
}

// ---------- FUNC-48 字段级代理查看（REP-17 / R-25 / REF-12） ----------

func TestFunc48FieldViewPassthroughAndNoLocalWrite(t *testing.T) {
	requireDB(t)
	rt := freshRT(t, time.Minute)
	peer := newROPeer()
	attachROPeer(t, rt, peer)
	clusterNo := seedCluster(t, ClusterStatusRunning)

	params := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE cluster_ref=?`, clusterNo)
	auditBefore := auditCount(t, "")
	out, err := IgnDeliveryConfigView(roClusterID(t, clusterNo), "hosts", "IPT-0007", "K8S-v1.31.6")
	if err != nil {
		t.Fatalf("FUNC-48 代理读失败: %v", err)
	}
	calls := peer.received()
	if len(calls) != 1 {
		t.Fatalf("对端只应收到一次读取,实际 %d 次", len(calls))
	}
	if want := "/api/v1/ign/delivery/config/" + clusterNo; calls[0].path != want {
		t.Fatalf("对端通道路径错配: want %s got %s", want, calls[0].path)
	}
	// 同一令牌（不前端直连）；面标识上行统一大写口径,引用原样透传由对端回落。
	if calls[0].auth != "Bearer peer-token-42" {
		t.Fatalf("代理读须复用部署期注入的令牌,实际请求头 %q", calls[0].auth)
	}
	if calls[0].query["file_face"] != "HOSTS" || calls[0].query["param_template_ref"] != "IPT-0007" ||
		calls[0].query["version_ref"] != "K8S-v1.31.6" {
		t.Fatalf("三个查询参数上行口径不符: %+v", calls[0].query)
	}
	// 展示对端结论:字段键 + 生效值 + 来源档原样透传,本域不重算、不合并四层。
	// 对端 flat JSON 解码后是 []any,故按 any 取列再逐个转 map(不假设对端 Go 类型)。
	fields := rowsOf(out["fields"])
	if len(fields) != 2 || Str(fields[0]["field_key"]) != "net.service_cidr" ||
		Str(fields[0]["source_layer"]) != "集群覆写" || Str(fields[1]["source_layer"]) != "交付版本派生" {
		t.Fatalf("对端字段级视图必须原样透传: %+v", out["fields"])
	}
	if Str(out["face_caliber"]) == "" || Str(out["cluster_no"]) != clusterNo {
		t.Fatalf("本域只补集群标识,不得吞掉对端字段: %+v", out)
	}
	if truthy(out["peer_unreachable"]) {
		t.Fatalf("对端已答且成功,不得标不可达: %+v", out)
	}
	// R-25 只读代理:不留副本、不落对象、不产生事件；FUNC-48 也无逐次留痕要求（那是 FUNC-49）。
	if n := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE cluster_ref=?`, clusterNo); n != params {
		t.Fatalf("代理读不得写本域业务表: clc_param %d → %d", params, n)
	}
	if n := auditCount(t, ""); n != auditBefore {
		t.Fatalf("FUNC-48 不设逐次留痕（模型只给 FUNC-49 提这条）: 审计 %d → %d", auditBefore, n)
	}
}

func TestFunc48PeerUnreachableThreeKinds(t *testing.T) {
	requireDB(t)
	clusterNo := seedCluster(t, ClusterStatusRunning)
	id := roClusterID(t, clusterNo)

	// 档一：本部署未接线（IgnitionBaseURL 空 ⇒ ErrPeerNotConfigured）。
	// 每档都新取一次运行期单例并立刻改其 remote：dl 是全局的，先后 freshRT 会互相顶掉。
	blank := freshRT(t, time.Minute)
	prevBase := config.Config.Delivery.IgnitionBaseURL
	config.Config.Delivery.IgnitionBaseURL = ""
	blank.remote = delivery.NewIgnition(delivery.IgnitionConfig{})
	out, err := IgnDeliveryConfigView(id, "", "", "")
	if err != nil {
		t.Fatalf("未配置对端也须回 200 级显式结论,不能升级为业务失败: %v", err)
	}
	if !truthy(out["peer_unreachable"]) || Str(out["unreachable_kind"]) != "unreachable" {
		t.Fatalf("未接线档口径不符: %+v", out)
	}
	if s := Str(out["jump_entry"]); s != "" {
		t.Fatalf("未配置对端地址时不得凭空造跳转入口: %q", s)
	}
	config.Config.Delivery.IgnitionBaseURL = prevBase

	// 档二：对端已答但拒绝（HTTP 409 + 门面 flat 错误体）。
	peer := newROPeer()
	attachROPeer(t, freshRT(t, time.Minute), peer)
	peer.setMode("reject")
	out, err = IgnDeliveryConfigView(id, "config", "", "")
	if err != nil {
		t.Fatalf("对端拒绝同样不得升级为业务失败: %v", err)
	}
	if !truthy(out["peer_unreachable"]) || Str(out["unreachable_kind"]) != "peer_answered" {
		t.Fatalf("对端已答档口径不符: %+v", out)
	}
	if Int(out["peer_error_code"]) != 409 || !strings.Contains(Str(out["peer_error_msg"]), "IGN_STATE_0002") {
		t.Fatalf("须如实带出对端状态码与中文口径: %+v", out)
	}
	if !strings.Contains(Str(out["jump_entry"]), clusterNo) {
		t.Fatalf("不可达必须保留跳转入口(REF-12): %+v", out)
	}
	// 不缓存冒充现状：不可达体里不得有任何字段级视图内容。
	if _, ok := out["fields"]; ok {
		t.Fatalf("不可达时不得回吐字段内容冒充现状: %+v", out)
	}

	// 档三：传输层不可达（对端地址指向已关闭的服务）。
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	closedURL := closed.URL
	closed.Close()
	rt2 := freshRT(t, time.Minute)
	prevBase2 := config.Config.Delivery.IgnitionBaseURL
	config.Config.Delivery.IgnitionBaseURL = closedURL
	rt2.remote = delivery.NewIgnition(delivery.IgnitionConfig{BaseURL: closedURL})
	out, err = IgnDeliveryConfigView(id, "", "", "")
	config.Config.Delivery.IgnitionBaseURL = prevBase2
	if err != nil {
		t.Fatalf("传输层不可达也走显式结论: %v", err)
	}
	if !truthy(out["peer_unreachable"]) || Str(out["unreachable_kind"]) != "unreachable" {
		t.Fatalf("传输层不可达档口径不符: %+v", out)
	}
}

// ---------- FUNC-49 原文代理只读（REP-18 / 逐次留痕 / INV-08） ----------

func TestFunc49RawFaceGuard(t *testing.T) {
	requireDB(t)
	rt := freshRT(t, time.Minute)
	peer := newROPeer()
	attachROPeer(t, rt, peer)
	clusterNo := seedCluster(t, ClusterStatusRunning)
	id := roClusterID(t, clusterNo)

	for _, bad := range []string{"", "  ", "bogus", "BOTH"} {
		_, err := IgnDeliveryConfigRawView(id, bad, testUser)
		if err == nil {
			t.Fatalf("file_face=%q 须被拒(一次一面、缺参不默认)", bad)
		}
		if !strings.Contains(AsBizErr(err).Msg, "HOSTS 或 CONFIG") {
			t.Fatalf("拒绝文案须点明合法域并给出下一步: %v", AsBizErr(err).Msg)
		}
	}
	// 缺参/越界一律拒绝 ⇒ 一次都不该打到对端,也不该留痕。
	if n := len(peer.received()); n != 0 {
		t.Fatalf("守卫失败却已打对端 %d 次", n)
	}
	if n := auditCountFor(t, "FUNC-49", clusterNo); n != 0 {
		t.Fatalf("参数守卫拒绝不应留痕(连读都没发生): %d 行", n)
	}
	// 集群不存在时同样是业务失败,而不是静默空视图。
	_, err := IgnDeliveryConfigRawView(9999999, "HOSTS", testUser)
	if err == nil {
		t.Fatal("集群不存在须报错")
	}
}

func TestFunc49RawPassthroughAndAuditEveryCall(t *testing.T) {
	requireDB(t)
	rt := freshRT(t, time.Minute)
	peer := newROPeer()
	attachROPeer(t, rt, peer)
	clusterNo := seedCluster(t, ClusterStatusRunning)
	id := roClusterID(t, clusterNo)
	user := UserInfo{ID: 7, Username: "auditor"}

	before := auditCountFor(t, "FUNC-49", clusterNo)
	out, err := IgnDeliveryConfigRawView(id, "hosts", user)
	if err != nil {
		t.Fatalf("FUNC-49 HOSTS 原文读取失败: %v", err)
	}
	if Str(out["file_face"]) != "HOSTS" || truthy(out["peer_unreachable"]) {
		t.Fatalf("面标识须按大写口径回显且不得标不可达: %+v", out)
	}
	if len(rowsOf(out["lines"])) != 1 {
		t.Fatalf("原文行必须由对端整行掩码后原样透传: %+v", out["lines"])
	}
	// 掩码前摘要本域不解析、不删除、不重算，原样透传给界面呈现（INV-08：再掩码一次即等于承认本域持有原文）。
	if Str(out["content_sha256"]) != "aabbccddeeff0011" {
		t.Fatalf("对端摘要字段须原样透传: %+v", out)
	}
	if c := peer.received(); len(c) != 1 || !strings.HasSuffix(c[0].path, "/raw") ||
		c[0].query["file_face"] != "HOSTS" {
		t.Fatalf("原文通道须走 /raw 且一次一面: %+v", c)
	}
	// 另一面独立调用（CONFIG），不得与 HOSTS 合并出面。
	if _, err := IgnDeliveryConfigRawView(id, "CONFIG", user); err != nil {
		t.Fatalf("CONFIG 面读取失败: %v", err)
	}
	if c := peer.received(); len(c) != 2 || c[1].query["file_face"] != "CONFIG" {
		t.Fatalf("两面须各自上行: %+v", c)
	}

	// 逐次留痕：两次成功 ⇒ 两行，且每行都齐「谁 / 哪个集群 / 哪一面」。
	if n := auditCountFor(t, "FUNC-49", clusterNo); n != before+2 {
		t.Fatalf("原文视图必须逐次留痕: 期望 +%d 行,实际 +%d 行", 2, n-before)
	}
	rows := mustList(t, `SELECT username, detail FROM audit_logs WHERE action='FUNC-49'
		AND detail LIKE ? ORDER BY id DESC LIMIT 2`, "%"+clusterNo+"%")
	for _, r := range rows {
		d := Str(r["detail"])
		if Str(r["username"]) != user.Username {
			t.Fatalf("留痕须记查看人: %+v", r)
		}
		if !strings.Contains(d, clusterNo) {
			t.Fatalf("留痕须记集群: %s", d)
		}
		// INV-08：只记「看了哪一面」,绝不把原文内容或摘要回写进审计（那等于本域持有原文）。
		if strings.Contains(d, "[all:vars]") || strings.Contains(d, "aabbccddeeff0011") {
			t.Fatalf("审计明细不得含原文内容或掩码前摘要: %s", d)
		}
	}

	// 失败尝试同样留痕（读尝试本身就是审计对象）；信封与 FUNC-48 同档。
	peer.setMode("reject")
	out, err = IgnDeliveryConfigRawView(id, "HOSTS", user)
	if err != nil {
		t.Fatalf("对端拒绝应转显式结论: %v", err)
	}
	if !truthy(out["peer_unreachable"]) || Str(out["file_face"]) != "HOSTS" {
		t.Fatalf("不可达体须保留面标识与显式结论: %+v", out)
	}
	if n := auditCountFor(t, "FUNC-49", clusterNo); n != before+3 {
		t.Fatalf("失败读取也要留痕(共 3 次尝试): 期望 %d 实际 %d", before+3, n)
	}
	last := mustOne(t, `SELECT detail FROM audit_logs WHERE action='FUNC-49' AND detail LIKE ?
		ORDER BY id DESC LIMIT 1`, "%"+clusterNo+"%")
	if !strings.Contains(Str(last["detail"]), "被对端拒绝") {
		t.Fatalf("失败留痕须点明拒绝方与原因: %+v", last)
	}
	// 不留副本、不写本域对象：不可达体里不得有原文行或摘要冒充现状（REF-12）。
	if _, ok := out["lines"]; ok {
		t.Fatalf("不可达时不得回吐原文内容: %+v", out)
	}
	if _, ok := out["content_sha256"]; ok {
		t.Fatalf("不可达时不得带出摘要: %+v", out)
	}
}

func TestFunc49PermissionCodeIsIndependentFromFieldView(t *testing.T) {
	requireDB(t)
	reg := ontology.Load()
	for _, code := range []string{"PERM-FUNC-48", "PERM-FUNC-49"} {
		p := reg.Permissions[code]
		if p == nil {
			t.Fatalf("m5 缺权限码 %s(原文视图独立发牌的前提是它是独立一张牌)", code)
		}
	}
	if Str(reg.Permissions["PERM-FUNC-49"]["targetRef"]) != "FUNC-49" ||
		Str(reg.Permissions["PERM-FUNC-48"]["targetRef"]) != "FUNC-48" {
		t.Fatal("两张牌的挂口必须各自对应自己的 m2 行为,不得共用")
	}
	// 库侧必须是两行独立权限,且鉴权按码精确匹配（持 48 不天然持 49）。
	one := mustOne(t, `SELECT COUNT(*) AS n FROM sys_permission
		WHERE code IN ('PERM-FUNC-48','PERM-FUNC-49')`)
	if Int(one["n"]) != 2 {
		t.Fatalf("PERM-FUNC-48/49 须各成一行,实际 %d 行", Int(one["n"]))
	}
	// m5 事实核验：只读角色 CLC-ROLE-04 持 47 而不持 48/49 —— 原文视图对只读档刻意收紧。
	role := mustOne(t, `SELECT COUNT(*) AS n FROM sys_role_permission rp
		JOIN sys_role ro ON ro.id=rp.role_id JOIN sys_permission p ON p.id=rp.permission_id
		WHERE ro.code='CLC-ROLE-04' AND p.code IN ('PERM-FUNC-47','PERM-FUNC-48','PERM-FUNC-49')`)
	if Int(role["n"]) != 1 {
		t.Fatalf("CLC-ROLE-04 应只持 PERM-FUNC-47(登录标识可读、原文不可读): 实际 %d 条", Int(role["n"]))
	}
	if n := countRows(t, `SELECT COUNT(*) FROM sys_role_permission rp
		JOIN sys_role ro ON ro.id=rp.role_id JOIN sys_permission p ON p.id=rp.permission_id
		WHERE ro.code='CLC-ROLE-04' AND p.code='PERM-FUNC-47'`); n != 1 {
		t.Fatal("CLC-ROLE-04 缺失 PERM-FUNC-47(登录标识对全部只读角色开放)")
	}
}

// ---------- FUNC-50 交付前置完备性预检（不拦截 / 不写对象 / 无报表） ----------

func TestFunc50ReadinessItemsFlipWithLocalFacts(t *testing.T) {
	requireDB(t)
	clusterNo := seedCluster(t, ClusterStatusDraft) // MCC 模式、无引用、无节点
	id := roClusterID(t, clusterNo)

	tasks := countRows(t, `SELECT COUNT(*) FROM clc_task WHERE cluster_ref=?`, clusterNo)
	auditBefore := auditCount(t, "")

	// 空档：既缺节点又缺引用 ⇒ 判定不通过,但调用本身成功（不拦截提交）。
	out, err := DeliveryReadinessCheck(id)
	if err != nil {
		t.Fatalf("预检是即时提示,不得因不完备而报错: %v", err)
	}
	if truthy(out["all_pass"]) {
		t.Fatalf("空集群不得报完备: %+v", out)
	}
	nodes := checkItemOf(t, out, "nodes_nonempty_with_master")
	if truthy(nodes["pass"]) || !strings.Contains(Str(nodes["note"]), "节点清单为空") {
		t.Fatalf("判定① 应指向 FUNC-06 登记节点: %+v", nodes)
	}
	refs := checkItemOf(t, out, "refs_ready_or_explicit_legacy")
	if truthy(refs["pass"]) || !strings.Contains(Str(refs["note"]), "INV-04") {
		t.Fatalf("判定② 应复用 checkRefConsistency 同一配方(INV-04): %+v", refs)
	}
	step := checkItemOf(t, out, "step_mode_determined")
	if !truthy(step["pass"]) {
		t.Fatalf("无在途任务时步进模式按入口缺省落定,不存在「未定」态: %+v", step)
	}
	// 首检即零写入：不建任务、不留痕（判定结论只活在响应体里）。
	if n := countRows(t, `SELECT COUNT(*) FROM clc_task WHERE cluster_ref=?`, clusterNo); n != tasks {
		t.Fatalf("预检不得建任务: clc_task %d → %d", tasks, n)
	}
	if n := auditCount(t, ""); n != auditBefore {
		t.Fatalf("预检不留痕也不发事件(即时提示): 审计 %d → %d", auditBefore, n)
	}

	// 补齐本域事实：MASTER 节点 + 一项中心引用 + 交付工具路径 ⇒ ①②③ 翻转。
	mustSQL(t, `INSERT INTO clc_cluster_node (node_no, cluster_ref, node_role, ip_address, node_status)
		VALUES ('NOD-RO-50M', ?, 'MASTER', '10.77.7.51', 'ADDED'),
		       ('NOD-RO-50W', ?, 'WORKER', '10.77.7.52', 'PENDING')`, clusterNo, clusterNo)
	mustSQL(t, `UPDATE clc_cluster SET version_ref='VER-K8S-1316' WHERE cluster_no=?`, clusterNo)
	prevTool := config.Config.Delivery.ToolBin
	config.Config.Delivery.ToolBin = "delivery-tool"
	defer func() { config.Config.Delivery.ToolBin = prevTool }()

	out, err = DeliveryReadinessCheck(id)
	if err != nil {
		t.Fatalf("补齐后再检失败: %v", err)
	}
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"nodes_nonempty_with_master", true},
		{"refs_ready_or_explicit_legacy", true},
		{"execution_path_registered_available", true},
		{"step_mode_determined", true},
		{"no_override_conflicts_derived", true},
	} {
		it := checkItemOf(t, out, tc.id)
		if truthy(it["pass"]) != tc.want {
			t.Fatalf("判定 %s 期望 pass=%v,实际 %+v", tc.id, tc.want, it)
		}
	}
	if !truthy(out["all_pass"]) {
		t.Fatalf("五档齐备应报完备: %+v", out)
	}

	// 判定⑤：覆写行与派生冲突（sync_pending=1 且来源为人工覆写）即不过。
	mustSQL(t, `INSERT INTO clc_param (cluster_ref, param_key, param_value, param_source, sync_pending, derive_note)
		VALUES (?, 'net.cni', 'calico', 'MANUAL_OVERRIDE', 1, '上游派生改为 flannel,与人工覆写冲突')`, clusterNo)
	out, _ = DeliveryReadinessCheck(id)
	ov := checkItemOf(t, out, "no_override_conflicts_derived")
	if truthy(ov["pass"]) || !strings.Contains(Str(ov["note"]), "R-04") {
		t.Fatalf("判定⑤ 须点名 R-04 冲突并给出 FUNC-15 处置路径: %+v", ov)
	}
	if truthy(out["all_pass"]) {
		t.Fatal("存在待同步冲突时不得报完备")
	}
	// 人工确认后（FUNC-15 清标记）回到完备。
	mustSQL(t, `UPDATE clc_param SET sync_pending=0 WHERE cluster_ref=? AND param_key='net.cni'`, clusterNo)
	out, _ = DeliveryReadinessCheck(id)
	if !truthy(out["all_pass"]) {
		t.Fatalf("冲突清除后应回到完备: %+v", out["checks"])
	}

	// 判定④ 的在途任务分支：STEP 任务存在时须点出任务号与模式。
	taskNo := seedTask(t, clusterNo, TaskKindInstall, TaskStatusQueued, ExecPathLocal)
	mustSQL(t, `UPDATE clc_task SET step_mode=? WHERE task_no=?`, StepModeStep, taskNo)
	out, _ = DeliveryReadinessCheck(id)
	step = checkItemOf(t, out, "step_mode_determined")
	if !truthy(step["pass"]) || !strings.Contains(Str(step["note"]), taskNo) ||
		!strings.Contains(Str(step["note"]), StepModeStep) {
		t.Fatalf("判定④ 须引用在途任务的已定模式(INV-50/REF-17): %+v", step)
	}

	// 只读：预检本身不写本域对象、不产生事件。基线在夹具齐备后重取，
	// 首检时的「零写入」已由上面的 tasks/auditBefore 断过，这里断终态一次调用的零增量。
	tasksNow := countRows(t, `SELECT COUNT(*) FROM clc_task WHERE cluster_ref=?`, clusterNo)
	paramsNow := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE cluster_ref=?`, clusterNo)
	auditNow := auditCount(t, "")
	if _, err := DeliveryReadinessCheck(id); err != nil {
		t.Fatalf("终态预检失败: %v", err)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_task WHERE cluster_ref=?`, clusterNo); n != tasksNow {
		t.Fatalf("预检不得建任务: clc_task %d → %d", tasksNow, n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM clc_param WHERE cluster_ref=?`, clusterNo); n != paramsNow {
		t.Fatalf("预检不得写参数行: clc_param %d → %d", paramsNow, n)
	}
	if n := auditCount(t, ""); n != auditNow {
		t.Fatalf("预检不留痕也不发事件(即时提示): 审计 %d → %d", auditNow, n)
	}
	mustSQL(t, `DELETE FROM clc_task WHERE task_no=?`, taskNo)
	mustSQL(t, `DELETE FROM clc_param WHERE cluster_ref=?`, clusterNo)
	mustSQL(t, `DELETE FROM clc_cluster_node WHERE cluster_ref=?`, clusterNo)
}

func TestFunc50PreflightHasNoReportButThreeProxiesDo(t *testing.T) {
	requireDB(t)
	reg := ontology.Load()
	// FUNC-50 是 M7 第五部分 5.16 登记的唯一显式例外：一次判定 + 一份即时提示，无留存价值。
	for id, q := range reg.QueryReports {
		if Str(q["behaviorRef"]) == "FUNC-50" {
			t.Fatalf("交付前置预检不得建报表(无列表形态),却有报表 %s %q", id, Str(q["name"]))
		}
	}
	want := map[string]string{"FUNC-47": "REP-16", "FUNC-48": "REP-17", "FUNC-49": "REP-18"}
	seen := map[string]string{}
	for id, q := range reg.QueryReports {
		if ref := Str(q["behaviorRef"]); want[ref] != "" {
			seen[ref] = id
		}
	}
	for ref, rep := range want {
		if seen[ref] != rep {
			t.Fatalf("%s 的报表须恰为 %s(双向一对一),实际 %+v", ref, rep, seen)
		}
	}
}
