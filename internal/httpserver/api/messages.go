package api

import (
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ListMessages GET /api/messages?page=1&size=50
func ListMessages(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "50"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}

	msgs, total, err := store.ListChatMessages(page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{
		Code:    0,
		Message: "ok",
		Data: model.PageResult{
			Total: total,
			Page:  page,
			Size:  size,
			Items: msgs,
		},
	})
}
