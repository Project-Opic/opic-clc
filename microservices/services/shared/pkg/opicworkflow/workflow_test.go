package opicworkflow

import (
	"context"
	"testing"
)

func TestTaskQueue(t *testing.T) {
	q, err := TaskQueue("application-center")
	if err != nil || q != "opic.application-center" {
		t.Fatalf("TaskQueue() = %q, err=%v", q, err)
	}
	if _, err := TaskQueue(""); err == nil {
		t.Fatal("空中心名应报错")
	}
}

func TestWorkflowID(t *testing.T) {
	id, err := WorkflowID("WFL-20260927-0001", 42)
	if err != nil || id != "WFL-20260927-0001-000042" {
		t.Fatalf("WorkflowID() = %q, err=%v", id, err)
	}
	if _, err := WorkflowID("WFL-1", 1); err == nil {
		t.Fatal("非法 WFL 编号应报错（必须来自 O-APP WorkflowDefinition）")
	}
}

func TestNoopRunner_如实降级(t *testing.T) {
	var r Runner = NoopRunner{}
	if r.Enabled() {
		t.Fatal("Noop 的 Enabled 必须为 false")
	}
	if _, err := r.Start(context.Background(), "WFL-20260927-0001", nil); err != nil {
		t.Fatalf("Noop Start 不应报错: %v", err)
	}
}
