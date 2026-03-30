// Package feishu 飞书 API 客户端（获取 tenant_access_token、读取消息上下文等）
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	baseURL = "https://open.feishu.cn/open-apis"
)

// Client 飞书 API 客户端
type Client struct {
	appID     string
	appSecret string
	httpCli   *http.Client

	tokenMu  sync.Mutex
	token    string
	tokenExp time.Time
}

// NewClient 创建飞书客户端
func NewClient(appID, appSecret string) *Client {
	return &Client{
		appID:     appID,
		appSecret: appSecret,
		httpCli:   &http.Client{Timeout: 15 * time.Second},
	}
}

// ─── Token 管理 ───────────────────────────────────────────────

type tokenResp struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"` // 秒
}

// getToken 获取 tenant_access_token（自动续期）
func (c *Client) getToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// 提前 60 秒刷新
	if c.token != "" && time.Now().Add(60*time.Second).Before(c.tokenExp) {
		return c.token, nil
	}

	payload := map[string]string{
		"app_id":     c.appID,
		"app_secret": c.appSecret,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return "", fmt.Errorf("get token: %w", err)
	}
	defer resp.Body.Close()

	var tr tokenResp
	if err = json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("decode token resp: %w", err)
	}
	if tr.Code != 0 {
		return "", fmt.Errorf("feishu token error %d: %s", tr.Code, tr.Msg)
	}

	c.token = tr.TenantAccessToken
	c.tokenExp = time.Now().Add(time.Duration(tr.Expire) * time.Second)
	return c.token, nil
}

// ─── 消息上下文 ───────────────────────────────────────────────

// ChatMessage 聊天消息
type ChatMessage struct {
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
	Sender    string `json:"sender"`
}

// GetMessageContext 获取消息线程上下文（拉取当前消息的父消息，构成对话历史）
func (c *Client) GetMessageContext(ctx context.Context, messageID string) ([]ChatMessage, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/im/v1/messages/%s", baseURL, messageID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	// 简单返回原始 JSON，上层根据需要解析
	return []ChatMessage{{MessageID: messageID, Content: string(data)}}, nil
}

// GetUserInfo 获取用户信息（姓名等）
func (c *Client) GetUserInfo(ctx context.Context, openID string) (string, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return openID, err
	}

	url := fmt.Sprintf("%s/contact/v3/users/%s?user_id_type=open_id", baseURL, openID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return openID, nil
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			User struct {
				Name string `json:"name"`
			} `json:"user"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return openID, nil
	}
	if result.Data.User.Name != "" {
		return result.Data.User.Name, nil
	}
	return openID, nil
}

// ─── 文档读取 ─────────────────────────────────────────────────

// GetDocContent 读取飞书文档内容（简单版：获取纯文本）
// docToken 是飞书文档 URL 中的 token 部分
func (c *Client) GetDocContent(ctx context.Context, docToken string) (string, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/docx/v1/documents/%s/raw_content", baseURL, docToken)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return "", fmt.Errorf("get doc: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", fmt.Errorf("feishu doc error %d: %s", result.Code, result.Msg)
	}
	return result.Data.Content, nil
}

// TestConnection 测试飞书连接
func (c *Client) TestConnection(ctx context.Context) error {
	_, err := c.getToken(ctx)
	return err
}
