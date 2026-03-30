// Package store 提供 SQLite 数据访问层
package store

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，无需 CGO
)

// DB 全局数据库连接
var DB *sql.DB

// Init 初始化数据库连接并执行 schema 迁移
func Init(dbPath string) error {
	// 确保目录存在
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create db dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}

	// 连接池配置（SQLite 不支持大并发，保守设置）
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	if err = db.Ping(); err != nil {
		return fmt.Errorf("ping sqlite: %w", err)
	}

	DB = db
	log.Printf("[store] SQLite connected: %s", dbPath)

	return migrate(db)
}

// migrate 执行 schema 建表（幂等）
func migrate(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS app_settings (
			key        TEXT PRIMARY KEY,
			value      TEXT NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS project_routes (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			keywords   TEXT NOT NULL DEFAULT '[]',
			repo_path  TEXT NOT NULL DEFAULT '',
			remote_url TEXT NOT NULL DEFAULT '',
			doc_source TEXT NOT NULL DEFAULT '',
			mcp_list   TEXT NOT NULL DEFAULT '[]',
			skill_list TEXT NOT NULL DEFAULT '[]',
			priority   INTEGER NOT NULL DEFAULT 0,
			enabled    INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS tool_configs (
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL UNIQUE,
			tool_type     TEXT NOT NULL DEFAULT 'mcp',
			description   TEXT NOT NULL DEFAULT '',
			command       TEXT NOT NULL DEFAULT '',
			args_template TEXT NOT NULL DEFAULT '{}',
			enabled       INTEGER NOT NULL DEFAULT 1,
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS prompt_templates (
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL UNIQUE,
			template_type TEXT NOT NULL DEFAULT 'system',
			content       TEXT NOT NULL DEFAULT '',
			enabled       INTEGER NOT NULL DEFAULT 1,
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS triggers (
			id               TEXT PRIMARY KEY,
			raw_message      TEXT NOT NULL DEFAULT '',
			sender_id        TEXT NOT NULL DEFAULT '',
			sender_name      TEXT NOT NULL DEFAULT '',
			chat_id          TEXT NOT NULL DEFAULT '',
			chat_type        TEXT NOT NULL DEFAULT '',
			message_id       TEXT NOT NULL DEFAULT '',
			intent           TEXT NOT NULL DEFAULT '',
			confidence       REAL NOT NULL DEFAULT 0,
			matched_project  TEXT NOT NULL DEFAULT '',
			status           TEXT NOT NULL DEFAULT 'pending',
			result_summary   TEXT NOT NULL DEFAULT '',
			mr_link          TEXT NOT NULL DEFAULT '',
			sql_suggestions  TEXT NOT NULL DEFAULT '',
			error_msg        TEXT NOT NULL DEFAULT '',
			started_at       DATETIME,
			finished_at      DATETIME,
			created_at       DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS trigger_steps (
			id          TEXT PRIMARY KEY,
			trigger_id  TEXT NOT NULL,
			step_index  INTEGER NOT NULL DEFAULT 0,
			step_name   TEXT NOT NULL DEFAULT '',
			step_type   TEXT NOT NULL DEFAULT '',
			input_data  TEXT NOT NULL DEFAULT '',
			output_data TEXT NOT NULL DEFAULT '',
			status      TEXT NOT NULL DEFAULT 'pending',
			error_msg   TEXT NOT NULL DEFAULT '',
			started_at  DATETIME,
			finished_at DATETIME,
			FOREIGN KEY (trigger_id) REFERENCES triggers(id)
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id          TEXT PRIMARY KEY,
			trigger_id  TEXT NOT NULL DEFAULT '',
			action      TEXT NOT NULL DEFAULT '',
			risk_level  TEXT NOT NULL DEFAULT 'low',
			detail      TEXT NOT NULL DEFAULT '',
			operator    TEXT NOT NULL DEFAULT 'system',
			result      TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS processed_events (
			event_id     TEXT PRIMARY KEY,
			processed_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_triggers_created_at ON triggers(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_trigger_steps_trigger_id ON trigger_steps(trigger_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_trigger_id ON audit_logs(trigger_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC)`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate exec: %w\nSQL: %s", err, stmt)
		}
	}

	// 写入默认提示词模板（如果不存在）
	if err := seedDefaultPrompts(db); err != nil {
		log.Printf("[store] seed prompts warning: %v", err)
	}

	log.Printf("[store] schema migration done")
	return nil
}

// seedDefaultPrompts 写入默认提示词（幂等）
func seedDefaultPrompts(db *sql.DB) error {
	templates := []struct {
		id, name, ttype, content string
	}{
		{
			"tpl-system-default",
			"系统默认提示词",
			"system",
			`你是一个专业的工程助手，负责分析飞书消息并执行相应的工程任务。
你必须严格按照 JSON 格式输出结果，不得输出任何额外文字。
保持专业、简洁、准确。`,
		},
		{
			"tpl-intent-default",
			"意图识别提示词",
			"intent",
			`请分析以下用户消息，判断意图类型。

用户消息：
{{.Message}}

请严格按照如下 JSON 格式输出，不得有任何额外内容：
{
  "intent": "<issue_troubleshooting|requirement_writing|ignore|need_more_context|risky_action>",
  "confidence": <0.0-1.0>,
  "matched_keywords": ["<keyword1>", "<keyword2>"],
  "suspected_project": "<project_name_or_empty>",
  "need_repo_access": <true|false>,
  "need_doc_access": <true|false>,
  "need_db_query": <true|false>,
  "risk_level": "<low|medium|high|critical>",
  "summary": "<一句话摘要>"
}`,
		},
		{
			"tpl-issue-default",
			"问题排查提示词",
			"issue",
			`你是一个资深后端工程师，正在排查一个线上问题。

消息内容：{{.Message}}
项目信息：{{.ProjectInfo}}
查询结果：{{.QueryResults}}

请分析并给出：
1. 问题根因（配置问题 / 数据问题 / 代码问题 / 暂时无法确认）
2. 详细分析过程
3. 修复建议
4. 涉及的 SQL 操作建议（如有）

以 JSON 格式输出：
{
  "root_cause_type": "<config|data|code|unknown>",
  "analysis": "<详细分析>",
  "fix_suggestion": "<修复建议>",
  "sql_suggestions": ["<SQL1>", "<SQL2>"],
  "confidence": <0.0-1.0>
}`,
		},
		{
			"tpl-requirement-default",
			"需求编写提示词",
			"requirement",
			`你是一个资深技术负责人，正在为一个新需求编写实现方案。

需求描述：{{.Message}}
项目信息：{{.ProjectInfo}}
相关文档：{{.DocContent}}

请输出完整的实现方案，以 JSON 格式返回：
{
  "background": "<背景>",
  "objective": "<目标>",
  "scope": "<范围>",
  "technical_plan": "<技术方案详细描述>",
  "db_changes": ["<数据库变更说明>"],
  "sql_suggestions": ["<建议SQL>"],
  "risks": ["<风险点>"],
  "test_suggestions": ["<测试建议>"],
  "estimated_files": ["<预计修改的文件路径>"]
}`,
		},
	}

	for _, t := range templates {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO prompt_templates (id, name, template_type, content, enabled)
			VALUES (?, ?, ?, ?, 1)
		`, t.id, t.name, t.ttype, t.content)
		if err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭数据库连接
func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}

// nowStr 返回当前时间字符串（SQLite 兼容格式）
func nowStr() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}
