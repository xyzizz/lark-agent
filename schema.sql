-- feishu-agent SQLite schema
-- 所有 JSON 字段均存储为 TEXT，业务层负责序列化/反序列化

-- 全局配置 KV 表
CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL DEFAULT '',
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 项目路由配置（关键词 -> 仓库 / 文档 / MCP）
CREATE TABLE IF NOT EXISTS project_routes (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    keywords   TEXT NOT NULL DEFAULT '[]',   -- JSON array of string
    repo_path  TEXT NOT NULL DEFAULT '',     -- 本地仓库绝对路径
    remote_url TEXT NOT NULL DEFAULT '',     -- git remote URL
    doc_source TEXT NOT NULL DEFAULT '',     -- 飞书文档 URL 或描述
    mcp_list   TEXT NOT NULL DEFAULT '[]',   -- JSON array：关联 MCP 名称
    skill_list TEXT NOT NULL DEFAULT '[]',   -- JSON array：关联 skill 名称
    priority   INTEGER NOT NULL DEFAULT 0,   -- 数字越大优先级越高
    enabled    INTEGER NOT NULL DEFAULT 1,   -- 1=启用 0=禁用
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 工具配置（MCP / shell script / skill）
CREATE TABLE IF NOT EXISTS tool_configs (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    tool_type     TEXT NOT NULL DEFAULT 'mcp',  -- mcp | skill | shell
    description   TEXT NOT NULL DEFAULT '',
    command       TEXT NOT NULL DEFAULT '',      -- 可执行命令或 MCP 服务地址
    args_template TEXT NOT NULL DEFAULT '{}',   -- JSON 参数模板
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Prompt 模板
CREATE TABLE IF NOT EXISTS prompt_templates (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    template_type TEXT NOT NULL DEFAULT 'system', -- system | intent | issue | requirement
    content       TEXT NOT NULL DEFAULT '',
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 触发记录（每次收到消息并处理的主记录）
CREATE TABLE IF NOT EXISTS triggers (
    id               TEXT PRIMARY KEY,
    raw_message      TEXT NOT NULL DEFAULT '',  -- 原始消息内容
    sender_id        TEXT NOT NULL DEFAULT '',
    sender_name      TEXT NOT NULL DEFAULT '',
    chat_id          TEXT NOT NULL DEFAULT '',
    chat_type        TEXT NOT NULL DEFAULT '',  -- group | p2p
    message_id       TEXT NOT NULL DEFAULT '',  -- 飞书 message_id，用于去重
    intent           TEXT NOT NULL DEFAULT '',  -- issue_troubleshooting | requirement_writing | ignore | ...
    confidence       REAL NOT NULL DEFAULT 0,
    matched_project  TEXT NOT NULL DEFAULT '',  -- 命中的 project_routes.name
    status           TEXT NOT NULL DEFAULT 'pending', -- pending | running | success | failed | skipped
    result_summary   TEXT NOT NULL DEFAULT '',
    mr_link          TEXT NOT NULL DEFAULT '',
    sql_suggestions  TEXT NOT NULL DEFAULT '',  -- JSON array of SQL statements
    error_msg        TEXT NOT NULL DEFAULT '',
    started_at       DATETIME,
    finished_at      DATETIME,
    created_at       DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 执行步骤明细（每个 trigger 下的子步骤）
CREATE TABLE IF NOT EXISTS trigger_steps (
    id          TEXT PRIMARY KEY,
    trigger_id  TEXT NOT NULL,
    step_index  INTEGER NOT NULL DEFAULT 0,
    step_name   TEXT NOT NULL DEFAULT '',
    step_type   TEXT NOT NULL DEFAULT '',  -- info | llm | tool_call | shell | git | feishu
    input_data  TEXT NOT NULL DEFAULT '',  -- 步骤输入（可为空）
    output_data TEXT NOT NULL DEFAULT '',  -- 步骤输出
    status      TEXT NOT NULL DEFAULT 'pending', -- pending | running | success | failed | skipped
    error_msg   TEXT NOT NULL DEFAULT '',
    started_at  DATETIME,
    finished_at DATETIME,
    FOREIGN KEY (trigger_id) REFERENCES triggers(id)
);

-- 审计日志（高风险动作强制记录）
CREATE TABLE IF NOT EXISTS audit_logs (
    id          TEXT PRIMARY KEY,
    trigger_id  TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL DEFAULT '',       -- 动作描述，如 git.push / db.write / mr.create
    risk_level  TEXT NOT NULL DEFAULT 'low',    -- low | medium | high | critical
    detail      TEXT NOT NULL DEFAULT '',       -- 详细参数（JSON 或文本）
    operator    TEXT NOT NULL DEFAULT 'system', -- 操作主体
    result      TEXT NOT NULL DEFAULT '',       -- 执行结果
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 消息去重表（防止同一飞书事件重复处理）
CREATE TABLE IF NOT EXISTS processed_events (
    event_id    TEXT PRIMARY KEY,
    processed_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_triggers_created_at ON triggers(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_trigger_steps_trigger_id ON trigger_steps(trigger_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_trigger_id ON audit_logs(trigger_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
