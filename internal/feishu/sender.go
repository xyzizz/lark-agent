package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ─── 消息发送 ─────────────────────────────────────────────────

// SendTextMessage 向指定会话发送文本消息
func (c *Client) SendTextMessage(ctx context.Context, chatID, text string) error {
	token, err := c.getToken(ctx)
	if err != nil {
		return err
	}

	content, _ := json.Marshal(map[string]string{"text": text})
	payload := map[string]any{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    string(content),
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/im/v1/messages?receive_id_type=chat_id", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.Code != 0 {
		return fmt.Errorf("feishu send error %d: %s", result.Code, result.Msg)
	}
	return nil
}

// SendCardMessage 发送卡片消息（富文本，用于展示排查结论）
func (c *Client) SendCardMessage(ctx context.Context, chatID string, card *CardMessage) error {
	token, err := c.getToken(ctx)
	if err != nil {
		return err
	}

	cardJSON, err := json.Marshal(card)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"receive_id": chatID,
		"msg_type":   "interactive",
		"content":    string(cardJSON),
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/im/v1/messages?receive_id_type=chat_id", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return fmt.Errorf("send card: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// SendWebhook 通过机器人 Webhook 发送消息（简单方案，不需要 token）
func SendWebhook(ctx context.Context, webhookURL, text string) error {
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": text},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	cli := &http.Client{}
	resp, err := cli.Do(req)
	if err != nil {
		return fmt.Errorf("webhook send: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// ─── 卡片消息结构 ─────────────────────────────────────────────

// CardMessage 飞书卡片消息（简化版）
type CardMessage struct {
	Schema  string     `json:"schema"`
	Config  CardConfig `json:"config"`
	Header  CardHeader `json:"header"`
	Elements []any     `json:"elements"`
}

type CardConfig struct {
	WideScreenMode bool `json:"wide_screen_mode"`
}

type CardHeader struct {
	Title    CardText `json:"title"`
	Template string   `json:"template"` // blue | green | red | orange
}

type CardText struct {
	Content string `json:"content"`
	Tag     string `json:"tag"` // plain_text | lark_md
}

// BuildResultCard 构建工作流结果卡片
func BuildResultCard(title, intent, summary, mrLink string, sqlSuggestions []string, success bool) *CardMessage {
	template := "green"
	if !success {
		template = "red"
	}

	elements := []any{
		map[string]any{
			"tag":     "div",
			"text": map[string]any{
				"content": "**意图识别：**" + intent,
				"tag":     "lark_md",
			},
		},
		map[string]any{
			"tag":     "div",
			"text": map[string]any{
				"content": "**执行摘要：**\n" + summary,
				"tag":     "lark_md",
			},
		},
	}

	if mrLink != "" {
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"content": "**MR 链接：**[点击查看](" + mrLink + ")",
				"tag":     "lark_md",
			},
		})
	}

	if len(sqlSuggestions) > 0 {
		sqlText := "**SQL 建议：**\n"
		for _, s := range sqlSuggestions {
			sqlText += "```sql\n" + s + "\n```\n"
		}
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"content": sqlText,
				"tag":     "lark_md",
			},
		})
	}

	return &CardMessage{
		Schema: "2.0",
		Config: CardConfig{WideScreenMode: true},
		Header: CardHeader{
			Title:    CardText{Content: title, Tag: "plain_text"},
			Template: template,
		},
		Elements: elements,
	}
}
