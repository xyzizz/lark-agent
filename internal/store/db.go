// Package store 提供 SQLite 数据访问层
package store

import (
	"database/sql"
	"encoding/json"
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
			repos      TEXT NOT NULL DEFAULT '[]',
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
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id          TEXT PRIMARY KEY,
			trigger_id  TEXT NOT NULL DEFAULT '',
			direction   TEXT NOT NULL DEFAULT '',
			chat_id     TEXT NOT NULL DEFAULT '',
			chat_type   TEXT NOT NULL DEFAULT '',
			sender_id   TEXT NOT NULL DEFAULT '',
			message_id  TEXT NOT NULL DEFAULT '',
			msg_type    TEXT NOT NULL DEFAULT 'text',
			content     TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_chat_id ON chat_messages(chat_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_created_at ON chat_messages(created_at DESC)`,
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

	// 迁移：repo_path → repo_paths（旧列存在时转换数据）
	migrateRepoPaths(db)

	// 清理已废弃的 prompt_templates 表（提示词已迁移到 tools/prompt/*.md 文件）
	db.Exec(`DROP TABLE IF EXISTS prompt_templates`) //nolint

	log.Printf("[store] schema migration done")
	return nil
}

// migrateRepoPaths 将旧的 repo_path/repo_paths 迁移为 repos JSON 数组
func migrateRepoPaths(db *sql.DB) {
	// 迁移 repo_path（单值字符串）→ repos
	var countOld int
	_ = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('project_routes') WHERE name='repo_path'`).Scan(&countOld)
	if countOld > 0 {
		db.Exec(`ALTER TABLE project_routes ADD COLUMN repos TEXT NOT NULL DEFAULT '[]'`) //nolint
		rows, err := db.Query(`SELECT id, repo_path FROM project_routes WHERE repo_path != ''`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, repoPath string
				if rows.Scan(&id, &repoPath) == nil && repoPath != "" {
					reposJSON := fmt.Sprintf(`[{"path":"%s","description":""}]`, repoPath)
					db.Exec(`UPDATE project_routes SET repos = ? WHERE id = ?`, reposJSON, id) //nolint
				}
			}
		}
		log.Printf("[store] migrated repo_path → repos")
		return
	}

	// 迁移 repo_paths（字符串数组）→ repos
	var countPaths int
	_ = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('project_routes') WHERE name='repo_paths'`).Scan(&countPaths)
	if countPaths > 0 {
		db.Exec(`ALTER TABLE project_routes ADD COLUMN repos TEXT NOT NULL DEFAULT '[]'`) //nolint
		rows, err := db.Query(`SELECT id, repo_paths FROM project_routes WHERE repo_paths != '[]' AND repo_paths != ''`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, pathsStr string
				if rows.Scan(&id, &pathsStr) == nil {
					var paths []string
					if json.Unmarshal([]byte(pathsStr), &paths) == nil && len(paths) > 0 {
						var repos []map[string]string
						for _, p := range paths {
							repos = append(repos, map[string]string{"path": p, "description": ""})
						}
						reposJSON, _ := json.Marshal(repos)
						db.Exec(`UPDATE project_routes SET repos = ? WHERE id = ?`, string(reposJSON), id) //nolint
					}
				}
			}
		}
		log.Printf("[store] migrated repo_paths → repos")
	}
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
