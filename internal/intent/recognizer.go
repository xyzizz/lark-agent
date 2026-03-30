// Package intent 使用 LLM 识别消息意图
package intent

import (
	"bytes"
	"context"
	"encoding/json"
	"feishu-agent/internal/config"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"
)

// Recognizer 意图识别器
type Recognizer struct {
	httpCli *http.Client
}

func NewRecognizer() *Recognizer {
	cfg := config.Get()
	return &Recognizer{
		httpCli: &http.Client{
			Timeout: time.Duration(cfg.LLM.TimeoutSeconds) * time.Second,
		},
	}
}

// Recognize 识别消息意图，返回结构化结果
func (r *Recognizer) Recognize(ctx context.Context, message string) (*model.IntentResult, error) {
	prompt, err := r.buildPrompt(message)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}

	raw, err := r.callLLM(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("call llm: %w", err)
	}

	result, err := ParseIntentJSON(raw)
	if err != nil {
		// 兜底：解析失败时返回 need_more_context
		return &model.IntentResult{
			Intent:     model.IntentNeedMoreContext,
			Confidence: 0.1,
			Summary:    "意图解析失败，原始响应：" + truncate(raw, 200),
		}, nil
	}
	return result, nil
}

// buildPrompt 渲染意图识别提示词
func (r *Recognizer) buildPrompt(message string) (string, error) {
	// 优先从数据库读取提示词模板
	tpl, err := store.GetPromptByType("intent")
	if err != nil || tpl == nil {
		// 使用内置默认模板
		return buildDefaultIntentPrompt(message)
	}
	return renderTemplate(tpl.Content, map[string]string{
		"Message": message,
	})
}

// loadLLMConfig 从 DB 读取 LLM 配置，覆盖 YAML 中的空/零值
// 优先级：DB 设置 > config.yaml
func loadLLMConfig() config.LLMConfig {
	cfg := config.Get()
	llm := cfg.LLM // 先拷贝 YAML 的值

	settings, _ := store.GetAllSettings()

	if v := settings["llm_api_key"]; v != "" {
		llm.APIKey = v
	}
	if v := settings["llm_base_url"]; v != "" {
		llm.BaseURL = v
	}
	if v := settings["llm_model"]; v != "" {
		llm.Model = v
	}
	if v := settings["llm_max_tokens"]; v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		if n > 0 {
			llm.MaxTokens = n
		}
	}
	// 兜底默认值
	if llm.BaseURL == "" {
		llm.BaseURL = "https://api.anthropic.com"
	}
	if llm.Model == "" {
		llm.Model = "claude-opus-4-6"
	}
	if llm.MaxTokens == 0 {
		llm.MaxTokens = 4096
	}
	return llm
}

// callLLM 调用 Claude API
func (r *Recognizer) callLLM(ctx context.Context, userPrompt string) (string, error) {
	llm := loadLLMConfig()
	if llm.APIKey == "" {
		return "", fmt.Errorf("LLM API key not configured（请在管理后台→飞书配置页填写）")
	}

	// 从 DB 读取系统提示词
	sysPromptContent := "你是意图识别助手，只输出 JSON，不得有额外文字。"
	if sysTpl, err := store.GetPromptByType("system"); err == nil && sysTpl != nil {
		sysPromptContent = sysTpl.Content
	}

	payload := map[string]any{
		"model":      llm.Model,
		"max_tokens": llm.MaxTokens,
		"system":     sysPromptContent,
		"messages": []map[string]string{
			{"role": "user", "content": userPrompt},
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		llm.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", llm.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := r.httpCli.Do(req)
	if err != nil {
		return "", fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm status %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}

	// 解析 Claude API 响应
	var claudeResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err = json.Unmarshal(respBody, &claudeResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if claudeResp.Error != nil {
		return "", fmt.Errorf("claude error: %s", claudeResp.Error.Message)
	}
	for _, c := range claudeResp.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf("no text content in response")
}

// ─── 工具函数 ─────────────────────────────────────────────────

func buildDefaultIntentPrompt(message string) (string, error) {
	const tmpl = `请分析以下用户消息，判断意图类型。

用户消息：
{{.Message}}

请严格按照如下 JSON 格式输出，不得有任何额外内容：
{
  "intent": "<issue_troubleshooting|requirement_writing|ignore|need_more_context|risky_action>",
  "confidence": <0.0到1.0之间的数字>,
  "matched_keywords": ["<keyword1>"],
  "suspected_project": "<猜测的项目名，无则空字符串>",
  "need_repo_access": <true|false>,
  "need_doc_access": <true|false>,
  "need_db_query": <true|false>,
  "risk_level": "<low|medium|high|critical>",
  "summary": "<一句话摘要，不超过50字>"
}

intent 含义：
- issue_troubleshooting: 线上问题、bug、报错、数据异常
- requirement_writing: 新功能、需求、优化、改动
- ignore: 闲聊、打招呼、无关内容
- need_more_context: 信息不足无法判断
- risky_action: 危险操作，如删库、强制部署`

	return renderTemplate(tmpl, map[string]string{"Message": message})
}

func renderTemplate(tmplStr string, data map[string]string) (string, error) {
	t, err := template.New("").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err = t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// CallLLMRaw 供工作流模块直接调用 LLM 的通用方法
func CallLLMRaw(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	r := NewRecognizer()
	llm := loadLLMConfig()
	if llm.APIKey == "" {
		return "", fmt.Errorf("LLM API key not configured")
	}

	payload := map[string]any{
		"model":      llm.Model,
		"max_tokens": llm.MaxTokens,
		"system":     systemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userPrompt},
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		llm.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", llm.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := r.httpCli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm status %d: %s", resp.StatusCode, string(respBody))
	}

	var claudeResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err = json.Unmarshal(respBody, &claudeResp); err != nil {
		return "", err
	}
	for _, c := range claudeResp.Content {
		if c.Type == "text" {
			return strings.TrimSpace(c.Text), nil
		}
	}
	return "", fmt.Errorf("no text in llm response")
}
