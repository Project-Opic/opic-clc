package temporalflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/expr"
	"gitcode.com/opic-ontology/opic-techbase/pkg/eventbus"
)

// 事件主题（step 执行轨迹进事件总线，供观测/审计/联动）。
const (
	TopicStepStarted   = "opic.techbase.temporalflow.step.started"
	TopicStepCompleted = "opic.techbase.temporalflow.step.completed"
	TopicRunCompleted  = "opic.techbase.temporalflow.run.completed"
)

// RunInput 解释器工作流输入（Start 时经 StartOptions.Input 传入）。
type RunInput struct {
	DefID    int64          `json:"def_id"`
	Code     string         `json:"code"`
	RunID    string         `json:"run_id"`
	Graph    json.RawMessage `json:"graph"`
	Vars     map[string]any `json:"vars"`
}

// StepExecutor 步骤执行 + 计时原语（双驱动各自的实现面）。
type StepExecutor interface {
	// Emit 发布步骤事件（step.started/completed）。
	Emit(ctx context.Context, topic string, payload map[string]any) error
	// Webhook HTTP POST 回调（step 可选动作）。
	Webhook(ctx context.Context, url string, payload map[string]any) error
	// Sleep 计时（temporal: workflow.Sleep 服务端持久化；local: time.After）。
	Sleep(ctx context.Context, d time.Duration) error
}

// RunResult 运行结果摘要。
type RunResult struct {
	Steps    int    `json:"steps"`
	Finished bool   `json:"finished"`
	Error    string `json:"error,omitempty"`
}

// Run 解释执行（驱动无关骨架：Sleep/Emit/Webhook 由 StepExecutor 提供）。
// 每步：Emit step.started →（timer: Sleep / step: 可选 webhook）→ Emit step.completed → next。
func Run(ctx context.Context, g *Graph, in RunInput, ex StepExecutor) (*RunResult, error) {
	cur, err := startID(g)
	if err != nil {
		return nil, err
	}
	res := &RunResult{}
	for i := 0; i < MaxSteps; i++ {
		n := nodeById(g, cur)
		if n == nil {
			return res, fmt.Errorf("节点 %s 不存在", cur)
		}
		switch nodeType(n) {
		case NodeEnd:
			res.Finished = true
			_ = ex.Emit(ctx, TopicRunCompleted, map[string]any{
				"run_id": in.RunID, "def_id": in.DefID, "code": in.Code, "steps": res.Steps,
			})
			return res, nil
		case NodeTimer:
			sec, _ := n["seconds"].(float64)
			if sec <= 0 {
				sec = 1
			}
			if err := ex.Emit(ctx, TopicStepStarted, stepPayload(in, n, res.Steps)); err != nil {
				return res, err
			}
			if err := ex.Sleep(ctx, time.Duration(sec)*time.Second); err != nil {
				return res, err
			}
			_ = ex.Emit(ctx, TopicStepCompleted, stepPayload(in, n, res.Steps))
		case NodeStep:
			name, _ := n["name"].(string)
			webhook, _ := n["webhook_url"].(string)
			if err := ex.Emit(ctx, TopicStepStarted, stepPayload(in, n, res.Steps)); err != nil {
				return res, err
			}
			if webhook != "" {
				if err := ex.Webhook(ctx, webhook, map[string]any{
					"run_id": in.RunID, "code": in.Code, "step": name, "vars": in.Vars,
				}); err != nil {
					return res, fmt.Errorf("step %s webhook 失败: %w", name, err)
				}
			}
			_ = ex.Emit(ctx, TopicStepCompleted, stepPayload(in, n, res.Steps))
		case NodeCondition:
			// 条件节点零副作用：只求值选路（确定性——expr 纯函数 + vars 为工作流参数）
		}
		res.Steps++
		next, err := nextID(g, cur, func(e string) bool { return expr.EvalBool(e, in.Vars) })
		if err != nil {
			return res, err
		}
		if next == "" {
			// 无 END 直达终点（设计缺陷宽容：视为结束）
			res.Finished = true
			_ = ex.Emit(ctx, TopicRunCompleted, map[string]any{
				"run_id": in.RunID, "def_id": in.DefID, "code": in.Code, "steps": res.Steps,
			})
			return res, nil
		}
		cur = next
	}
	return res, fmt.Errorf("超过最大步数 %d（图可能成环）", MaxSteps)
}

func stepPayload(in RunInput, n map[string]any, idx int) map[string]any {
	name, _ := n["name"].(string)
	if name == "" {
		name, _ = n["type"].(string)
	}
	return map[string]any{
		"run_id": in.RunID, "def_id": in.DefID, "code": in.Code,
		"step": name, "node_id": nodeID(n), "index": idx, "vars": in.Vars,
	}
}

// ── local 驱动 StepExecutor（进程内 eventbus 直发；开发/降级/测试）────────────

// LocalExecutor local 实现。
type LocalExecutor struct{ EB eventbus.EventBus }

// Emit 发布事件。
func (l *LocalExecutor) Emit(ctx context.Context, topic string, payload map[string]any) error {
	if l.EB == nil {
		return nil
	}
	b, _ := json.Marshal(payload)
	return l.EB.Publish(ctx, topic, b)
}

// Webhook 本地实现直连 HTTP。
func (l *LocalExecutor) Webhook(ctx context.Context, url string, payload map[string]any) error {
	return postJSON(ctx, url, payload)
}

// Sleep 本地计时。
func (l *LocalExecutor) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// DecodeInput 从 StartOptions.Input 解析运行输入。
func DecodeInput(input []byte) (*RunInput, *Graph, error) {
	var in RunInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, nil, fmt.Errorf("运行输入解析失败: %w", err)
	}
	g, err := Parse(in.Graph)
	if err != nil {
		return nil, nil, err
	}
	return &in, g, nil
}
