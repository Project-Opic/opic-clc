package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// IgnitionConfig 点火中心链路配置(远端动作白名单语义归交付域,clc 不拥有)。
type IgnitionConfig struct {
	BaseURL      string
	Token        string
	SubmitPath   string // 默认 /api/delivery/submit
	ReceiptPath  string // 默认 /api/delivery/receipt/{ref}
	CancelPath   string // 默认 /api/delivery/cancel/{ref}
	PollInterval time.Duration
	HTTP         *http.Client
}

// Ignition 远程链路适配器:HTTP 提交 + 事件回执轮询对账(B-AUTO-12 / R-16)。
type Ignition struct {
	cfg IgnitionConfig
}

// NewIgnition 构造(缺省路径与轮询周期补齐)。
func NewIgnition(cfg IgnitionConfig) *Ignition {
	if cfg.SubmitPath == "" {
		cfg.SubmitPath = "/api/delivery/submit"
	}
	if cfg.ReceiptPath == "" {
		cfg.ReceiptPath = "/api/delivery/receipt/{ref}"
	}
	if cfg.CancelPath == "" {
		cfg.CancelPath = "/api/delivery/cancel/{ref}"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	return &Ignition{cfg: cfg}
}

// Form 适配器形态(REF-16)。
func (a *Ignition) Form() string { return FormRemote }

// submitReq 提交意图:只携带登记标识(集群/步骤/任务),不携带命令面 —— 远端动作白名单语义在对端。
type submitReq struct {
	TaskNo    string `json:"task_no"`
	ClusterNo string `json:"cluster_no"`
	StepIndex int    `json:"step_index"`
	StepCode  string `json:"step_code"`
}

type submitResp struct {
	RemoteTaskRef string `json:"remote_task_ref"`
	Status        string `json:"status"`
}

type receiptResp struct {
	Status   string `json:"status"` // RUNNING / SUCCEEDED / FAILED / CANCELLED / LOST
	ExitCode *int   `json:"exit_code"`
	Summary  string `json:"summary"`
}

func (a *Ignition) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.cfg.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	}
	resp, err := a.cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("点火中心返回 %d: %s", resp.StatusCode, string(raw))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Execute 提交单步执行意图并轮询回执;远端引用缺失即失败(INV-46),
// ctx 取消时撤销远端任务(FUNC-33 远端链路)。
func (a *Ignition) Execute(ctx context.Context, ref StepRef, sink LineSink) Outcome {
	if a.cfg.BaseURL == "" {
		return Outcome{Lost: true, Detail: "远程链路未配置点火中心地址(部署配置 delivery.ignition_base_url 缺失)"}
	}
	var sub submitResp
	if err := a.do(ctx, http.MethodPost, a.cfg.SubmitPath, submitReq{
		TaskNo: ref.TaskNo, ClusterNo: ref.ClusterID, StepIndex: ref.StepIndex, StepCode: ref.StepCode,
	}, &sub); err != nil {
		return Outcome{Detail: "提交远端执行失败: " + err.Error()}
	}
	// INV-46:remote_task_ref 必写 —— 登记为远程却无远端引用即登记值与真实位置不符
	if sub.RemoteTaskRef == "" {
		return Outcome{Detail: "远端回执缺少任务引用(INV-46),远程执行不可信,即登记失败"}
	}
	emit := func(level, text string) {
		if sink != nil {
			sink(Line{Level: level, Text: text})
		}
	}
	emit(LevelInfo, fmt.Sprintf("[回执] 远端任务 %s 已受理(步骤 %02d)", sub.RemoteTaskRef, ref.StepIndex))

	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()
	for {
		var rc receiptResp
		err := a.do(ctx, http.MethodGet, pathWithRef(a.cfg.ReceiptPath, sub.RemoteTaskRef), nil, &rc)
		if err != nil {
			emit(LevelWarn, "[回执] 查询失败(将随保留周期重试): "+err.Error())
		} else {
			switch rc.Status {
			case "SUCCEEDED":
				return Outcome{Success: true, Verdict: VerdictRemoteReceipt, RemoteRef: sub.RemoteTaskRef,
					Detail: rc.Summary, ExitCode: derefInt(rc.ExitCode)}
			case "FAILED":
				return Outcome{Verdict: VerdictRemoteReceipt, RemoteRef: sub.RemoteTaskRef,
					Detail: nonEmpty(rc.Summary, "远端回执结论:失败")}
			case "CANCELLED":
				return Outcome{Cancelled: true, Verdict: VerdictRemoteReceipt, RemoteRef: sub.RemoteTaskRef,
					Detail: nonEmpty(rc.Summary, "远端回执结论:已取消")}
			case "LOST":
				return Outcome{Lost: true, RemoteRef: sub.RemoteTaskRef,
					Detail: nonEmpty(rc.Summary, "远端执行痕迹不可存活(B-AUTO-11)")}
			default:
				emit(LevelInfo, fmt.Sprintf("[回执] 远端任务 %s 执行中(步骤 %02d)…", sub.RemoteTaskRef, ref.StepIndex))
			}
		}
		select {
		case <-ctx.Done():
			if cerr := a.do(context.Background(), http.MethodPost,
				pathWithRef(a.cfg.CancelPath, sub.RemoteTaskRef), nil, nil); cerr != nil {
				emit(LevelWarn, "[取消] 远端撤销失败(残留可见,人工处置): "+cerr.Error())
			} else {
				emit(LevelInfo, "[取消] 已请求远端撤销(INV-46 引用 "+sub.RemoteTaskRef+")")
			}
			return Outcome{Cancelled: true, Verdict: VerdictRemoteReceipt, RemoteRef: sub.RemoteTaskRef,
				Detail: "人工取消:已请求远端撤销"}
		case <-ticker.C:
		}
	}
}

// Reconcile 以既有远端引用回读结论(B-AUTO-12 / R-16:失联恢复与回执对账以远端为准,
// 绝不重复提交;ctx 取消时同样走远端撤销链)。
func (a *Ignition) Reconcile(ctx context.Context, remoteRef string, sink LineSink) Outcome {
	if a.cfg.BaseURL == "" || remoteRef == "" {
		return Outcome{Lost: true, Detail: "对账缺少前提(点火中心地址或远端任务引用为空)"}
	}
	emit := func(level, text string) {
		if sink != nil {
			sink(Line{Level: level, Text: text})
		}
	}
	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()
	for {
		var rc receiptResp
		if err := a.do(ctx, http.MethodGet, pathWithRef(a.cfg.ReceiptPath, remoteRef), nil, &rc); err != nil {
			emit(LevelWarn, "[对账] 回执查询失败(将随保留周期重试): "+err.Error())
		} else {
			switch rc.Status {
			case "SUCCEEDED":
				return Outcome{Success: true, Verdict: VerdictRemoteReceipt, RemoteRef: remoteRef,
					Detail: rc.Summary, ExitCode: derefInt(rc.ExitCode)}
			case "FAILED":
				return Outcome{Verdict: VerdictRemoteReceipt, RemoteRef: remoteRef,
					Detail: nonEmpty(rc.Summary, "对账修正:远端结论为失败(R-16 以远端为准)")}
			case "CANCELLED":
				return Outcome{Cancelled: true, Verdict: VerdictRemoteReceipt, RemoteRef: remoteRef,
					Detail: nonEmpty(rc.Summary, "对账修正:远端结论为已取消(R-16 以远端为准)")}
			case "LOST":
				return Outcome{Lost: true, RemoteRef: remoteRef,
					Detail: nonEmpty(rc.Summary, "远端执行痕迹不可存活(B-AUTO-11)")}
			default:
				emit(LevelInfo, "[对账] 远端任务 "+remoteRef+" 仍在执行…")
			}
		}
		select {
		case <-ctx.Done():
			if cerr := a.do(context.Background(), http.MethodPost,
				pathWithRef(a.cfg.CancelPath, remoteRef), nil, nil); cerr != nil {
				emit(LevelWarn, "[取消] 远端撤销失败(残留可见,人工处置): "+cerr.Error())
			}
			return Outcome{Cancelled: true, Verdict: VerdictRemoteReceipt, RemoteRef: remoteRef,
				Detail: "人工取消:已请求远端撤销"}
		case <-ticker.C:
		}
	}
}

func pathWithRef(tpl, ref string) string {
	return replaceAll(tpl, "{ref}", ref)
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func nonEmpty(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
