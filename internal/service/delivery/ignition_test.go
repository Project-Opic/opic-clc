package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeIgnition 按点火中心交付门面真实口径替身(O-IGN FUNC-34~36):
// 路径 /api/v1/ign/delivery/*、成功终态 COMPLETED(IGN_TASK_STATUS 字典码)、
// 错误走 flat JSON {return_code, error_msg} + 非 200。
type fakeIgnition struct {
	mu       sync.Mutex
	submits  []submitReq
	receipts []string // 已请求的回执路径(含查询串,断言游标推进用)
	cancels  []string
	sequence []string // 回执状态轮询脚本
	next     int
	failRef  bool
	logs     []map[string]any // 对端回吐的增量日志行(本域不得消费)
	errBody  string           // 非空 ⇒ 受理按门面错误体回非 200
	errCode  int
}

func (f *fakeIgnition) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/submit"):
			if f.errBody != "" {
				w.WriteHeader(f.errCode)
				_, _ = w.Write([]byte(f.errBody))
				return
			}
			var req submitReq
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.submits = append(f.submits, req)
			ref := ""
			if !f.failRef {
				ref = "IGN-TSK-77"
			}
			_ = json.NewEncoder(w).Encode(submitResp{RemoteTaskRef: ref, TaskNo: ref, Status: "RUNNING"})
		case strings.Contains(r.URL.Path, "/receipt/"):
			f.receipts = append(f.receipts, r.URL.Path+"?"+r.URL.RawQuery)
			st := "RUNNING"
			if f.next < len(f.sequence) {
				st = f.sequence[f.next]
				f.next++
			}
			summary := "远端结论"
			if st == "LOST" {
				summary = "" // LOST 不带结论摘要：适配器必须自己给出可诊断口径
			}
			body := map[string]any{
				"status": st, "summary": summary, "exit_code": 0,
				"next_seq": len(f.logs), "truncated": false,
			}
			if len(f.logs) > 0 {
				body["logs"] = f.logs // 对端照实回吐日志行,本域必须只看结论、不留副本
			}
			_ = json.NewEncoder(w).Encode(body)
		case strings.Contains(r.URL.Path, "/cancel/"):
			f.cancels = append(f.cancels, r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "CANCELLING"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newTestIgnition(t *testing.T, f *fakeIgnition) *Ignition {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return NewIgnition(IgnitionConfig{BaseURL: srv.URL, PollInterval: 20 * time.Millisecond, HTTP: srv.Client()})
}

func TestIgnitionSubmitAndReceipt(t *testing.T) {
	f := &fakeIgnition{
		sequence: []string{"RUNNING", "COMPLETED"},
		logs:     []map[string]any{{"seq": 1, "direction": "TOOL", "log_line": "PLAY [etcd] ***"}},
	}
	a := newTestIgnition(t, f)
	cap := &sinkCapture{}
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-20", ClusterID: "CLU-9", StepIndex: 2, StepCode: "02"}, cap.On)
	if !out.Success || out.Verdict != VerdictRemoteReceipt || out.RemoteRef != "IGN-TSK-77" {
		t.Fatalf("远程结论不符: %+v", out)
	}
	if len(f.submits) != 1 || f.submits[0].ClusterRef != "CLU-9" || f.submits[0].StepCode != "02" {
		t.Fatalf("提交意图不符(clc 不拥有远端动作白名单语义): %+v", f.submits)
	}
	// 门面幂等键 = 来源任务标识 + 来源步骤序号(INV-34/INV-35 对端侧)
	if f.submits[0].SourceTaskNo != "TSK-20" || f.submits[0].SourceStepIndex != 2 {
		t.Fatalf("提交必须携带来源两列: %+v", f.submits[0])
	}
	if f.submits[0].ActionCode != "" {
		t.Fatal("提交不得携带本域动作码词汇(命令面与动作白名单归点火中心私有)")
	}
	if !strings.Contains(cap.Text(), "已受理") {
		t.Fatal("受理事件应进日志通道")
	}
	// REF-12 / 口径 8:远程链路的原始日志只在点火中心侧,本域不留副本(界面给跳转)。
	if strings.Contains(cap.Text(), "PLAY [etcd]") {
		t.Fatalf("远端原始日志不得回流进本域日志通道(REF-12 不留副本): %s", cap.Text())
	}
	// 游标必须推进:否则对端每轮从第 0 行重吐整页(白占带宽)。
	if len(f.receipts) < 2 || !strings.Contains(f.receipts[0], "since_seq=0") ||
		!strings.Contains(f.receipts[1], "since_seq=1") {
		t.Fatalf("回执游标未推进: %v", f.receipts)
	}
}

func TestIgnitionMissingRefFails(t *testing.T) {
	// INV-46:remote_task_ref 缺失即失败
	f := &fakeIgnition{failRef: true}
	a := newTestIgnition(t, f)
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-21", ClusterID: "CLU-9", StepIndex: 1}, nil)
	if out.Success || !strings.Contains(out.Detail, "INV-46") {
		t.Fatalf("远端引用缺失必须失败并引用 INV-46: %+v", out)
	}
}

func TestIgnitionReceiptFailureVerdict(t *testing.T) {
	f := &fakeIgnition{sequence: []string{"FAILED"}}
	a := newTestIgnition(t, f)
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-22", ClusterID: "CLU-9"}, nil)
	if out.Success || out.Verdict != VerdictRemoteReceipt || out.RemoteRef == "" {
		t.Fatalf("远端失败回执: %+v", out)
	}
}

// 门面治理拒绝(R-12/R-13)与执行位不可用(R-15)是非 200 + flat JSON:
// 错误理由必须回到本域结论里,不能吞成"提交失败"空话。
func TestIgnitionGovernanceRefuseSurfacing(t *testing.T) {
	f := &fakeIgnition{
		errBody: `{"return_code":"IGN-GOV-403","error_msg":"执行动作不在白名单内（R-12 治理拒绝）"}`,
		errCode: http.StatusForbidden,
	}
	a := newTestIgnition(t, f)
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-25", ClusterID: "CLU-9", StepIndex: 1, StepCode: "08"}, nil)
	if out.Success || !strings.Contains(out.Detail, "R-12 治理拒绝") ||
		!strings.Contains(out.Detail, "403") {
		t.Fatalf("受理侧拒绝理由须原样回传(可诊断): %+v", out)
	}
}

// 引用未命中 ⇒ 登记值与真实位置不符(INV-46),不静默重试。
func TestIgnitionReceiptUnknownRefIsLost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ign/delivery/receipt/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"return_code":"IGN-NF-404","error_msg":"任务不存在"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	a := NewIgnition(IgnitionConfig{BaseURL: srv.URL, PollInterval: 10 * time.Millisecond})
	out := a.Reconcile(context.Background(), "IGN-GONE", nil)
	if !out.Lost || !strings.Contains(out.Detail, "INV-46") {
		t.Fatalf("远端引用未命中应判失联并留 INV-46 口径: %+v", out)
	}
}

func TestIgnitionCancelRequestsRemoteRevoke(t *testing.T) {
	f := &fakeIgnition{sequence: []string{"RUNNING", "RUNNING", "RUNNING"}}
	a := newTestIgnition(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Outcome, 1)
	go func() { done <- a.Execute(ctx, StepRef{TaskNo: "TSK-23", ClusterID: "CLU-9"}, nil) }()
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case out := <-done:
		if !out.Cancelled {
			t.Fatalf("ctx 取消应给出取消结论: %+v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("取消后 Execute 未返回")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cancels) == 0 {
		t.Fatal("远程链路取消必须撤销远端任务(FUNC-33)")
	}
}

// 请求体的节点清单是结构化条目(O-IGN 侧 Q-72:清单进请求体,拓扑事实由本域给出):
// 安装族带角色与节点名,只给地址时不得出现空的角色/节点名键(对端按"只给地址 ⇒ 工作节点"处理)。
func TestIgnitionSubmitBodyShape(t *testing.T) {
	var raw string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ign/delivery/submit", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		_, _ = w.Write([]byte(`{"remote_task_ref":"IGN-27","status":"ACCEPTED"}`))
	})
	mux.HandleFunc("/api/v1/ign/delivery/receipt/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"COMPLETED","summary":"完成"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	a := NewIgnition(IgnitionConfig{BaseURL: srv.URL, PollInterval: 10 * time.Millisecond})
	out := a.Execute(context.Background(), StepRef{
		TaskNo: "TSK-27", ClusterID: "CLU-9", StepIndex: 1, StepCode: "01",
		VersionRef: "VDV-20261006-0001",
		Nodes: []NodeSpec{
			{Addr: "10.9.0.11", Role: "MASTER", Nodename: "clu-9-10-9-0-11"},
			{Addr: "10.9.0.12"},
		},
	}, nil)
	if !out.Success {
		t.Fatalf("单步应成功: %+v", out)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	text := func(v any) string { s, _ := v.(string); return s }
	if text(body["version_ref"]) != "VDV-20261006-0001" {
		t.Fatalf("交付版本引用须随请求体下发: %s", raw)
	}
	items, _ := body["nodes"].([]any)
	if len(items) != 2 {
		t.Fatalf("节点清单应逐项下发: %s", raw)
	}
	first, _ := items[0].(map[string]any)
	if text(first["addr"]) != "10.9.0.11" || text(first["role"]) != "MASTER" ||
		text(first["nodename"]) != "clu-9-10-9-0-11" {
		t.Fatalf("拓扑条目须含地址/角色/节点名(角色词汇由被调用方归一): %v", first)
	}
	second, _ := items[1].(map[string]any)
	if _, ok := second["role"]; ok {
		t.Fatalf("只给地址不得带空角色键: %s", raw)
	}
	if _, ok := second["nodename"]; ok {
		t.Fatalf("只给地址不得带空节点名键: %s", raw)
	}
	// 命令面与凭据不出域:清单之外不得混入 argv / 工作目录 / 私钥
	if _, ok := body["args"]; ok {
		t.Fatal("远程链路不得携带命令面参数(INV-47 只属本地链路)")
	}
	if _, ok := body["working_dir"]; ok {
		t.Fatal("工作目录归被调用方派生,提交不得携带")
	}
}

// Reconcile 只回读既有远端引用(B-AUTO-12 / R-16:失联恢复与对账以远端为准,不重复提交)。
func TestIgnitionReconcileReadsExistingRef(t *testing.T) {
	var submits int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ign/delivery/submit", func(w http.ResponseWriter, _ *http.Request) {
		submits++
		_, _ = w.Write([]byte(`{"remote_task_ref":"IGN-9","status":"RUNNING"}`))
	})
	mux.HandleFunc("/api/v1/ign/delivery/receipt/IGN-9", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"COMPLETED","summary":"远端已执行完毕(重启期间完成)","verdict_source":"EXIT_CODE"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	a := NewIgnition(IgnitionConfig{BaseURL: srv.URL, PollInterval: 10 * time.Millisecond})
	out := a.Reconcile(context.Background(), "IGN-9", nil)
	if !out.Success || out.Verdict != VerdictRemoteReceipt || out.RemoteRef != "IGN-9" {
		t.Fatalf("对账应以远端结论为准: %+v", out)
	}
	if submits != 0 {
		t.Fatal("对账不得重复提交远端任务")
	}
}

// LOST 是独立终态(点火中心 INV-37),既不记成功也不并入失败。
func TestIgnitionLostVerdict(t *testing.T) {
	f := &fakeIgnition{sequence: []string{"LOST"}}
	a := newTestIgnition(t, f)
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-26", ClusterID: "CLU-9", StepIndex: 1, StepCode: "03"}, nil)
	if out.Success || out.Cancelled || !out.Lost {
		t.Fatalf("LOST 应判失联: %+v", out)
	}
	if !strings.Contains(out.Detail, "痕迹不可存活") {
		t.Fatalf("失联结论须给出可诊断口径: %s", out.Detail)
	}
}

func TestIgnitionUnconfiguredIsLost(t *testing.T) {
	a := NewIgnition(IgnitionConfig{})
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-24"}, nil)
	if !out.Lost {
		t.Fatalf("未配置点火中心地址的远程链路应判不可执行: %+v", out)
	}
}

// 缺省端点路径 = 点火中心交付门面契约路径。
func TestIgnitionDefaultPaths(t *testing.T) {
	a := NewIgnition(IgnitionConfig{BaseURL: "http://ign:9709"})
	if a.cfg.SubmitPath != "/api/v1/ign/delivery/submit" ||
		a.cfg.ReceiptPath != "/api/v1/ign/delivery/receipt/{ref}" ||
		a.cfg.CancelPath != "/api/v1/ign/delivery/cancel/{ref}" {
		t.Fatalf("缺省门面路径不符: %+v", a.cfg)
	}
}
