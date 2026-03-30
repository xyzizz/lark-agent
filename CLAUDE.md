# feishu-agent — CLAUDE.md

项目开发约定，供 Claude Code 在此 repo 工作时遵循。

---

## 技术栈

| 层级 | 选型 |
|------|------|
| 语言 | Go 1.21 |
| HTTP 框架 | Gin (`github.com/gin-gonic/gin`) |
| 数据库 | SQLite via `modernc.org/sqlite`（pure-Go，驱动名 `"sqlite"`） |
| 配置 | YAML（`config.yaml`）+ SQLite 运行时覆盖 |
| 前端 | 原生 HTML/CSS/JS，无框架 |
| LLM | Claude API（直接 HTTP 调用，`x-api-key` + `anthropic-version: 2023-06-01`） |

---

## 项目结构

```
cmd/server/main.go          # 入口：加载配置 → 初始化 DB → 启动 HTTP
internal/
  config/config.go          # 全局配置（sync.RWMutex + sync.Once），config.Get()/Update()
  store/                    # SQLite 存取层（db.go 含内联 schema）
  feishu/                   # 飞书 API 客户端 + Webhook 解析 + 消息发送
  intent/recognizer.go      # LLM 意图识别，loadLLMConfig() 每次调用时读 DB
  router/matcher.go         # 关键词路由，将消息匹配到 ProjectRoute
  workflow/                 # 工作流编排
    runner.go               # 入口：意图识别 → 路由 → 分发
    issue.go                # 问题排查工作流（9 步）
    requirement.go          # 需求编写工作流
  executor/                 # 底层执行器
    mcp.go                  # MCPClient 接口 + Mock + HTTP 实现 + 全局注册表
    git.go                  # Git 操作封装 + MRCreator 接口
    claude.go               # Claude Code 子进程调用
    shell.go                # Shell 命令执行
  httpserver/
    server.go               # Gin 路由注册 + Feishu Webhook 处理
    api/                    # REST API 处理器
  model/models.go           # 所有核心数据结构
web/                        # 静态前端（6 个 HTML 页面 + app.js + style.css）
```

---

## 核心约定

### LLM 配置读取
**始终用 `loadLLMConfig()`（在 `internal/intent/recognizer.go`），不要直接读 `config.Get().LLM`。**
`loadLLMConfig()` 在每次调用时从 SQLite 读取最新值并覆盖 YAML，确保通过管理后台保存的配置立即生效。

```go
// 正确做法
llm := loadLLMConfig()
req.Header.Set("x-api-key", llm.APIKey)

// 错误做法（不读 DB，重启前改动无效）
cfg := config.Get()
req.Header.Set("x-api-key", cfg.LLM.APIKey)
```

### 配置同步双保险
- **启动时**：`main.go` 的 `applyDBSettings()` 将 DB 配置合并进内存 Config
- **保存时**：`api/settings.go` 的 `applySettingsToConfig()` 在写 DB 后立即同步内存

两处都需要更新，否则重启后或热保存后会有一端不一致。

### API 响应格式
统一使用 `model.APIResponse{Code, Message, Data}`：
- 成功：`Code: 0`
- 业务错误：`Code: 1`（HTTP 200）
- 服务错误：`Code: 500`（HTTP 500）

### 变量命名
在同一函数中同时需要"请求体结构体"和 `*http.Request` 时，HTTP 请求变量命名为 `httpReq`，避免与解析结构体的 `req` 变量冲突（见 `TestLLM`）。

### 前端
- 所有 API 调用走 `app.js` 中的 `API.get()` / `API.post()`
- 通知用 `Toast.success()` / `Toast.error()` / `Toast.info()`
- 加载状态用 `setLoading(btn, true/false)`
- 密码/密钥字段不回显明文，已保存时只更新 `placeholder`

### 数据库 Schema
Schema 内联在 `store/db.go` 的 `initSchema()` 中（`CREATE TABLE IF NOT EXISTS`，幂等），不依赖外部 `schema.sql`。`schema.sql` 仅作参考文档。

### 工具类型
`ToolConfig.ToolType` 取值：`mcp` | `skill` | `shell`。
`PromptTemplate.TemplateType` 取值：`system` | `intent` | `issue` | `requirement`。

### 安全默认值
Harness 所有自动化选项默认关闭：
```yaml
auto_commit: false
auto_push: false
auto_create_mr: false
allow_db_write: false
allow_auto_merge: false
```
修改这些默认值需要明确的用户指令，不要在功能开发中顺手打开。

---

## 常用命令

```bash
make run          # go run ./cmd/server/main.go -config ./config.yaml
make build        # 编译到 ./bin/feishu-agent
make tidy         # go mod tidy
make fmt          # gofmt -w ./...
make test         # go test ./...
make clean        # 删除编译产物和本地 DB
```

---

## 敏感文件
- `feishu-agent.db`：含 API Key、App Secret 等，已加入 `.gitignore`，**禁止提交**
- `config.yaml`：模板文件，所有敏感字段为空字符串，**可以提交**
- `logs/`：日志目录，已加入 `.gitignore`
