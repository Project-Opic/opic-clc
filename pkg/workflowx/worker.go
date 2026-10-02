package workflowx

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"
	sdkworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

type workerRunner struct {
	c         client.Client
	taskQueue string
}

func (w *workerRunner) Register(fn any) {
	worker := sdkworker.New(w.c, w.taskQueue, sdkworker.Options{})
	worker.RegisterWorkflow(fn)
}

// RegisterNamed 以显式名称注册工作流函数（client 按 name 调度时需一致）。
func (w *workerRunner) RegisterNamed(name string, fn any) {
	worker := sdkworker.New(w.c, w.taskQueue, sdkworker.Options{})
	worker.RegisterWorkflowWithOptions(fn, workflow.RegisterOptions{Name: name})
}

func (w *workerRunner) Run(ctx context.Context) error {
	worker := sdkworker.New(w.c, w.taskQueue, sdkworker.Options{})
	if err := worker.Start(); err != nil {
		return fmt.Errorf("workflowx: worker 启动失败: %w", err)
	}
	<-ctx.Done()
	worker.Stop()
	return nil
}

func (w *workerRunner) Close() { w.c.Close() }
