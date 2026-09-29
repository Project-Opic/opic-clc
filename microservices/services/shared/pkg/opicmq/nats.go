// nats.go —— 事件总线的 NATS/JetStream 实现（第 3 层可替换；v3.3 附录 A.2 第 14 项）。
// 业务代码不得直接 import 本包之外的 nats SDK（契约不漂移判据）。
package opicmq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSPublisher NATS 实现。JetStream 开启时使用持久化发布（等待服务端确认）。
type NATSPublisher struct {
	conn *nats.Conn
	js   jetstream.JetStream
	jsOn bool
}

// NewNATSPublisher 连接 NATS。jetstreamOn=true 时启用 JetStream（事件回放 / 持久订阅）。
func NewNATSPublisher(url string, jetstreamOn bool, timeout time.Duration) (*NATSPublisher, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	conn, err := nats.Connect(url,
		nats.Name("opic-mq"),
		nats.Timeout(timeout),
		nats.ReconnectWait(2*time.Second),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, fmt.Errorf("mq: NATS 连接失败 %s: %w", url, err)
	}
	p := &NATSPublisher{conn: conn}
	if jetstreamOn {
		js, err := jetstream.New(conn)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("mq: JetStream 初始化失败: %w", err)
		}
		p.js, p.jsOn = js, true
	}
	return p, nil
}

// Publish 发布事件：信封序列化为载荷；X-Trace-Id / X-User-Id 写入消息头（审计头透传）。
func (p *NATSPublisher) Publish(ctx context.Context, env Envelope) error {
	subj, err := Subject(env.Source, env.Type)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("mq: 信封序列化失败: %w", err)
	}
	msg := nats.NewMsg(subj)
	msg.Data = payload
	if env.TraceID != "" {
		msg.Header.Set("X-Trace-Id", env.TraceID)
	}
	if env.UserID != "" {
		msg.Header.Set("X-User-Id", env.UserID)
	}
	msg.Header.Set("Nats-Msg-Id", env.ID) // JetStream 幂等发布键
	if p.jsOn && p.js != nil {
		if _, err := p.js.PublishMsg(ctx, msg); err != nil {
			return fmt.Errorf("mq: JetStream 发布失败 %s: %w", subj, err)
		}
		return nil
	}
	if err := p.conn.PublishMsg(msg); err != nil {
		return fmt.Errorf("mq: 发布失败 %s: %w", subj, err)
	}
	return nil
}

// Enabled 连接健康即视为可用。
func (p *NATSPublisher) Enabled() bool { return p != nil && p.conn != nil && p.conn.IsConnected() }

// Close 关闭连接。
func (p *NATSPublisher) Close() error { p.conn.Close(); return nil }
