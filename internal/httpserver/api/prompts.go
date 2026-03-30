package api

import (
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"net/http"

	"github.com/gin-gonic/gin"
)

func ListPrompts(c *gin.Context) {
	prompts, err := store.ListPrompts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "ok", Data: prompts})
}

func CreatePrompt(c *gin.Context) {
	var p model.PromptTemplate
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: err.Error()})
		return
	}
	if p.Name == "" || p.Content == "" {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: "name and content are required"})
		return
	}
	if err := store.CreatePrompt(&p); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "created", Data: p})
}

func UpdatePrompt(c *gin.Context) {
	id := c.Param("id")
	var p model.PromptTemplate
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: err.Error()})
		return
	}
	p.ID = id
	if err := store.UpdatePrompt(&p); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "updated"})
}

func DeletePrompt(c *gin.Context) {
	id := c.Param("id")
	if err := store.DeletePrompt(id); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "deleted"})
}
