// Package feishu WebSocket 长连接客户端，替代 Webhook 接收飞书事件
package feishu

import (
	"context"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"log"
	"strings"
	"time"

	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// MessageHandler 消息处理回调
type MessageHandler interface {
	HandleMessage(msg *model.FeishuMessage)
}

// WSClient 飞书 WebSocket 长连接客户端
type WSClient struct {
	appID        string
	appSecret    string
	handler      MessageHandler
	feishuClient *Client
}

// NewWSClient 创建 WebSocket 客户端
func NewWSClient(appID, appSecret string, handler MessageHandler, feishuClient *Client) *WSClient {
	return &WSClient{
		appID:        appID,
		appSecret:    appSecret,
		handler:      handler,
		feishuClient: feishuClient,
	}
}

// Start 启动长连接（阻塞）
func (w *WSClient) Start(ctx context.Context) error {
	// WebSocket 模式不需要 verification token 和 encrypt key
	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			w.onMessage(event)
			return nil
		}).
		OnP2MessageReadV1(func(ctx context.Context, event *larkim.P2MessageReadV1) error {
			return nil // 消息已读事件，忽略
		})

	cli := larkws.NewClient(w.appID, w.appSecret,
		larkws.WithEventHandler(eventHandler),
		larkws.WithAutoReconnect(true),
	)

	log.Printf("[ws] connecting to feishu (app_id=%s)...", w.appID)
	return cli.Start(ctx)
}

// onMessage 处理接收到的消息事件
func (w *WSClient) onMessage(event *larkim.P2MessageReceiveV1) {
	if event.Event == nil || event.Event.Message == nil || event.Event.Sender == nil {
		return
	}

	msg := event.Event.Message
	sender := event.Event.Sender

	// 只处理文本和 post 类型
	msgType := deref(msg.MessageType)
	if msgType != "text" && msgType != "post" {
		return
	}

	settings, _ := store.GetAllSettings()
	chatType := deref(msg.ChatType)

	// 群聊消息过滤：默认只接受私聊，feishu_allow_group=true 时才处理群聊
	if chatType == "group" {
		if settings["feishu_allow_group"] != "true" {
			return
		}
		// 群聊模式下仍需 @机器人 才触发
		botOpenID := settings["feishu_bot_open_id"]
		if !isMentionedSDK(msg.Mentions, botOpenID) {
			return
		}
	}

	// 发送者白名单过滤（私聊 + 群聊通用）
	senderOpenID := ""
	if sender.SenderId != nil {
		senderOpenID = deref(sender.SenderId.OpenId)
	}
	if allowed := settings["feishu_allowed_senders"]; allowed != "" {
		if !isAllowedSender(senderOpenID, allowed) {
			return
		}
	}

	// 添加「敲键盘」表情回复，让对方知道已收到
	if w.feishuClient != nil {
		if msgID := deref(msg.MessageId); msgID != "" {
			go func() {
				if err := w.feishuClient.AddReaction(context.Background(), msgID, "TYPING"); err != nil {
					log.Printf("[ws] add reaction: %v", err)
				}
			}()
		}
	}

	// 解析文本内容
	content := deref(msg.Content)
	textContent := parseTextContent(content)
	textContent = cleanMentionTextWS(textContent)
	if strings.TrimSpace(textContent) == "" {
		return
	}

	feishuMsg := &model.FeishuMessage{
		MessageID:  deref(msg.MessageId),
		SenderID:   senderOpenID,
		SenderName: senderOpenID,
		ChatID:     deref(msg.ChatId),
		ChatType:   chatType,
		Content:    textContent,
		Timestamp:  time.Now(),
	}

	// 记录收到的消息
	_ = store.SaveChatMessage(&model.ChatMessage{
		Direction: "incoming",
		ChatID:    feishuMsg.ChatID,
		ChatType:  chatType,
		SenderID:  senderOpenID,
		MessageID: feishuMsg.MessageID,
		MsgType:   msgType,
		Content:   textContent,
	})

	log.Printf("[ws] received message from %s in %s: %s", senderOpenID, deref(msg.ChatId), truncate(textContent, 80))

	if w.handler != nil {
		w.handler.HandleMessage(feishuMsg)
	}
}

// isAllowedSender 检查发送者是否在白名单中（逗号分隔的 Open ID 列表）
func isAllowedSender(senderID, allowedList string) bool {
	if senderID == "" {
		return false
	}
	for _, id := range strings.Split(allowedList, ",") {
		if strings.TrimSpace(id) == senderID {
			return true
		}
	}
	return false
}

// isMentionedSDK 检查 SDK 的 MentionEvent 列表中是否包含机器人
func isMentionedSDK(mentions []*larkim.MentionEvent, botOpenID string) bool {
	if botOpenID == "" {
		return true
	}
	for _, m := range mentions {
		if m.Id != nil && deref(m.Id.OpenId) == botOpenID {
			return true
		}
	}
	return false
}

// parseTextContent 从 content JSON 中提取文本（复用 ParseTextContent）
func parseTextContent(content string) string {
	return ParseTextContent(content)
}

// cleanMentionTextWS 清除文本中的 @提及格式
func cleanMentionTextWS(text string) string {
	parts := strings.Fields(text)
	var filtered []string
	for _, p := range parts {
		if !strings.HasPrefix(p, "@") {
			filtered = append(filtered, p)
		}
	}
	return strings.Join(filtered, " ")
}

// deref 安全解引用 *string
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// truncate 截断字符串
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
