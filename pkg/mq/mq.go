// Package mq —— OPIC 事件总线的客户端封装（O-SYS 事件总线契约的 Go 实现，v3.3 附录 A.2 第 14 项）。
//
// 规范要点（基线 v3.3 /《113-变更单 V33-01》）：
//   - 主题命名：opic.<source>.<type>（source = 能力中心名，type = O-SYS 登记的事件类型 EventType）
//   - 信封遵守 O-ARC 契约：跨中心异步通信不是「免检通道」，审计头 X-Trace-Id / X-User-Id 必须透传
//   - 判据「如实降级」：mq.enabled=false 时使用 NoopPublisher，不报错、不静默丢（调用方可查 Enabled()）
//   - 判据「契约不漂移」：业务代码只依赖本包接口，不得直接 import NATS SDK（第 3 层可整体替换）
package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Envelope 事件信封（O-SYS 事件总线契约；信封字段与 O-ARC 错误码体系并行，不混用 HTTP 信封）。
type Envelope struct {
	ID         string          `json:"id"`         // 事件唯一标识（生产方生成，JetStream 幂等发布键）
	Type       string          `json:"type"`       // 事件类型（O-SYS EventType，如 app.version.published）
	Source     string          `json:"source"`     // 发布方能力中心名（如 application-center）
	TraceID    string          `json:"traceId"`    // 审计头透传：X-Trace-Id
	UserID     string          `json:"userId"`     // 审计头透传：X-User-Id（系统事件为空）
	OccurredAt time.Time       `json:"occurredAt"` // 事件发生时点（生产方时钟）
	Data       json.RawMessage `json:"data"`       // 载荷（发布方 schema 自治；消费方按名承载）
}

// Publisher 事件发布接口（业务代码只依赖此接口，不依赖 NATS SDK）。
type Publisher interface {
	Publish(ctx context.Context, env Envelope) error
	// Enabled 报告当前是否真实连接总线（如实降级判据：noop 时业务可感知）。
	Enabled() bool
	Close() error
}

// Subject 生成主题名：opic.<source>.<type>。段内仅小写字母数字与 - _ .。
func Subject(source, eventType string) (string, error) {
	if err := validSeg(source); err != nil {
		return "", fmt.Errorf("mq: source 段非法: %w", err)
	}
	if err := validSeg(eventType); err != nil {
		return "", fmt.Errorf("mq: type 段非法: %w", err)
	}
	return strings.ToLower("opic." + source + "." + eventType), nil
}

var segRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-_]*$`)

func validSeg(s string) error {
	if s == "" {
		return errors.New("空段")
	}
	for _, part := range strings.Split(s, ".") {
		if !segRe.MatchString(part) {
			return fmt.Errorf("段 %q 含非法字符", part)
		}
	}
	return nil
}

// NoopPublisher 降级实现（mq.enabled=false）。Publish 不发送、不报错——如实降级。
type NoopPublisher struct{}

func (NoopPublisher) Publish(_ context.Context, _ Envelope) error { return nil }
func (NoopPublisher) Enabled() bool                               { return false }
func (NoopPublisher) Close() error                                { return nil }

// InitDefault 按配置装配发布器（main.go 装配点；enabled=false 返回 Noop）。
func InitDefault(cfg struct {
	Enabled       bool
	URL           string
	SubjectPrefix string
	JetStream     bool
	TimeoutSec    int
}) Publisher {
	if !cfg.Enabled {
		return NoopPublisher{}
	}
	p, err := NewNATSPublisher(cfg.URL, cfg.JetStream, time.Duration(cfg.TimeoutSec)*time.Second)
	if err != nil {
		// 如实降级：连接失败不致命，返回 Noop 并由调用方日志呈现 Enabled()=false
		return NoopPublisher{}
	}
	return p
}
