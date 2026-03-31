// Package httpserver 初始化 Gin 路由与静态页面托管
package httpserver

import (
	"encoding/json"
	"feishu-agent/internal/feishu"
	"feishu-agent/internal/httpserver/api"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"feishu-agent/internal/workflow"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Server HTTP 服务
type Server struct {
	engine       *gin.Engine
	runner       *workflow.Runner
	feishuClient *feishu.Client
}

// New 创建 HTTP 服务
func New(debug bool) *Server {
	if !debug {
		gin.SetMode(gin.ReleaseMode)
	}
	s := &Server{
		engine: gin.New(),
	}
	s.engine.Use(gin.Recovery(), logMiddleware())
	s.setup()
	return s
}

// InitFeishu 初始化飞书客户端与工作流 Runner
func (s *Server) InitFeishu(appID, appSecret string) {
	if appID == "" || appSecret == "" {
		log.Printf("[server] feishu not configured, webhook handling disabled")
		return
	}
	s.feishuClient = feishu.NewClient(appID, appSecret)
	s.runner = workflow.NewRunner(s.feishuClient)
	api.SetRunner(s.runner)
	api.SetFeishuClient(s.feishuClient)
	log.Printf("[server] feishu client initialized, app_id=%s", appID)
}

// GetRunner 返回工作流 Runner（供 WebSocket 客户端使用）
func (s *Server) GetRunner() *workflow.Runner {
	return s.runner
}

// GetFeishuClient 返回飞书客户端（供 WebSocket 客户端使用）
func (s *Server) GetFeishuClient() *feishu.Client {
	return s.feishuClient
}

// Run 启动服务
func (s *Server) Run(addr string) error {
	log.Printf("[server] starting on %s", addr)
	return s.engine.Run(addr)
}

// setup 注册路由
func (s *Server) setup() {
	r := s.engine

	// ─── 静态页面（前端）────────────────────────────────────────
	r.Static("/static", "./web/static")
	r.StaticFile("/", "./web/index.html")
	r.StaticFile("/settings", "./web/settings.html")
	r.StaticFile("/routes", "./web/routes.html")
	r.StaticFile("/tools", "./web/tools.html")
	r.StaticFile("/harness", "./web/harness.html")
	r.StaticFile("/triggers", "./web/triggers.html")
	r.StaticFile("/messages", "./web/messages.html")
	r.StaticFile("/terminal", "./web/terminal.html")

	// ─── WebSocket 终端 ──────────────────────────────────────────
	r.GET("/ws/terminal", s.handleTerminal)

	// ─── 飞书 Webhook ────────────────────────────────────────────
	r.POST("/webhook/feishu", s.handleFeishuWebhook)

	// ─── REST API ────────────────────────────────────────────────
	apiGroup := r.Group("/api")
	{
		apiGroup.GET("/dashboard", api.GetDashboard)

		// 配置
		apiGroup.GET("/settings", api.GetSettings)
		apiGroup.POST("/settings", api.PostSettings)

		// 项目路由
		apiGroup.GET("/routes", api.ListRoutes)
		apiGroup.POST("/routes", api.CreateRoute)
		apiGroup.PUT("/routes/:id", api.UpdateRoute)
		apiGroup.DELETE("/routes/:id", api.DeleteRoute)

		// 工具配置
		apiGroup.GET("/tools", api.ListTools)
		apiGroup.POST("/tools", api.CreateTool)
		apiGroup.PUT("/tools/:id", api.UpdateTool)
		apiGroup.DELETE("/tools/:id", api.DeleteTool)

		// 提示词模板
		apiGroup.GET("/prompts", api.ListPrompts)
		apiGroup.POST("/prompts", api.CreatePrompt)
		apiGroup.PUT("/prompts/:id", api.UpdatePrompt)
		apiGroup.DELETE("/prompts/:id", api.DeletePrompt)

		// 触发记录
		apiGroup.GET("/triggers", api.ListTriggers)
		apiGroup.GET("/triggers/:id", api.GetTrigger)
		apiGroup.GET("/triggers/:id/logs", api.GetTriggerLogs)
		apiGroup.POST("/triggers/:id/cancel", api.CancelTrigger)
		apiGroup.GET("/jobs/active", api.ListActiveJobs)

		// 对话记录
		apiGroup.GET("/messages", api.ListMessages)

		// 用户名解析
		apiGroup.GET("/users/resolve", api.ResolveUsers)

		// 测试接口
		apiGroup.POST("/test/intent", api.TestIntent)
		apiGroup.POST("/test/feishu", api.TestFeishu)
		apiGroup.POST("/test/llm", api.TestLLM)
		apiGroup.POST("/manual/trigger", api.ManualTrigger)
	}
}

// handleFeishuWebhook 处理飞书事件推送
func (s *Server) handleFeishuWebhook(c *gin.Context) {
	// 读取请求体
	body, err := feishu.ReadBody(c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 读取飞书配置
	settings, _ := store.GetAllSettings()
	encryptKey := settings["feishu_encrypt_key"]
	verificationToken := settings["feishu_verification_token"]

	// 验证签名（如果配置了 encrypt_key）
	if encryptKey != "" {
		ts := c.GetHeader("X-Lark-Request-Timestamp")
		nonce := c.GetHeader("X-Lark-Request-Nonce")
		sig := c.GetHeader("X-Lark-Signature")
		if !feishu.VerifySignature(ts, nonce, encryptKey, string(body), sig) {
			log.Printf("[webhook] signature verification failed")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}

		// 解密消息体
		var enc feishu.EncryptedBody
		if err = json.Unmarshal(body, &enc); err == nil && enc.Encrypt != "" {
			body, err = feishu.Decrypt(enc.Encrypt, encryptKey)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "decrypt failed: " + err.Error()})
				return
			}
		}
	}

	// 解析事件
	var raw feishu.RawEvent
	if err = json.Unmarshal(body, &raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse event: " + err.Error()})
		return
	}

	// 处理飞书 URL Challenge（首次配置 webhook 时）
	if raw.Type == "url_verification" || raw.Challenge != "" {
		// 验证 token
		if verificationToken != "" && raw.Token != verificationToken {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"challenge": raw.Challenge})
		return
	}

	// 只处理消息事件
	if raw.Header == nil || raw.Header.EventType != "im.message.receive_v1" {
		c.JSON(http.StatusOK, gin.H{"msg": "ignored"})
		return
	}

	// 去重
	if raw.Header.EventID != "" {
		processed, _ := store.IsEventProcessed(raw.Header.EventID)
		if processed {
			c.JSON(http.StatusOK, gin.H{"msg": "duplicate"})
			return
		}
		store.MarkEventProcessed(raw.Header.EventID) //nolint
	}

	// 解析消息体
	var msgEvent feishu.MessageEvent
	if err = json.Unmarshal(raw.Event, &msgEvent); err != nil {
		log.Printf("[webhook] parse message event: %v", err)
		c.JSON(http.StatusOK, gin.H{"msg": "parse error"})
		return
	}

	if msgEvent.Message == nil || msgEvent.Sender == nil {
		c.JSON(http.StatusOK, gin.H{"msg": "empty message"})
		return
	}

	// 只处理文本类型消息（其他类型暂忽略）
	if msgEvent.Message.MessageType != "text" && msgEvent.Message.MessageType != "post" {
		c.JSON(http.StatusOK, gin.H{"msg": "non-text message ignored"})
		return
	}

	// 群聊消息过滤：默认只接受私聊，feishu_allow_group=true 时才处理群聊
	if msgEvent.Message.ChatType == "group" {
		if settings["feishu_allow_group"] != "true" {
			c.JSON(http.StatusOK, gin.H{"msg": "group message ignored"})
			return
		}
		// 群聊模式下仍需 @机器人 才触发
		botOpenID := settings["feishu_bot_open_id"]
		if !isMentioned(msgEvent.Message.Mentions, botOpenID) {
			c.JSON(http.StatusOK, gin.H{"msg": "not mentioned"})
			return
		}
	}

	// 发送者白名单过滤（私聊 + 群聊通用）
	if allowed := settings["feishu_allowed_senders"]; allowed != "" {
		senderID := ""
		if msgEvent.Sender.SenderID != nil {
			senderID = msgEvent.Sender.SenderID.OpenID
		}
		if !isAllowedSender(senderID, allowed) {
			c.JSON(http.StatusOK, gin.H{"msg": "sender not in whitelist"})
			return
		}
	}

	// 构建消息对象
	textContent := feishu.ParseTextContent(msgEvent.Message.Content)
	// 去掉 @机器人 前缀
	textContent = cleanMentionText(textContent)

	if strings.TrimSpace(textContent) == "" {
		c.JSON(http.StatusOK, gin.H{"msg": "empty content"})
		return
	}

	// 添加「敲键盘」表情回复，让对方知道已收到
	if s.feishuClient != nil && msgEvent.Message.MessageID != "" {
		go s.feishuClient.AddReaction(c.Request.Context(), msgEvent.Message.MessageID, "TYPING") //nolint
	}

	senderName := ""
	if msgEvent.Sender.SenderID != nil {
		senderName = msgEvent.Sender.SenderID.OpenID
		// 可异步获取用户名（避免阻塞 webhook 响应）
	}

	msg := &model.FeishuMessage{
		MessageID:  msgEvent.Message.MessageID,
		SenderID:   func() string { if msgEvent.Sender.SenderID != nil { return msgEvent.Sender.SenderID.OpenID }; return "" }(),
		SenderName: senderName,
		ChatID:     msgEvent.Message.ChatID,
		ChatType:   msgEvent.Message.ChatType,
		Content:    textContent,
		Timestamp:  time.Now(),
	}

	// 记录收到的消息
	_ = store.SaveChatMessage(&model.ChatMessage{
		Direction: "incoming",
		ChatID:    msg.ChatID,
		ChatType:  msg.ChatType,
		SenderID:  msg.SenderID,
		MessageID: msg.MessageID,
		MsgType:   msgEvent.Message.MessageType,
		Content:   textContent,
	})

	// 交给 Runner 处理（异步）
	if s.runner != nil {
		s.runner.HandleMessage(msg)
	} else {
		log.Printf("[webhook] runner not initialized, message dropped: %s", msg.Content)
	}

	c.JSON(http.StatusOK, gin.H{"msg": "ok"})
}

// isMentioned 检查 @列表中是否包含机器人
func isMentioned(mentions []*feishu.Mention, botOpenID string) bool {
	if botOpenID == "" {
		// 未配置机器人 ID，默认处理所有消息
		return true
	}
	for _, m := range mentions {
		if m.ID != nil && m.ID.OpenID == botOpenID {
			return true
		}
	}
	return false
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

// cleanMentionText 清除文本中的 @提及格式
func cleanMentionText(text string) string {
	// 飞书消息文本中 @ 格式为 @xxx，去掉后返回剩余内容
	// 简单处理：去掉 @开头的词
	parts := strings.Fields(text)
	var filtered []string
	for _, p := range parts {
		if !strings.HasPrefix(p, "@") {
			filtered = append(filtered, p)
		}
	}
	return strings.Join(filtered, " ")
}

// logMiddleware 请求日志中间件
func logMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Printf("[http] %s %s %d %v",
			c.Request.Method, c.Request.URL.Path,
			c.Writer.Status(), time.Since(start))
	}
}

// addr 生成监听地址
func Addr(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
