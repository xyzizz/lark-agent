# feishu-agent

飞书消息驱动的智能工程助手。

私聊飞书机器人，即可触发 Claude Code 自动分析问题或设计需求方案。分析结果通过飞书消息返回，需要执行修改时可在 Web 终端中交互式操作。

---

## 功能概览

| 能力 | 说明 |
|------|------|
| **意图识别** | Claude API 判断消息是"问题排查"还是"需求编写"，自动分流 |
| **项目路由** | 关键词匹配，将消息路由到对应代码仓库 |
| **问题排查** | 构建 prompt → Claude Code 分析根因、给出修复方案 |
| **需求编写** | 构建 prompt → Claude Code 设计技术方案、给出代码变更建议 |
| **Web 终端** | 浏览器内交互式 claude 会话，需要执行修改时手动操作 |
| **安全护栏** | 私聊触发 + 发送者白名单 + 多层校验 |
| **对话记录** | 双向消息记录，点击跳转飞书对话 |
| **管理后台** | 8 个 Web 页面，配置飞书/LLM、管理路由和工具、查看执行记录 |

---

## 快速开始

### 前置条件

- Go 1.25+
- Claude Code CLI（已安装并配置好 MCP / Skills）
- 飞书开放平台企业自建应用（获取 App ID / App Secret）
- Claude API Key（用于意图识别）
- **本机部署**无需公网地址（使用 WebSocket 长连接模式）

### 1. 克隆并启动

```bash
git clone <repo-url>
cd feishu-agent
make run
```

首次启动时 SQLite 数据库自动创建，`tools/` 目录下的工具定义自动加载。

### 2. 打开管理后台

浏览器访问 `http://localhost:8080`，进入「飞书配置」页面填写：

- **事件接收模式**：选择「WebSocket 长连接」（推荐，无需公网地址）
- **飞书 App ID / App Secret**：在飞书开放平台「凭证与基础信息」页面获取
- **允许的发送者**：填入你的飞书 Open ID（白名单，确保只有你能触发）
- **LLM API Key / Model**：Claude API 配置（用于意图识别）

填写后点击「保存配置」，重启服务使 WebSocket 模式生效。

### 3. 配置飞书应用

在飞书开放平台：
- 「事件订阅」→ 选择「使用长连接接收事件」
- 订阅事件：`im.message.receive_v1`（接收消息）

### 4. 配置项目路由

在管理后台「项目路由」页面，为每个代码仓库添加路由规则：

- **关键词**：触发该路由的关键词列表（如 `订单`, `支付`）
- **仓库**：本地 Git 仓库路径 + 描述（支持多个，点击"+"添加）

### 5. 使用

**自动分析**：在飞书中私聊机器人发送消息：

- `"订单支付接口报500错误"` → 自动识别为问题排查，返回分析方案
- `"给用户模块加一个头像上传功能"` → 自动识别为需求编写，返回技术方案

**手动执行**：在触发记录详情页点击「打开终端」，进入交互式 claude 会话进行实际修改。

---

## 架构说明

```
飞书私聊消息
  │
  ├─ WebSocket 长连接（推荐，无需公网）
  │  或
  ├─ Webhook（需公网地址）
  │
  ▼
发送者白名单校验 → 添加「敲键盘」表情回执
  │
  ▼
意图识别（Claude API → 轻量分类）
  │
  ├─ ignore           → 忽略
  ├─ need_more_context → 回复引导
  ├─ risky_action     → 人工确认
  │
  ├─ issue_troubleshooting ──→ 构建 prompt → claude -p → 返回分析方案
  │
  └─ requirement_writing ────→ 构建 prompt → claude -p → 返回技术方案

                  需要执行修改时 ──→ Web 终端（交互式 claude 会话）
```

Claude Code 执行时自动利用已安装的 MCP 服务和 Skills，无需在本系统重复配置。

### 配置优先级

```
SQLite（管理后台保存）> config.yaml（YAML 文件）> 代码内置默认值
```

---

## 工具配置

工具定义保存在 `tools/` 目录，启动时自动加载：

```
tools/
  mcp/
    _example.json      # 示例（下划线开头，自动跳过）
    mysql.json          # MCP 服务（JSON 格式）
  skill/
    _example.md         # 示例
    deploy-prod.md      # Skill（Markdown + frontmatter）
```

也可通过管理后台「工具配置」页面在线管理，保存时自动同步到文件。

Claude Code 已全局安装的 MCP 和 Skills 无需在此重复配置，`tools/` 目录用于项目级补充。

---

## 管理后台页面

| 路径 | 功能 |
|------|------|
| `/` | 仪表盘：触发统计、手动触发（选择白名单用户）、近期记录 |
| `/settings` | 飞书 & LLM 配置（事件模式、白名单、群聊开关） |
| `/routes` | 项目路由管理（多仓库 + 描述） |
| `/tools` | MCP / Skill 工具配置（按类型分区展示） |
| `/harness` | 执行策略（自动化开关） |
| `/triggers` | 触发记录（白名单标签 + 一键加白 + 取消执行 + 实时日志） |
| `/messages` | 对话记录（双向消息 + 飞书跳转） |
| `/terminal` | Web 终端（交互式 claude 会话，选择仓库目录启动） |

---

## 安全设计

| 层级 | 措施 |
|------|------|
| 触发方式 | 默认仅私聊触发，群聊默认关闭（`feishu_allow_group` 开关） |
| 发送者 | 白名单过滤（`feishu_allowed_senders`），非白名单消息直接丢弃 |
| 回复防线 | Runner 发送前再次校验白名单，防止任何路径绕过 |
| 执行模式 | `claude -p` 只分析不修改，需要修改时通过 Web 终端人工操作 |
| 任务超时 | 10 分钟自动超时 + 手动取消按钮 |
| 审计 | 所有操作记录审计日志 |

---

## 配置文件

`config.yaml` 为模板文件，所有敏感字段默认为空（通过管理后台填写）：

```yaml
server:
  host: "0.0.0.0"
  port: 8080

feishu:
  app_id: ""
  app_secret: ""
  event_mode: "webhook"  # webhook | websocket

llm:
  api_key: ""
  base_url: "https://api.anthropic.com"
  model: "claude-opus-4-6"
  max_tokens: 4096
```

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

---

## 未来扩展

- **多用户**：在白名单中添加同事的 Open ID，即可支持多人私聊使用
- **群聊模式**：打开「允许群聊触发」开关，群里 @机器人即可触发
