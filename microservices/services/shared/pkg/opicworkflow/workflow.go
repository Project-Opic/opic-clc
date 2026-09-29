// Package workflow —— OPIC 持久工作流运行时的客户端封装（v3.3 附录 A.2 第 15 项）。
//
// 边界（v3.3 变更单 V33-01 / O-SYS §1.6.1 第 7 行）：
//   - 编排**结构合法性**判定在 O-APP（WorkflowDefinition / INV / PROC-08）——本包不解释编排语义；
//   - 编排**执行**在域外运行时（O-APP 显式声明「执行明细不落本域」）——本包是其 Go 侧接入点；
//   - 执行明细供 O-MON 采集（本包只出 runID 与状态，不落业务库）；
//   - task queue 命名：opic.<中心名>（每中心一个队列，MCC 不共享执行域）；
//   - WorkflowID 与 O-APP 编号族对齐：<WFL-YYYYMMDD-NNNN>-<run 序号>。
package opicworkflow

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

// Runner 持久工作流运行接口（业务代码只依赖此接口，不依赖 Temporal SDK）。
type Runner interface {
	// Start 启动一次工作流运行。wflNo 必须是 O-APP 已登记的 WorkflowDefinition 编号（WFL-YYYYMMDD-NNNN）。
	Start(ctx context.Context, wflNo string, payload []byte) (runID string, err error)
	// Enabled 报告运行时是否真实接入（enabled=false 时为 Noop，如实降级）。
	Enabled() bool
	Close() error
}

// TaskQueue 生成队列名：opic.<center>。
func TaskQueue(center string) (string, error) {
	if center == "" {
		return "", errors.New("workflow: 中心名不能为空")
	}
	return "opic." + center, nil
}

var wflRe = regexp.MustCompile(`^WFL-\d{8}-\d{4}$`)

// WorkflowID 生成与 O-APP 编号族对齐的运行标识：<WFL编号>-<run>。
func WorkflowID(wflNo string, run int) (string, error) {
	if !wflRe.MatchString(wflNo) {
		return "", fmt.Errorf("workflow: WFL 编号格式非法 %q（应为 WFL-YYYYMMDD-NNNN，且必须是 O-APP 已登记的 WorkflowDefinition）", wflNo)
	}
	if run <= 0 {
		return "", errors.New("workflow: run 序号须为正整数")
	}
	return fmt.Sprintf("%s-%06d", wflNo, run), nil
}

// NoopRunner 降级实现（workflow.enabled=false）。
type NoopRunner struct{}

func (NoopRunner) Start(_ context.Context, _ string, _ []byte) (string, error) {
	return "", nil
}
func (NoopRunner) Enabled() bool { return false }
func (NoopRunner) Close() error  { return nil }

// Config 装配参数（来自 config.Config.Workflow）。
type Config struct {
	Enabled   bool
	Host      string
	Namespace string
	TaskQueue string
}

// InitDefault 按配置装配 Runner（main.go 装配点；enabled=false 返回 Noop；连接失败如实降级）。
func InitDefault(cfg Config) Runner {
	if !cfg.Enabled {
		return NoopRunner{}
	}
	r, err := NewTemporalRunner(cfg.Host, cfg.Namespace, cfg.TaskQueue)
	if err != nil {
		return NoopRunner{}
	}
	return r
}
