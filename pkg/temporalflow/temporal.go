package temporalflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"gitcode.com/opic-ontology/opic-techbase/pkg/eventbus"
)

// TemporalExecutor temporal 驱动原语（workflow 上下文内执行）。
type TemporalExecutor struct {
	Ctx workflow.Context
}

// Emit 发布步骤事件 —— temporal workflow 内不允许直接网络 IO，
// 经 side_effect activity 执行（服务端记录、重放安全）。
func (t *TemporalExecutor) Emit(ctx context.Context, topic string, payload map[string]any) error {
	b, _ := json.Marshal(payload)
	opts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	}
	actCtx := workflow.WithActivityOptions(t.Ctx, opts)
	return workflow.ExecuteActivity(actCtx, "EmitEventActivity", topic, json.RawMessage(b)).Get(actCtx, nil)
}

// Webhook HTTP 回调经 activity。
func (t *TemporalExecutor) Webhook(ctx context.Context, url string, payload map[string]any) error {
	b, _ := json.Marshal(payload)
	opts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	}
	actCtx := workflow.WithActivityOptions(t.Ctx, opts)
	return workflow.ExecuteActivity(actCtx, "WebhookActivity", url, json.RawMessage(b)).Get(actCtx, nil)
}

// Sleep 服务端持久化定时器（进程重启不丢 —— temporal 核心价值）。
func (t *TemporalExecutor) Sleep(_ context.Context, d time.Duration) error {
	return workflow.Sleep(t.Ctx, d)
}

// TemporalInterpreterWorkflow 通用解释器工作流（注册名 "temporalInterpreter"）。
// 输入 = RunInput（含 DSL 图 + 运行变量）；workflow 非确定性安全：
// 图/变量来自工作流参数（重放一致），Expr 求值为纯函数。
func TemporalInterpreterWorkflow(ctx workflow.Context, input RunInput) (*RunResult, error) {
	g, err := Parse(input.Graph)
	if err != nil {
		return nil, err
	}
	ex := &TemporalExecutor{Ctx: ctx}
	// Run 骨架的 ctx 仅透传给 executor 原语（temporal 原语内部用 workflow.Context），
	// 故此处传标准 context 即可
	res, err := Run(context.Background(), g, input, ex)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ── Activities（非确定性原语，普通函数注册）──────────────────────────────────

// EmitEventActivity 发布事件到事件总线（NATS）。
func EmitEventActivity(topic string, payload json.RawMessage) error {
	eb, err := eventbus.New(eventbus.Options{
		Driver: "nats",
		URL:    envOr("EVENTBUS_URL", "nats://opic-nats:4222"),
		Username: envOr("EVENTBUS_USERNAME", "opic"),
		Password: envOr("EVENTBUS_PASSWORD", ""),
		StreamName: "OPIC",
	})
	if err != nil {
		return err
	}
	defer eb.Close()
	b, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return eb.Publish(ctx, topic, b)
}

// WebhookActivity HTTP 回调。
func WebhookActivity(url string, payload json.RawMessage) error {
	var m map[string]any
	_ = json.Unmarshal(payload, &m)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return postJSON(ctx, url, m)
}

// StartInterpreter 启动解释器工作流（temporal 驱动；供 API 层调用）。
func StartInterpreter(address, namespace string, in RunInput) (runID string, err error) {
	c, err := client.Dial(client.Options{HostPort: address, Namespace: namespace})
	if err != nil {
		return "", fmt.Errorf("连接 Temporal %s 失败: %w", address, err)
	}
	defer c.Close()
	wctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	co := client.StartWorkflowOptions{
		ID:                       fmt.Sprintf("temporalflow-%s-%s", in.Code, in.RunID),
		TaskQueue:                "techbase",
		WorkflowExecutionTimeout: 24 * time.Hour,
	}
	run, err := c.ExecuteWorkflow(wctx, co, TemporalInterpreterWorkflow, in)
	if err != nil {
		return "", err
	}
	return run.GetRunID(), nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
