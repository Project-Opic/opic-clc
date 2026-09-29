// temporal.go —— Temporal 实现 Runner（第 3 层可替换；v3.3 附录 A.2 第 15 项）。
// 业务代码不得直接 import Temporal SDK（契约不漂移判据）。
package workflow

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/client"
)

// TemporalRunner Temporal 实现。
type TemporalRunner struct {
	c         client.Client
	taskQueue string
}

// NewTemporalRunner 连接 Temporal Server（host 如 localhost:7233）。
func NewTemporalRunner(host, namespace, taskQueue string) (*TemporalRunner, error) {
	c, err := client.Dial(client.Options{HostPort: host, Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("workflow: Temporal 连接失败 %s: %w", host, err)
	}
	return &TemporalRunner{c: c, taskQueue: taskQueue}, nil
}

// Start 启动工作流：payload 原样作为输入；WorkflowID 与 O-APP WFL 编号对齐，便于执行明细与 O-MON 关联。
// 超时上界 24h 与 O-APP 批量作业完成窗口承诺同量级；具体窗口由编排定义携带（结构合法性在 O-APP）。
func (r *TemporalRunner) Start(ctx context.Context, wflNo string, payload []byte) (string, error) {
	wfID, err := WorkflowID(wflNo, int(time.Now().UnixNano()%1_000_000)+1) // 轻量 run 序号；建模期可换分布式序
	if err != nil {
		return "", err
	}
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                r.taskQueue,
		WorkflowExecutionTimeout: 24 * time.Hour,
	}
	if _, err := r.c.ExecuteWorkflow(ctx, opts, wflNo, payload); err != nil {
		return "", fmt.Errorf("workflow: 启动失败 %s: %w", wfID, err)
	}
	return wfID, nil
}

// Enabled 连接健康即视为可用。
func (r *TemporalRunner) Enabled() bool { return r != nil && r.c != nil }

// Close 关闭连接。
func (r *TemporalRunner) Close() error { r.c.Close(); return nil }
