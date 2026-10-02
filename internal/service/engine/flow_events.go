package engine

// 流程事件接线 —— flow 引擎 × step1 事件总线（pkg/eventbus）。
//
// 事件主题域约定（opic-env-rule / pkg/eventbus）：opic.techbase.flow.<event>；
// payload 为 JSON。发布失败仅记日志，**绝不阻塞审批主流程**（事件是旁路观察）。
// driver=inproc 时为进程内直调（开发/降级），driver=nats 时进 JetStream（at-least-once）。

import (
	"context"
	"encoding/json"
	"log"
)

// 流程事件主题（O-SYS 事件编目预留：主题名即事件类型实现载体）。
const (
	TopicInstanceStarted = "opic.techbase.flow.instance.started"
	TopicTaskCreated     = "opic.techbase.flow.task.created"
	TopicInstanceDone    = "opic.techbase.flow.instance.completed"
	TopicSLAExceeded     = "opic.techbase.flow.sla.exceeded"
)

// publish 发布流程事件（旁路：失败不阻塞）。
func (e *Engine) publish(topic string, payload map[string]any) {
	if e.EB == nil {
		return
	}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[flow-events] 序列化失败 topic=%s: %v", topic, err)
		return
	}
	if err := e.EB.Publish(context.Background(), topic, b); err != nil {
		log.Printf("[flow-events] 发布失败 topic=%s: %v", topic, err)
	}
}
