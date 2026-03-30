package api

import (
	"feishu-agent/internal/intent"
	"feishu-agent/internal/model"
	"net/http"

	"github.com/gin-gonic/gin"
)

// TestIntent POST /api/test/intent
// 用于在前端直接测试意图识别效果
func TestIntent(c *gin.Context) {
	var req struct {
		Message string `json:"message"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Message == "" {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: "message is required"})
		return
	}

	recognizer := intent.NewRecognizer()
	result, err := recognizer.Recognize(c.Request.Context(), req.Message)
	if err != nil {
		c.JSON(http.StatusOK, model.APIResponse{Code: 1, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "ok", Data: result})
}

// ManualTrigger POST /api/manual/trigger
// 手动触发工作流（不通过飞书消息，直接测试）
func ManualTrigger(c *gin.Context) {
	var req struct {
		Message    string `json:"message"`
		SenderName string `json:"sender_name"`
		ChatID     string `json:"chat_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Message == "" {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: "message is required"})
		return
	}

	// 获取全局 Runner（在 server.go 中注入）
	runner := GetRunner()
	if runner == nil {
		c.JSON(http.StatusServiceUnavailable, model.APIResponse{Code: 503, Message: "runner not initialized"})
		return
	}

	msg := &model.FeishuMessage{
		MessageID:  "manual-" + c.GetString("request_id"),
		SenderID:   "manual",
		SenderName: req.SenderName,
		ChatID:     req.ChatID,
		ChatType:   "p2p",
		Content:    req.Message,
	}
	runner.HandleMessage(msg)
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "trigger submitted, processing in background"})
}

// runner 全局引用（由 server 注入）
var globalRunner RunnerInterface

type RunnerInterface interface {
	HandleMessage(msg *model.FeishuMessage)
}

func SetRunner(r RunnerInterface) { globalRunner = r }
func GetRunner() RunnerInterface  { return globalRunner }
