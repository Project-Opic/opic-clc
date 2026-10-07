// Package delivery —— 交付执行内核(第 3 层交付工具适配器,B-AUTO-10 / FLOW-PROC-06)。
//
// 分层可插拔「交付执行适配器」裁定(m3 REF-16):形态在任务上登记后运行期不互转;
// local = 后端进程直调交付工具(无 shell,参数数组直调,INV-47),逐行采集 + 剧本汇总判定;
// ignition = 经点火中心提交远端执行 + 回执对账(clc 不拥有远端动作白名单语义)。
package delivery

import "context"

// 适配器形态(CLC_EXEC_PATH,m1 ClusterTask.executionPath;REF-16)。
const (
	FormLocal  = "LOCAL"
	FormRemote = "REMOTE_EXECUTOR"
)

// 单步判定依据(CLC_STEP_VERDICT,m3 / B-AUTO-10)。
const (
	VerdictExitCode      = "EXIT_CODE"
	VerdictPlayRecap     = "PLAY_RECAP"
	VerdictRemoteReceipt = "REMOTE_RECEIPT"
)

// 日志级别(CLC_LOG_LEVEL;INV-42 落库前掩码由调用方承载)。
const (
	LevelInfo  = "INFO"
	LevelWarn  = "WARN"
	LevelError = "ERROR"
)

// NodeSpec 交付门面请求体的节点清单条目（R-19 三档取用 / O-IGN 侧裁定 Q-72：清单进请求体，两侧各校验一次）。
//
// 只登记标识与本域事实：地址 + 角色（CLC_NODE_ROLE 字典码原文）+ K8s 节点名。
// 角色词汇由被调用方归一（其 R-13），本域不翻译；不带角色即"只给地址"（节点增删类的命令面口径）。
type NodeSpec struct {
	Addr     string `json:"addr"`
	Role     string `json:"role,omitempty"`
	Nodename string `json:"nodename,omitempty"`
}

// StepRef 单步执行请求（远程链路的取用口径见 R-19:哪些字段带、哪些字段一律不填）。
//
// 本地链路:Args = 已校验的命令数组(INV-47,不经 shell 直调)。
// 远程链路:Args 一律不填 —— 命令面归点火中心私有(其 R-13),本域只登记标识:
// StepCode(安装族 01~07;其余族与任务类别同名,REF-13 既有契约)+
// Nodes(节点清单:安装族为在册节点全量拓扑、节点增删类为本次目标地址)+
// VersionRef(安装族的交付版本引用,可空即沿用发行包自身版本)。
type StepRef struct {
	TaskNo     string
	ClusterID  string
	StepIndex  int
	StepCode   string
	Nodes      []NodeSpec
	VersionRef string
	Args       []string
}

// Line 采集到的一行交付输出(级别 + 原文;掩码与落库在服务层)。
type Line struct {
	Level string
	Text  string
}

// LineSink 逐行回调(单步执行期间被并发调用,实现方须自行保证安全)。
type LineSink func(Line)

// Outcome 单步结论(B-AUTO-10 / A13 判定网关)。
type Outcome struct {
	Success   bool
	Verdict   string // CLC_STEP_VERDICT;终局必填(INV-38)
	ExitCode  int
	TimedOut  bool   // 单步超时按失败落行(REF-15)
	Cancelled bool   // 取消处置(R-15)
	Lost      bool   // 进程不可存活(B-AUTO-11:失联,不是失败)
	RemoteRef string // 远端任务引用(INV-46:远程链路必写)
	Detail    string // 结论摘要(失败定位;落库前须掩码)
}

// Adapter 交付执行适配器(两实现互斥登记,REF-16)。
//
// Execute 阻塞直至单步有结论;ctx 取消 = 取消该步(local 终止进程组 / ignition 撤销远端任务)。
type Adapter interface {
	Form() string
	Execute(ctx context.Context, ref StepRef, sink LineSink) Outcome
}
