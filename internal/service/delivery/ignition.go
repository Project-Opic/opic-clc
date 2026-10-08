package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 点火中心交付门面契约(O-IGN FUNC-34~36,需求文档 3.2.1 / 8.3):
// 词汇由被调用方定义 —— 回执状态取 IGN_TASK_STATUS 字典码(成功 = COMPLETED),
// 请求体只携带登记标识与本域事实(来源任务号 / 来源步骤序号 / 集群标识 / 步骤编码 /
// 节点清单 / 交付版本引用),不含命令、参数数组与工作目录(命令面映射归点火中心私有,其 R-13)。
const (
	defaultSubmitPath  = "/api/v1/ign/delivery/submit"
	defaultReceiptPath = "/api/v1/ign/delivery/receipt/{ref}"
	defaultCancelPath  = "/api/v1/ign/delivery/cancel/{ref}"
	// defaultConfigPath O-IGN FUNC-50 跨中心交付配置只读通道(字段级视图;CLC FUNC-48 的对端)。
	defaultConfigPath = "/api/v1/ign/delivery/config/{ref}"
	// configRawSuffix 同一组内的原文只读通道(必带 file_face、一次一面;CLC FUNC-49 的对端)。
	configRawSuffix = "/raw"
)

// ErrPeerNotConfigured 未配置对端地址(与「对端不可达」分列:一个是本部署没接线,一个是接了线连不上)。
var ErrPeerNotConfigured = errors.New("远程链路未配置点火中心地址(部署配置 delivery.ignition_base_url 缺失)")

// 远端终态(IGN_TASK_STATUS;LOST 为第四终态,与 FAILED/CANCELLED 分列)。
const (
	remoteCompleted = "COMPLETED"
	remoteFailed    = "FAILED"
	remoteCancelled = "CANCELLED"
	remoteLost      = "LOST"
)

// IgnitionConfig 点火中心链路配置(远端动作白名单语义归交付域,clc 不拥有)。
type IgnitionConfig struct {
	BaseURL      string
	Token        string
	SubmitPath   string // 默认 /api/v1/ign/delivery/submit
	ReceiptPath  string // 默认 /api/v1/ign/delivery/receipt/{ref}
	CancelPath   string // 默认 /api/v1/ign/delivery/cancel/{ref}
	PollInterval time.Duration
	HTTP         *http.Client
}

// Ignition 远程链路适配器:门面受理 + 回执轮询对账(B-AUTO-12 / R-16)。
type Ignition struct {
	cfg IgnitionConfig
}

// NewIgnition 构造(缺省路径与轮询周期补齐)。
func NewIgnition(cfg IgnitionConfig) *Ignition {
	if cfg.SubmitPath == "" {
		cfg.SubmitPath = defaultSubmitPath
	}
	if cfg.ReceiptPath == "" {
		cfg.ReceiptPath = defaultReceiptPath
	}
	if cfg.CancelPath == "" {
		cfg.CancelPath = defaultCancelPath
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

// submitReq 受理请求(INV-34 跨域只存标识 / R-19 登记事实携带:字段就是本域能送出去的边界形态)。
//
// action_code 留空:点火中心按「步骤编码命中 01~07 ⇒ 安装族单步 / 命中本域任务类别名 ⇒
// 私有映射取单步动作码」解析(其 R-13),本域不下发命令面词汇;两者都空才是治理拒绝。
//
// nodes 是结构化清单(O-IGN 侧 Q-72):安装族带在册节点全量(地址 + 角色 + 节点名),对端据此派生
// 集群交付配置(inventory);节点增删类只带本次目标地址(角色留空 = 对端的"只给地址 ⇒ 工作节点")。
type submitReq struct {
	SourceTaskNo    string     `json:"source_task_no"`
	SourceStepIndex int        `json:"source_step_index"`
	ClusterRef      string     `json:"cluster_ref"`
	ActionCode      string     `json:"action_code,omitempty"`
	StepCode        string     `json:"step_code,omitempty"`
	VersionRef      string     `json:"version_ref,omitempty"`
	Nodes           []NodeSpec `json:"nodes,omitempty"`
}

type submitResp struct {
	RemoteTaskRef string `json:"remote_task_ref"`
	TaskNo        string `json:"task_no"`
	Status        string `json:"status"`
	Reused        bool   `json:"reused"`
	Note          string `json:"note"`
}

// receiptResp 回执体(IGN_TASK_STATUS 字典码;LOST 与 FAILED/CANCELLED 分列)。
//
// 回执也带增量日志行,但本域**不解析、不落库**(clc 需求 REF-12 / 口径 8:远程链路的原始流
// 只在点火中心侧,本域界面给跳转、不留副本)。next_seq 仍要消费 —— 它是 since_seq 游标的推进量,
// 不推则对端每轮都从第 0 行重吐一整页(白占带宽)。
type receiptResp struct {
	Status     string `json:"status"` // RUNNING/CANCELLING/COMPLETED/FAILED/CANCELLED/LOST
	ExitCode   *int   `json:"exit_code"`
	FailReason string `json:"fail_reason"`
	Summary    string `json:"summary"`
	NextSeq    int64  `json:"next_seq"`
	Truncated  bool   `json:"truncated"`
}

// httpStatusError 门面错误体是 flat JSON {return_code, error_msg}(非 200),不是本域信封。
type httpStatusError struct {
	Code int
	Msg  string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("点火中心返回 %d：%s", e.Code, e.Msg)
}

func (a *Ignition) do(ctx context.Context, method, path string, query string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	url := a.cfg.BaseURL + path
	if query != "" {
		url += "?" + query
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
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
		var flat struct {
			ReturnCode string `json:"return_code"`
			ErrorMsg   string `json:"error_msg"`
		}
		_ = json.Unmarshal(raw, &flat)
		msg := strings.TrimSpace(flat.ErrorMsg)
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		if msg == "" {
			msg = "无错误说明"
		}
		if flat.ReturnCode != "" {
			msg = flat.ReturnCode + " " + msg
		}
		return &httpStatusError{Code: resp.StatusCode, Msg: msg}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Execute 受理单步执行意图并轮询回执结论;远端引用缺失即失败(INV-46),
// ctx 取消时撤销远端任务(FUNC-33 远端链路)。
func (a *Ignition) Execute(ctx context.Context, ref StepRef, sink LineSink) Outcome {
	if a.cfg.BaseURL == "" {
		return Outcome{Lost: true, Detail: "远程链路未配置点火中心地址(部署配置 delivery.ignition_base_url 缺失)"}
	}
	var sub submitResp
	if err := a.do(ctx, http.MethodPost, a.cfg.SubmitPath, "", submitReq{
		SourceTaskNo: ref.TaskNo, SourceStepIndex: ref.StepIndex, ClusterRef: ref.ClusterID,
		StepCode: ref.StepCode, VersionRef: ref.VersionRef, Nodes: ref.Nodes,
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
	accepted := fmt.Sprintf("[受理] 远端任务 %s 已受理(步骤 %02d)", sub.RemoteTaskRef, ref.StepIndex)
	if sub.Reused {
		accepted += ";幂等复用:命中同一「来源任务号 + 步骤序号」的在途任务"
	}
	if sub.Note != "" {
		accepted += ":" + sub.Note
	}
	emit(LevelInfo, accepted)

	res := a.poll(ctx, sub.RemoteTaskRef, sink)
	res.RemoteRef = sub.RemoteTaskRef
	if res.Verdict == "" {
		res.Verdict = VerdictRemoteReceipt
	}
	return res
}

// Reconcile 以既有远端引用回读结论(B-AUTO-12 / R-16:失联恢复与回执对账以远端为准,
// 绝不重复提交;ctx 取消时同样走远端撤销链)。
func (a *Ignition) Reconcile(ctx context.Context, remoteRef string, sink LineSink) Outcome {
	if a.cfg.BaseURL == "" || remoteRef == "" {
		return Outcome{Lost: true, Detail: "对账缺少前提(点火中心地址或远端任务引用为空)"}
	}
	res := a.poll(ctx, remoteRef, sink)
	if res.RemoteRef == "" {
		res.RemoteRef = remoteRef
	}
	if res.Verdict == "" {
		res.Verdict = VerdictRemoteReceipt
	}
	return res
}

// poll 回执轮询:等终态结论(状态 + 退出码 + 摘要),同时推进对端日志游标;
// truncated ⇒ 立即续读同周期下一批(点火中心 REF-14 单页上限,不推游标会反复重吐同一页)。
func (a *Ignition) poll(ctx context.Context, remoteRef string, sink LineSink) Outcome {
	emit := func(level, text string) {
		if sink != nil {
			sink(Line{Level: level, Text: text})
		}
	}
	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()
	var since int64
	for {
		var rc receiptResp
		err := a.do(ctx, http.MethodGet, pathWithRef(a.cfg.ReceiptPath, remoteRef),
			fmt.Sprintf("since_seq=%d", since), nil, &rc)
		if err != nil {
			var se *httpStatusError
			if asStatus(err, &se) && se.Code == http.StatusNotFound {
				// 引用未命中:登记值与真实位置不符(点火中心 FUNC-35 口径),不静默重试
				return Outcome{Lost: true, Detail: "远端任务引用未命中(INV-46):" + se.Msg}
			}
			emit(LevelWarn, "[回执] 查询失败(将随保留周期重试): "+err.Error())
		} else {
			if rc.NextSeq > since {
				since = rc.NextSeq
			}
			if rc.Truncated {
				continue // 单页上限截断:立刻续读同周期下一批(REF-14 对端侧)
			}
			switch rc.Status {
			case remoteCompleted:
				return Outcome{Success: true, RemoteRef: remoteRef,
					Detail: nonEmpty(rc.Summary, "远端结论:完成"), ExitCode: derefInt(rc.ExitCode)}
			case remoteFailed:
				return Outcome{RemoteRef: remoteRef,
					Detail:   nonEmpty(rc.Summary, nonEmpty(rc.FailReason, "远端回执结论:失败")),
					ExitCode: derefInt(rc.ExitCode)}
			case remoteCancelled:
				return Outcome{Cancelled: true, RemoteRef: remoteRef,
					Detail: nonEmpty(rc.Summary, "远端回执结论:已取消")}
			case remoteLost:
				return Outcome{Lost: true, RemoteRef: remoteRef,
					Detail: nonEmpty(rc.Summary, "远端执行痕迹不可存活(点火中心 INV-37:退出码为空,与失败分列)")}
			default: // RUNNING / CANCELLING 等未终态
				if rc.Status == "CANCELLING" {
					emit(LevelWarn, "[回执] 远端撤销在途,终态由执行事实决定:"+remoteRef)
				}
			}
		}
		select {
		case <-ctx.Done():
			a.revoke(remoteRef, emit)
			return Outcome{Cancelled: true, RemoteRef: remoteRef,
				Detail: "人工取消:已请求远端撤销"}
		case <-ticker.C:
		}
	}
}

// revoke 远端撤销(FUNC-36);撤销失败不代答终态 —— 残留可见,由人工处置(R-16 以远端为准)。
func (a *Ignition) revoke(remoteRef string, emit func(level, text string)) {
	if err := a.do(context.Background(), http.MethodPost,
		pathWithRef(a.cfg.CancelPath, remoteRef), "", nil, nil); err != nil {
		emit(LevelWarn, "[取消] 远端撤销失败(残留可见,人工处置): "+err.Error())
		return
	}
	emit(LevelInfo, "[取消] 已请求远端撤销(INV-46 引用 "+remoteRef+")")
}

// ConfigQuery 字段级视图的取数上下文(O-IGN FUNC-50 只读通道的三个可选入参)。
// 留空即由对端按自己的口径回落:面标识不传 ⇒ 对端回已定义面的取值结论;
// 参数模板标识不传 ⇒ 对端回落到接管快照里记录的那张模板(不是默认模板,其 R-20)。
type ConfigQuery struct {
	FileFace         string
	ParamTemplateRef string
	VersionRef       string
}

// ReadConfig 代理读对端交付配置的字段级视图(CLC FUNC-48 的对端通道)。
// 返回值原样透传对端 flat JSON:本域不重算四层取值、不合并、不落副本(R-25 / REF-12 同源)。
func (a *Ignition) ReadConfig(ctx context.Context, clusterRef string, q ConfigQuery) (map[string]any, error) {
	if a.cfg.BaseURL == "" {
		return nil, ErrPeerNotConfigured
	}
	query := url.Values{}
	if v := strings.TrimSpace(q.FileFace); v != "" {
		query.Set("file_face", v)
	}
	if v := strings.TrimSpace(q.ParamTemplateRef); v != "" {
		query.Set("param_template_ref", v)
	}
	if v := strings.TrimSpace(q.VersionRef); v != "" {
		query.Set("version_ref", v)
	}
	var out map[string]any
	if err := a.do(ctx, http.MethodGet, pathWithRef(defaultConfigPath, clusterRef), query.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadConfigRaw 代理读对端交付配置原文(CLC FUNC-49 的对端通道;file_face 必带且一次一面)。
// lines[].text 已由对端在流出前完成掩码,content_sha256 是掩码前原文摘要 ——
// 本域既不二次解析也不落库该摘要(INV-08:再掩码一次即等于承认本域持有原文)。
func (a *Ignition) ReadConfigRaw(ctx context.Context, clusterRef, fileFace string) (map[string]any, error) {
	if a.cfg.BaseURL == "" {
		return nil, ErrPeerNotConfigured
	}
	face := strings.ToUpper(strings.TrimSpace(fileFace))
	if face == "" {
		return nil, fmt.Errorf("原文通道必须携带 file_face 且一次只出一面(HOSTS / CONFIG)")
	}
	var out map[string]any
	q := url.Values{"file_face": []string{face}}
	if err := a.do(ctx, http.MethodGet, pathWithRef(defaultConfigPath, clusterRef)+configRawSuffix,
		q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PeerStatus 取对端返回的 HTTP 状态码与错误说明(401/403/404/409 的中文口径由调用方分派;
// ok=false 表示不是对端状态错误 —— 传输层失败或未配置地址,即「对端不可达」)。
func PeerStatus(err error) (code int, msg string, ok bool) {
	var se *httpStatusError
	if asStatus(err, &se) {
		return se.Code, se.Msg, true
	}
	return 0, "", false
}

func asStatus(err error, out **httpStatusError) bool {
	se, ok := err.(*httpStatusError)
	if ok {
		*out = se
	}
	return ok
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
