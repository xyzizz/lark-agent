package api

import (
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ListPrompts 列出所有提示词文件
func ListPrompts(c *gin.Context) {
	prompts, err := store.ListPrompts()
	if err != nil {
		c.JSON(http.StatusOK, model.APIResponse{Code: 1, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Data: prompts})
}

// UpdatePrompt 更新提示词文件内容
func UpdatePrompt(c *gin.Context) {
	name := c.Param("name")
	var body struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, model.APIResponse{Code: 1, Message: "参数错误"})
		return
	}
	if err := store.SavePrompt(name, body.Content); err != nil {
		c.JSON(http.StatusOK, model.APIResponse{Code: 1, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "保存成功"})
}
