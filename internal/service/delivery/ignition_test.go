package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeIgnition struct {
	mu       sync.Mutex
	submits  []submitReq
	receipts []string
	cancels  []string
	sequence []string // 回执状态轮询脚本
	next     int
	failRef  bool
	status   int
}

func (f *fakeIgnition) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/submit"):
			var req submitReq
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.submits = append(f.submits, req)
			ref := ""
			if !f.failRef {
				ref = "IGN-TSK-77"
			}
			_ = json.NewEncoder(w).Encode(submitResp{RemoteTaskRef: ref, Status: "ACCEPTED"})
		case strings.Contains(r.URL.Path, "/receipt/"):
			f.receipts = append(f.receipts, r.URL.Path)
			st := "RUNNING"
			if f.next < len(f.sequence) {
				st = f.sequence[f.next]
				f.next++
			}
			_ = json.NewEncoder(w).Encode(receiptResp{Status: st, Summary: "远端结论"})
		case strings.Contains(r.URL.Path, "/cancel/"):
			f.cancels = append(f.cancels, r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "CANCELLED"})
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
	f := &fakeIgnition{sequence: []string{"RUNNING", "SUCCEEDED"}}
	a := newTestIgnition(t, f)
	cap := &sinkCapture{}
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-20", ClusterID: "CLU-9", StepIndex: 2, StepCode: "02"}, cap.On)
	if !out.Success || out.Verdict != VerdictRemoteReceipt || out.RemoteRef != "IGN-TSK-77" {
		t.Fatalf("远程结论不符: %+v", out)
	}
	if len(f.submits) != 1 || f.submits[0].ClusterNo != "CLU-9" || f.submits[0].StepCode != "02" {
		t.Fatalf("提交意图不符(clc 不拥有远端动作白名单语义): %+v", f.submits)
	}
	for _, p := range f.submits {
		if p.TaskNo == "" {
			t.Fatal("提交必须携带任务号")
		}
	}
	if !strings.Contains(cap.Text(), "已受理") {
		t.Fatal("受理事件应进日志通道")
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

// Reconcile 只回读既有远端引用(B-AUTO-12 / R-16:失联恢复与对账以远端为准,不重复提交)。
func TestIgnitionReconcileReadsExistingRef(t *testing.T) {
	var submits int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/delivery/submit", func(w http.ResponseWriter, _ *http.Request) {
		submits++
		_, _ = w.Write([]byte(`{"remote_task_ref":"IGN-9","status":"ACCEPTED"}`))
	})
	mux.HandleFunc("/api/delivery/receipt/IGN-9", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCEEDED","summary":"远端已执行完毕(重启期间完成)"}`))
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

func TestIgnitionUnconfiguredIsLost(t *testing.T) {
	a := NewIgnition(IgnitionConfig{})
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-24"}, nil)
	if !out.Lost {
		t.Fatalf("未配置点火中心地址的远程链路应判不可执行: %+v", out)
	}
}
