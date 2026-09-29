package opicmq

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSubject(t *testing.T) {
	s, err := Subject("application-center", "app.version.published")
	if err != nil || s != "opic.application-center.app.version.published" {
		t.Fatalf("Subject() = %q, err=%v", s, err)
	}
	if _, err := Subject("", "t"); err == nil {
		t.Fatal("空 source 应报错")
	}
	if _, err := Subject("APP", "t"); err == nil {
		t.Fatal("大写段应报错")
	}
}

func TestNoopPublisher_如实降级(t *testing.T) {
	p := NoopPublisher{}
	if p.Enabled() {
		t.Fatal("Noop 的 Enabled 必须为 false（如实降级判据）")
	}
	env := Envelope{ID: "e1", Type: "t", Source: "s", TraceID: "tr", OccurredAt: time.Now()}
	if err := p.Publish(context.Background(), env); err != nil {
		t.Fatalf("Noop Publish 不应报错: %v", err)
	}
}

func TestEnvelope_JSON字段名(t *testing.T) {
	b, err := json.Marshal(Envelope{ID: "e1", Type: "t", Source: "s", TraceID: "tr", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"traceId":"tr"`, `"userId":"u1"`, `"occurredAt"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("信封缺字段 %s: %s", want, b)
		}
	}
}
