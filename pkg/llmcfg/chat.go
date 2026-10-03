package llmcfg

// LLM Chat Completions 调用（OpenAI 兼容 /chat/completions；deepseek/qwen/vllm 皆此协议）。
// 无外部 SDK 依赖：直接 HTTP。失败返回 error，调用方降级回规则匹配引擎。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Message 对话消息。
type Message struct {
	Role    string `json:"role"` // system|user|assistant
	Content string `json:"content"`
}

// Chat 调用 LLM；30s 超时。llm 配置未启用时返回 ( "", llmDisabledErr )。
func Chat(ctx context.Context, cfg *Config, messages []Message) (string, error) {
	if cfg == nil || !cfg.Enabled || cfg.APIKey == "" || cfg.BaseURL == "" || cfg.Model == "" {
		return "", ErrDisabled
	}
	body := map[string]any{
		"model":    cfg.Model,
		"messages": messages,
	}
	// temperature 仅在 >0 时发送（部分模型如 kimi-for-coding 只允许默认温度，0.3 会被拒）
	if cfg.Temperature > 0 {
		body["temperature"] = cfg.Temperature
	}
	b, _ := json.Marshal(body)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost,
		strings.TrimRight(cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", fmt.Errorf("LLM API 错误: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("LLM API 返回空 choices")
	}
	return out.Choices[0].Message.Content, nil
}

// ErrDisabled llm 未启用（调用方应降级回规则匹配引擎）。
var ErrDisabled = fmt.Errorf("LLM 未启用")
