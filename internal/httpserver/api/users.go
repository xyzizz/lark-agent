package api

import (
	"feishu-agent/internal/feishu"
	"feishu-agent/internal/model"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

var (
	feishuClient   *feishu.Client
	userNameCache  sync.Map // open_id → name
)

func SetFeishuClient(c *feishu.Client) { feishuClient = c }

// ResolveUsers GET /api/users/resolve?ids=ou_xxx,ou_yyy
// 批量解析 open_id 为用户名，结果带缓存
func ResolveUsers(c *gin.Context) {
	idsParam := c.Query("ids")
	if idsParam == "" {
		c.JSON(http.StatusOK, model.APIResponse{Code: 0, Data: map[string]string{}})
		return
	}

	ids := strings.Split(idsParam, ",")
	result := make(map[string]string, len(ids))

	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || id == "manual" {
			continue
		}

		// 先查缓存
		if cached, ok := userNameCache.Load(id); ok {
			result[id] = cached.(string)
			continue
		}

		// 调飞书 API 解析
		if feishuClient == nil {
			result[id] = id
			continue
		}
		name, err := feishuClient.GetUserInfo(c.Request.Context(), id)
		if err != nil || name == id {
			result[id] = id
			continue
		}
		userNameCache.Store(id, name)
		result[id] = name
	}

	c.JSON(http.StatusOK, model.APIResponse{Code: 0, Data: result})
}
