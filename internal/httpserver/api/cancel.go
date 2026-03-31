package api

import (
	"feishu-agent/internal/executor"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CancelTrigger POST /api/triggers/:id/cancel
func CancelTrigger(c *gin.Context) {
	triggerID := c.Param("id")

	// 检查 trigger 是否存在且在运行中
	t, err := store.GetTrigger(triggerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	if t == nil {
		c.JSON(http.StatusNotFound, model.APIResponse{Code: 404, Message: "trigger not found"})
		return
	}
	if t.Status != "pending" && t.Status != "running" {
		c.JSON(http.StatusOK, model.APIResponse{Code: 1, Message: "该任务已结束，无法取消"})
		return
	}

	// 取消进程
	if !executor.CancelJob(triggerID) {
		// 进程可能已经结束但状态未更新，直接标记
		store.UpdateTriggerStatus(triggerID, "cancelled", "", "", "", "手动取消（进程已退出）") //nolint
		c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "已标记为取消"})
		return
	}

	// CancelJob 调用了 cancelFunc，runner 的 defer 会更新状态
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Message: "正在取消..."})
}

// ListActiveJobs GET /api/jobs/active
func ListActiveJobs(c *gin.Context) {
	ids := executor.ListActiveJobs()
	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Data: ids})
}
