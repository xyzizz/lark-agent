package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// MCPClient MCP 工具调用接口（平台无关抽象）
type MCPClient interface {
	Call(ctx context.Context, method string, params map[string]any) (*model.MCPResponse, error)
	Name() string
}

// ─── Mock 实现（本地开发用）────────────────────────────────────

// MockMCPClient 用于本地开发/测试
type MockMCPClient struct {
	name string
}

func NewMockMCPClient(name string) *MockMCPClient {
	return &MockMCPClient{name: name}
}

func (m *MockMCPClient) Name() string { return m.name }

func (m *MockMCPClient) Call(ctx context.Context, method string, params map[string]any) (*model.MCPResponse, error) {
	paramsJSON, _ := json.Marshal(params)
	return &model.MCPResponse{
		Success: true,
		Data: map[string]any{
			"mock":   true,
			"method": method,
			"params": string(paramsJSON),
			"note":   "This is a mock response. Configure real MCP in tool settings.",
		},
	}, nil
}

// ─── HTTP MCP 实现 ────────────────────────────────────────────

// HTTPMCPClient 通过 HTTP 调用 MCP 服务
type HTTPMCPClient struct {
	name    string
	baseURL string
	httpCli *http.Client
}

func NewHTTPMCPClient(name, baseURL string) *HTTPMCPClient {
	return &HTTPMCPClient{
		name:    name,
		baseURL: baseURL,
		httpCli: &http.Client{Timeout: 30 * time.Second},
	}
}

func (h *HTTPMCPClient) Name() string { return h.name }

func (h *HTTPMCPClient) Call(ctx context.Context, method string, params map[string]any) (*model.MCPResponse, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      time.Now().UnixNano(),
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpCli.Do(req)
	if err != nil {
		return &model.MCPResponse{Error: err.Error()}, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var rpcResp struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err = json.Unmarshal(respBody, &rpcResp); err != nil {
		return &model.MCPResponse{Error: "parse response: " + err.Error()}, err
	}
	if rpcResp.Error != nil {
		return &model.MCPResponse{Error: fmt.Sprintf("[%d] %s", rpcResp.Error.Code, rpcResp.Error.Message)}, nil
	}
	return &model.MCPResponse{Success: true, Data: rpcResp.Result}, nil
}

// ─── MCP 注册表 ───────────────────────────────────────────────

// MCPRegistry 管理所有已配置的 MCP 客户端
type MCPRegistry struct {
	mu      sync.RWMutex
	clients map[string]MCPClient
}

var globalRegistry = &MCPRegistry{
	clients: make(map[string]MCPClient),
}

// Register 注册 MCP 客户端
func (r *MCPRegistry) Register(client MCPClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[client.Name()] = client
}

// Get 按名称获取 MCP 客户端
func (r *MCPRegistry) Get(name string) (MCPClient, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[name]
	return c, ok
}

// GetRegistry 获取全局注册表
func GetRegistry() *MCPRegistry {
	return globalRegistry
}

// InitMCPClients 从数据库配置初始化 MCP 客户端
func InitMCPClients() error {
	tools, err := store.ListTools(true)
	if err != nil {
		return err
	}
	for _, t := range tools {
		if t.ToolType != "mcp" {
			continue
		}
		var client MCPClient
		if t.Command == "" || t.Command == "mock" {
			client = NewMockMCPClient(t.Name)
		} else {
			client = NewHTTPMCPClient(t.Name, t.Command)
		}
		globalRegistry.Register(client)
	}
	return nil
}

// CallMCP 便捷函数：按名称调用 MCP 工具
func CallMCP(ctx context.Context, name, method string, params map[string]any) (*model.MCPResponse, error) {
	client, ok := globalRegistry.Get(name)
	if !ok {
		// 未注册的 MCP，返回 mock 结果
		client = NewMockMCPClient(name)
	}
	return client.Call(ctx, method, params)
}
