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

// buildPrompt 渲染意图识别提示词（从文件加载模板）
func (r *Recognizer) buildPrompt(message string) (string, error) {
	projectList := buildProjectList()

	content, err := store.LoadPrompt("intent")
	if err != nil {
		return "", fmt.Errorf("load intent prompt: %w", err)
	}
	return renderTemplate(content, map[string]string{
		"Message":     message,
		"ProjectList": projectList,
	})
}

// buildProjectList 从 DB 读取所有已启用路由，构建项目列表文本
func buildProjectList() string {
	routes, err := store.ListRoutes(true)
	if err != nil || len(routes) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, r := range routes {
		keywords := ""
		if len(r.Keywords) > 0 {
			keywords = " (关键词: " + strings.Join(r.Keywords, ", ") + ")"
		}
		sb.WriteString(fmt.Sprintf("- %s%s\n", r.Name, keywords))
	}
	return sb.String()
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

	// 从文件读取系统提示词
	sysPromptContent, err := store.LoadPrompt("system")
	if err != nil {
		return "", fmt.Errorf("load system prompt: %w", err)
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
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
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
