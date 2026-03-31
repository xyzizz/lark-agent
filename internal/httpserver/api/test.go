package api

import (
	"feishu-agent/internal/intent"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
// 手动触发工作流，sender_id 必须是白名单中的用户
func ManualTrigger(c *gin.Context) {
	var req struct {
		Message  string `json:"message"`
		SenderID string `json:"sender_id"` // 白名单用户的 open_id
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Message == "" {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: "message is required"})
		return
	}

	runner := GetRunner()
	if runner == nil {
		c.JSON(http.StatusServiceUnavailable, model.APIResponse{Code: 503, Message: "runner not initialized"})
		return
	}

	// 必须选择一个白名单用户
	settings, _ := store.GetAllSettings()
	allowed := settings["feishu_allowed_senders"]
	if req.SenderID == "" {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: "请选择发送用户"})
		return
	}
	if allowed != "" {
		found := false
		for _, id := range strings.Split(allowed, ",") {
			if strings.TrimSpace(id) == req.SenderID {
				found = true
				break
			}
		}
		if !found {
			c.JSON(http.StatusForbidden, model.APIResponse{Code: 403, Message: "该用户不在白名单中"})
			return
		}
	}

	msg := &model.FeishuMessage{
		MessageID:  "manual-" + uuid.NewString()[:8],
		SenderID:   req.SenderID,
		SenderName: req.SenderID,
		ChatID:     req.SenderID, // 私聊的 chatID 由飞书自动处理，这里用 senderID 占位
		ChatType:   "p2p",
		Content:    req.Message,
	}
	runner.HandleMessage(msg)
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "已提交，正在后台处理"})
}

// runner 全局引用（由 server 注入）
var globalRunner RunnerInterface

type RunnerInterface interface {
	HandleMessage(msg *model.FeishuMessage)
}

func SetRunner(r RunnerInterface) { globalRunner = r }
func GetRunner() RunnerInterface  { return globalRunner }
