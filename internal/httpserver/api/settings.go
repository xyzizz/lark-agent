// Package api HTTP API 处理器
package api

import (
	"bytes"
	"encoding/json"
	"feishu-agent/internal/config"
	"feishu-agent/internal/feishu"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GetSettings GET /api/settings
func GetSettings(c *gin.Context) {
	settings, err := store.GetAllSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "ok", Data: settings})
}

// PostSettings POST /api/settings（批量保存）
// 接受 map[string]any，value 可以是 string / number / bool，统一转为 string 存储
func PostSettings(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: err.Error()})
		return
	}
	kvs := make(map[string]string, len(raw))
	for k, v := range raw {
		switch val := v.(type) {
		case string:
			kvs[k] = val
		case bool:
			if val {
				kvs[k] = "true"
			} else {
				kvs[k] = "false"
			}
		default:
			kvs[k] = fmt.Sprintf("%v", val)
		}
	}
	if err := store.SetSettings(kvs); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	// 立即同步进全局 Config，无需重启
	applySettingsToConfig(kvs)
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "saved"})
}

// applySettingsToConfig 将 KV 同步进内存 Config（保存时 + 启动时都调用）
func applySettingsToConfig(kvs map[string]string) {
	config.Update(func(c *config.Config) {
		if v := kvs["feishu_app_id"]; v != "" {
			c.Feishu.AppID = v
		}
		if v := kvs["feishu_app_secret"]; v != "" {
			c.Feishu.AppSecret = v
		}
		if v := kvs["feishu_verification_token"]; v != "" {
			c.Feishu.VerificationToken = v
		}
		if v := kvs["feishu_encrypt_key"]; v != "" {
			c.Feishu.EncryptKey = v
		}
		if v := kvs["feishu_bot_webhook"]; v != "" {
			c.Feishu.BotWebhook = v
		}
		if v := kvs["feishu_event_mode"]; v != "" {
			c.Feishu.EventMode = v
		}
		if v := kvs["llm_api_key"]; v != "" {
			c.LLM.APIKey = v
		}
		if v := kvs["llm_base_url"]; v != "" {
			c.LLM.BaseURL = v
		}
		if v := kvs["llm_model"]; v != "" {
			c.LLM.Model = v
		}
		if v := kvs["llm_max_tokens"]; v != "" {
			var n int
			fmt.Sscanf(v, "%d", &n)
			if n > 0 {
				c.LLM.MaxTokens = n
			}
		}
	})
}

// TestFeishu POST /api/test/feishu — 用请求体中的参数直接测试飞书连通性
// 请求体字段优先，缺省时回退到已保存的配置
func TestFeishu(c *gin.Context) {
	var req struct {
		AppID     string `json:"app_id"`
		AppSecret string `json:"app_secret"`
	}
	_ = c.ShouldBindJSON(&req)

	cfg := config.Get()
	appID := req.AppID
	if appID == "" {
		appID = cfg.Feishu.AppID
	}
	appSecret := req.AppSecret
	if appSecret == "" {
		appSecret = cfg.Feishu.AppSecret
	}
	if appID == "" || appSecret == "" {
		c.JSON(http.StatusOK, model.APIResponse{Code: 1, Message: "飞书 App ID 或 App Secret 未配置"})
		return
	}
	cli := feishu.NewClient(appID, appSecret)
	if err := cli.TestConnection(c.Request.Context()); err != nil {
		c.JSON(http.StatusOK, model.APIResponse{Code: 1, Message: "连接失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "连接成功"})
}

// TestLLM POST /api/test/llm — 用请求体中的参数直接测试 LLM 连通性
// 请求体字段优先，缺省时回退到已保存的配置
func TestLLM(c *gin.Context) {
	var req struct {
		APIKey    string `json:"api_key"`
		BaseURL   string `json:"base_url"`
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
	}
	// 忽略解析错误，用零值兜底
	_ = c.ShouldBindJSON(&req)

	// 回退：请求体为空时使用已保存配置
	cfg := config.Get()
	apiKey := firstNonEmptyStr(req.APIKey, cfg.LLM.APIKey)
	baseURL := firstNonEmptyStr(req.BaseURL, cfg.LLM.BaseURL, "https://api.anthropic.com")
	mdl := firstNonEmptyStr(req.Model, cfg.LLM.Model, "claude-opus-4-6")

	if apiKey == "" {
		c.JSON(http.StatusOK, model.APIResponse{Code: 1, Message: "API Key 不能为空"})
		return
	}

	payload := map[string]any{
		"model":      mdl,
		"max_tokens": 16,
		"messages": []map[string]string{
			{"role": "user", "content": "reply with the single word: ok"},
		},
	}
	bodyBytes, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost,
		baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		c.JSON(http.StatusOK, model.APIResponse{Code: 1, Message: "构建请求失败: " + err.Error()})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	cli := &http.Client{Timeout: 15 * time.Second}
	resp, err := cli.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusOK, model.APIResponse{Code: 1, Message: "请求失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		c.JSON(http.StatusOK, model.APIResponse{
			Code:    0,
			Message: fmt.Sprintf("LLM 连接成功（model: %s, base_url: %s）", mdl, baseURL),
		})
	} else {
		msg := string(respBody)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		c.JSON(http.StatusOK, model.APIResponse{
			Code:    1,
			Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, msg),
		})
	}
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// GetDashboard GET /api/dashboard
func GetDashboard(c *gin.Context) {
	stats, err := store.GetDashboardStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "ok", Data: stats})
}
