# feishu-agent

飞书消息驱动的自动问题排查 / 需求编写系统。

在飞书群聊中 @ 机器人，或直接发私信，即可触发 AI 自动分析、查询日志、定位代码、生成修复方案或需求文档，并可选择自动提交 MR。

---

## 功能概览

| 能力 | 说明 |
|------|------|
| **意图识别** | 用 Claude 判断消息是"问题排查"还是"需求编写"，自动分流 |
| **项目路由** | 关键词匹配，将消息路由到对应代码仓库和工具集 |
| **问题排查工作流** | MCP 查日志 → LLM 分析 → 生成修复代码 → Git Commit → 创建 MR |
| **需求编写工作流** | 拉取文档 → 生成技术方案 → 生成代码 → Git Commit → 创建 MR |
| **安全护栏（Harness）** | auto-push / auto-MR / DB 写入均默认关闭，高风险动作强制人工确认 |
| **管理后台** | 6 个 Web 页面，配置飞书/LLM、管理路由和工具、查看执行记录 |

---

## 快速开始

### 前置条件

- Go 1.21+
- 飞书开放平台企业自建应用（获取 App ID / App Secret）
- Claude API Key（或兼容 Anthropic 协议的代理）

### 1. 克隆并启动

```bash
git clone <repo-url>
cd feishu-agent
make run
```

首次启动时 SQLite 数据库自动创建，无需额外初始化。

### 2. 打开管理后台

浏览器访问 `http://localhost:8080`，进入「飞书配置」页面填写：

- **飞书 App ID / App Secret**：在飞书开放平台「凭证与基础信息」页面获取
- **Verification Token / Encrypt Key**：在「事件订阅」页面获取（可选）
- **机器人 Open ID**：群聊场景下用于过滤 @ 消息（可选）
- **LLM API Key / Base URL / Model**：Claude API 配置

填写后点击「保存配置」，无需重启服务即时生效。

### 3. 配置飞书 Webhook

在飞书开放平台「事件订阅」中，将请求地址设为：

```
http://你的服务地址/webhook/feishu
```

订阅事件：`im.message.receive_v1`（接收消息）。

首次配置时飞书会发送 URL Challenge，服务会自动响应。

### 4. 配置项目路由

在管理后台「项目路由」页面，为每个代码仓库添加路由规则：

- **关键词**：触发该路由的关键词列表（如 `订单`, `支付`）
- **仓库路径**：服务器上的本地 Git 仓库绝对路径
- **Remote URL**：用于创建 MR 的远程仓库地址
- **MCP 工具**：该项目关联的 MCP 工具名称列表

---

## 架构说明

```
飞书消息
  │
  ▼
Webhook 接收（签名验证 + 解密 + 去重）
  │
  ▼
意图识别（Claude API → JSON）
  │
  ├─ ignore          → 忽略
  ├─ need_more_context → 回复引导
  ├─ risky_action    → 人工确认
  │
  ├─ issue_troubleshooting ──→ 问题排查工作流
  │                              MCP 查日志 → LLM 分析 → Claude Code 修复 → Git → MR
  │
  └─ requirement_writing ────→ 需求编写工作流
                                 文档拉取 → 方案生成 → Claude Code 编码 → Git → MR
```

### 配置优先级

```
SQLite（管理后台保存）> config.yaml（YAML 文件）> 代码内置默认值
```

DB 中的配置在服务启动时自动加载，保存后立即生效，无需重启。

---

## 管理后台页面

| 路径 | 功能 |
|------|------|
| `/` | 仪表盘：触发统计、近期记录 |
| `/settings` | 飞书 & LLM 配置 |
| `/routes` | 项目路由管理 |
| `/tools` | MCP / Shell 工具配置 |
| `/harness` | 执行策略（自动化开关） |
| `/triggers` | 触发记录与执行步骤详情 |

---

## 配置文件

`config.yaml` 为模板文件，所有敏感字段默认为空（通过管理后台填写）：

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  debug: false

feishu:
  app_id: ""
  app_secret: ""

llm:
  api_key: ""
  base_url: "https://api.anthropic.com"
  model: "claude-opus-4-6"
  max_tokens: 4096
  timeout_seconds: 60

harness:
  auto_commit: false      # 自动 git commit
  auto_push: false        # 自动 git push
  auto_create_mr: false   # 自动创建 MR
  allow_db_write: false   # 允许写数据库
  require_confirm_on_risky: true   # 高风险动作需人工确认
  dry_run: false          # 演习模式（只生成方案，不执行）
```

---

## MCP 工具扩展

在「工具配置」页面添加工具，`tool_type` 选 `mcp`，`command` 填写 MCP 服务的 HTTP 地址。
未配置地址时使用内置 Mock 客户端，返回模拟数据，适合本地开发调试。

工具注册后，在项目路由的「MCP 工具」字段中按名称引用即可。

---

## 开发

```bash
make run      # 启动开发服务
make build    # 编译二进制到 ./bin/feishu-agent
make test     # 运行测试
make fmt      # 格式化代码
make clean    # 清理编译产物和本地数据库
```

**敏感文件**：`feishu-agent.db`（含 API Key）已加入 `.gitignore`，请勿手动提交。
