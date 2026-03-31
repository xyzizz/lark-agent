package api

import (
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func ListTools(c *gin.Context) {
	tools, err := store.ListTools(false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "ok", Data: tools})
}

func CreateTool(c *gin.Context) {
	var t model.ToolConfig
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: err.Error()})
		return
	}
	if t.Name == "" {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: "name is required"})
		return
	}
	if t.ArgsTemplate == "" {
		t.ArgsTemplate = "{}"
	}
	if err := store.CreateTool(&t); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	if err := store.SaveToolFile(&t); err != nil {
		log.Printf("[api] save tool file: %v", err)
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "created", Data: t})
}

func UpdateTool(c *gin.Context) {
	id := c.Param("id")
	// 读取旧记录用于文件重命名
	old, _ := store.GetTool(id)
	var t model.ToolConfig
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: err.Error()})
		return
	}
	t.ID = id
	if err := store.UpdateTool(&t); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	// 同步文件：先删旧文件再写新文件
	if old != nil && (old.Name != t.Name || old.ToolType != t.ToolType) {
		_ = store.DeleteToolFile(old.ToolType, old.Name)
	}
	if err := store.SaveToolFile(&t); err != nil {
		log.Printf("[api] save tool file: %v", err)
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "updated"})
}

func DeleteTool(c *gin.Context) {
	id := c.Param("id")
	// 读取记录用于删除对应文件
	t, _ := store.GetTool(id)
	if err := store.DeleteTool(id); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	if t != nil {
		if err := store.DeleteToolFile(t.ToolType, t.Name); err != nil {
			log.Printf("[api] delete tool file: %v", err)
		}
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "deleted"})
}
