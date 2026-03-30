package api

import (
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ListTriggers GET /api/triggers?page=1&size=20
func ListTriggers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	triggers, total, err := store.ListTriggers(page, size)
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
			Items: triggers,
		},
	})
}

// GetTrigger GET /api/triggers/:id
func GetTrigger(c *gin.Context) {
	id := c.Param("id")
	t, err := store.GetTrigger(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	if t == nil {
		c.JSON(http.StatusNotFound, model.APIResponse{Code: 404, Message: "not found"})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "ok", Data: t})
}
