package workflowx

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/client"
)

// temporalClient Temporal 生产实现（step1 stacks/temporal，业务命名空间 opic）。
//
// 工作流**函数**在中心应用侧注册进 worker（NewWorkerWorker 路由到本包提供配置）；
// 本客户端只承担启动（按 workflow name + 单参数 []byte 输入）、状态查询与取消。
type temporalClient struct {
	c   client.Client
	ns  string
	tq  string
}

// NewTemporal 连接 Temporal frontend。
func NewTemporal(opts Options) (Client, error) {
	if opts.Address == "" {
		return nil, fmt.Errorf("workflowx: temporal driver 需要 Address")
	}
	ns := opts.Namespace
	if ns == "" {
		ns = "opic"
	}
	tq := "techbase"
	c, err := client.Dial(client.Options{
		HostPort:  opts.Address,
		Namespace: ns,
	})
	if err != nil {
		return nil, fmt.Errorf("workflowx: 连接 Temporal %s 失败: %w", opts.Address, err)
	}
	return &temporalClient{c: c, ns: ns, tq: tq}, nil
}

func (t *temporalClient) Start(ctx context.Context, name string, _ WorkflowFunc, opts StartOptions) (RunInfo, error) {
	if opts.ID == "" {
		opts.ID = fmt.Sprintf("%s-%d", name, time.Now().UnixNano())
	}
	co := client.StartWorkflowOptions{
		ID:        opts.ID,
		TaskQueue: t.tq,
	}
	if opts.TaskQueue != "" {
		co.TaskQueue = opts.TaskQueue
	}
	if opts.RunTimeoutSec > 0 {
		co.WorkflowExecutionTimeout = time.Duration(opts.RunTimeoutSec) * time.Second
	} else if opts.Timeout > 0 {
		co.WorkflowExecutionTimeout = opts.Timeout
	}
	run, err := t.c.ExecuteWorkflow(ctx, co, name, opts.Input)
	if err != nil {
		return RunInfo{}, fmt.Errorf("workflowx: 启动 %s 失败: %w", name, err)
	}
	return RunInfo{ID: opts.ID, RunID: run.GetRunID(), Status: StatusRunning}, nil
}

func (t *temporalClient) Status(ctx context.Context, id string) (RunInfo, error) {
	resp, err := t.c.DescribeWorkflowExecution(ctx, id, "")
	if err != nil {
		return RunInfo{}, ErrNotFound
	}
	info := RunInfo{ID: id, RunID: resp.WorkflowExecutionInfo.GetExecution().GetRunId()}
	switch resp.WorkflowExecutionInfo.GetStatus() {
	case 1: // RUNNING
		info.Status = StatusRunning
	case 2: // COMPLETED
		info.Status = StatusDone
	case 3: // FAILED
		info.Status = StatusFailed
	case 4: // CANCELED / TERMINATED
		info.Status = StatusCanceled
	default:
		info.Status = StatusUnknown
	}
	return info, nil
}

func (t *temporalClient) Cancel(ctx context.Context, id string) error {
	return t.c.CancelWorkflow(ctx, id, "")
}

func (t *temporalClient) Close() error { t.c.Close(); return nil }

// NewWorkerWorker 构造 Temporal worker（中心应用注册工作流函数后 Run()）。
// 返回 *worker.Worker 以便中心调用 RegisterWorkflow/RegisterActivity。
func NewWorkerWorker(opts Options, taskQueue string) (WorkerRunner, error) {
	ns := opts.Namespace
	if ns == "" {
		ns = "opic"
	}
	c, err := client.Dial(client.Options{HostPort: opts.Address, Namespace: ns})
	if err != nil {
		return nil, fmt.Errorf("workflowx: worker 连接失败: %w", err)
	}
	if taskQueue == "" {
		taskQueue = "techbase"
	}
	return &workerRunner{c: c, taskQueue: taskQueue}, nil
}

// WorkerRunner worker 运行句柄。
type WorkerRunner interface {
	// Register 注册工作流函数（temporal worker 原生语义，fn 由应用用 sdk workflow 装饰器定义）。
	Register(fn any)
	// Run 阻塞运行 worker（graceful 停止经 ctx）。
	Run(ctx context.Context) error
	Close()
}
