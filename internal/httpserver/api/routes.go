package api

import (
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ListRoutes GET /api/routes
func ListRoutes(c *gin.Context) {
	routes, err := store.ListRoutes(false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "ok", Data: routes})
}

// CreateRoute POST /api/routes
func CreateRoute(c *gin.Context) {
	var r model.ProjectRoute
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: err.Error()})
		return
	}
	if r.Name == "" {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: "name is required"})
		return
	}
	if err := store.CreateRoute(&r); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "created", Data: r})
}

// UpdateRoute PUT /api/routes/:id
func UpdateRoute(c *gin.Context) {
	id := c.Param("id")
	var r model.ProjectRoute
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: err.Error()})
		return
	}
	r.ID = id
	if err := store.UpdateRoute(&r); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "updated"})
}

// DeleteRoute DELETE /api/routes/:id
func DeleteRoute(c *gin.Context) {
	id := c.Param("id")
	if err := store.DeleteRoute(id); err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "deleted"})
}
