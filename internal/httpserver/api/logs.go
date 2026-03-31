package api

import (
	"feishu-agent/internal/executor"
	"feishu-agent/internal/model"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
)

// GetTriggerLogs GET /api/triggers/:id/logs?offset=0
// 从日志文件的指定偏移量开始读取新内容，支持前端轮询
func GetTriggerLogs(c *gin.Context) {
	triggerID := c.Param("id")
	offset, _ := strconv.ParseInt(c.DefaultQuery("offset", "0"), 10, 64)

	logPath := executor.LogFilePath(triggerID)
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, model.APIResponse{Code: 0, Data: map[string]any{
				"content": "",
				"offset":  0,
				"done":    false,
			}})
			return
		}
		c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: err.Error()})
		return
	}
	defer f.Close()

	// 获取文件大小
	info, _ := f.Stat()
	size := info.Size()

	if offset >= size {
		// 没有新内容，检查是否已完成
		c.JSON(http.StatusOK, model.APIResponse{Code: 0, Data: map[string]any{
			"content": "",
			"offset":  offset,
			"done":    isDone(logPath),
		}})
		return
	}

	// 从 offset 开始读取
	f.Seek(offset, 0) //nolint
	buf := make([]byte, size-offset)
	n, _ := f.Read(buf)
	newOffset := offset + int64(n)

	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Data: map[string]any{
		"content": string(buf[:n]),
		"offset":  newOffset,
		"done":    isDone(logPath),
	}})
}

// isDone 检查日志是否已写入完成标记
func isDone(logPath string) bool {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return false
	}
	s := string(data)
	return len(s) > 6 && (s[len(s)-7:] == "[DONE]\n" || s[len(s)-6:] == "[DONE]" ||
		contains(s, "[DONE]") || contains(s, "[ERROR]"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchLast(s, substr)
}

func searchLast(s, substr string) bool {
	// 只检查最后 200 字符
	start := len(s) - 200
	if start < 0 {
		start = 0
	}
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
