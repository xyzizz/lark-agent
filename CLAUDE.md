# feishu-agent — CLAUDE.md

项目开发约定，供 Claude Code 在此 repo 工作时遵循。

---

## 技术栈

| 层级 | 选型 |
|------|------|
| 语言 | Go 1.25+ |
| HTTP 框架 | Gin (`github.com/gin-gonic/gin`) |
| 数据库 | SQLite via `modernc.org/sqlite`（pure-Go，驱动名 `"sqlite"`） |
| 配置 | YAML（`config.yaml`）+ SQLite 运行时覆盖 |
| 前端 | 原生 HTML/CSS/JS，无框架；终端页使用 xterm.js |
| LLM 意图识别 | Claude API（直接 HTTP 调用，`x-api-key` + `anthropic-version: 2023-06-01`） |
| 工作流分析 | Claude Code CLI（`claude -p`，只分析不修改文件） |
| Web 终端 | PTY + WebSocket + xterm.js（交互式 claude 会话，需要修改时手动操作） |
| 飞书 SDK | `github.com/larksuite/oapi-sdk-go/v3`（WebSocket 长连接 + 事件分发） |

---

## 项目结构

```
cmd/server/main.go          # 入口：加载配置 → 加载工具文件 → 初始化 DB → 启动 HTTP + WebSocket
internal/
  config/config.go          # 全局配置（sync.RWMutex + sync.Once），config.Get()/Update()
  store/                    # SQLite 存取层（db.go 含内联 schema）
    toolfiles.go            # tools/ 目录文件读写（MCP→JSON, Skill→Markdown）
    messages.go             # 对话消息记录
  feishu/                   # 飞书 API 客户端 + Webhook 解析 + WebSocket 长连接 + 消息发送
  intent/recognizer.go      # LLM 意图识别，loadLLMConfig() 每次调用时读 DB
  router/matcher.go         # 关键词路由，将消息匹配到 ProjectRoute
  workflow/                 # 工作流编排
    runner.go               # 入口：意图识别 → 路由 → 分发 → 10分钟超时 + defer 安全网
    issue.go                # 问题排查工作流（构建 prompt → claude -p 分析）
    requirement.go          # 需求编写工作流（构建 prompt → claude -p 分析）
    helpers.go              # 共享工具函数（logStep, finishStep, AuditAction）
  executor/                 # 底层执行器
    claude.go               # Claude Code CLI 调用（claude -p，只分析）+ Job 注册表（取消支持）
    mcp.go                  # MCPClient 接口 + Mock + HTTP 实现 + 全局注册表
    git.go                  # Git 操作封装 + MRCreator 接口
    shell.go                # Shell 命令执行 + ~ 路径展开
  httpserver/
    server.go               # Gin 路由注册 + Feishu Webhook 处理
    terminal.go             # WebSocket 终端（PTY + xterm.js，交互式 claude）
    api/                    # REST API 处理器
  model/models.go           # 所有核心数据结构
tools/                      # 项目级工具定义（启动时自动加载到 DB）
  mcp/                      # MCP 服务配置（.json 文件）
  skill/                    # Skill 配置（.md 文件，Markdown + frontmatter）
web/                        # 静态前端（8 个 HTML 页面 + app.js + style.css）
```

---

## 核心约定

### 工作流执行模型（分析 + 手动执行）
工作流分为两阶段：
1. **自动分析**（飞书触发）：意图识别 → 路由 → `claude -p` 输出分析方案 → 飞书回复
2. **手动执行**（Web 终端）：用户在管理后台打开交互式 claude 终端，手动确认并执行修改

`claude -p` 的 prompt 明确要求「只输出分析和建议，不修改文件」。

### 工具文件格式
- **MCP**：`tools/mcp/{name}.json`（JSON 格式）
- **Skill**：`tools/skill/{name}.md`（Markdown + YAML frontmatter）
- 下划线开头的文件（如 `_example.json`）为示例，启动时自动跳过
- 启动时扫描 `tools/` 目录并同步到 DB；管理后台保存/删除时同步到文件

### LLM 配置读取
**始终用 `loadLLMConfig()`（在 `internal/intent/recognizer.go`），不要直接读 `config.Get().LLM`。**
`loadLLMConfig()` 在每次调用时从 SQLite 读取最新值并覆盖 YAML，确保通过管理后台保存的配置立即生效。

### 配置同步双保险
- **启动时**：`main.go` 的 `applyDBSettings()` 将 DB 配置合并进内存 Config
- **保存时**：`api/settings.go` 的 `applySettingsToConfig()` 在写 DB 后立即同步内存

两处都需要更新，否则重启后或热保存后会有一端不一致。

### 消息安全
- **私聊优先**：默认只接受私聊消息，群聊默认关闭（`feishu_allow_group` 控制）
- **发送者白名单**：`feishu_allowed_senders` 配置允许的 Open ID，非白名单用户的消息直接丢弃
- **发送层防线**：`Runner.isSendAllowed()` 在发送回复前再次校验白名单，防止任何路径绕过
- **ManualTrigger 限制**：`/api/manual/trigger` 必须选择白名单中的用户
- **收到消息回执**：通过 `AddReaction("TYPING")` 给消息加表情，让用户知道已收到

### 任务管理
- **10 分钟超时**：`HandleMessage` 的 context 带 10 分钟 deadline
- **defer 安全网**：无论 panic、超时还是手动取消，trigger 状态都会被更新
- **手动取消**：`POST /api/triggers/:id/cancel` 调用 job 的 cancelFunc 终止进程
- **Job 注册表**：`executor.RegisterJob` / `CancelJob` / `ListActiveJobs` 跟踪活跃任务

### API 响应格式
统一使用 `model.APIResponse{Code, Message, Data}`：
- 成功：`Code: 0`
- 业务错误：`Code: 1`（HTTP 200）
- 服务错误：`Code: 500`（HTTP 500）

### 前端
- 所有 API 调用走 `app.js` 中的 `API.get()` / `API.post()`
- 通知用 `Toast.success()` / `Toast.error()` / `Toast.info()`
- 加载状态用 `setLoading(btn, true/false)`
- 密码/密钥字段不回显明文，已保存时只更新 `placeholder`
- 终端页使用 xterm.js + WebSocket，PTY 交互式 claude 会话

### 数据库 Schema
Schema 内联在 `store/db.go` 的 `migrate()` 中（`CREATE TABLE IF NOT EXISTS`，幂等），不依赖外部文件。
启动时自动迁移旧列（`repo_path` → `repos`）和清理废弃模板。

### 工具类型
`ToolConfig.ToolType` 取值：`mcp` | `skill`。
`PromptTemplate.TemplateType` 取值：`system` | `intent`。

### 飞书事件接收模式
`FeishuConfig.EventMode` 取值：`webhook`（默认）| `websocket`。
- **websocket**（推荐）：飞书官方 WebSocket 长连接，无需公网地址
- **webhook**：传统 HTTP 回调，需要公网地址
- 切换模式需要重启服务
- 消息格式支持：text（纯文本）+ post（富文本，自动提取文字和链接）

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
- `tools/`：项目级工具定义，不含敏感信息，**可以提交**
- `logs/`：日志目录（含 claude 执行日志），已加入 `.gitignore`
