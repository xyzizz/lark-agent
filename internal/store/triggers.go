package store

import (
	"database/sql"
	"errors"
	"feishu-agent/internal/model"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ─── Trigger ──────────────────────────────────────────────────

func CreateTrigger(t *model.Trigger) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	_, err := DB.Exec(`
		INSERT INTO triggers
		(id, raw_message, sender_id, sender_name, chat_id, chat_type, message_id,
		 intent, confidence, matched_project, status, result_summary, mr_link,
		 sql_suggestions, error_msg, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		t.ID, t.RawMessage, t.SenderID, t.SenderName, t.ChatID, t.ChatType,
		t.MessageID, t.Intent, t.Confidence, t.MatchedProject, t.Status,
		t.ResultSummary, t.MRLink, t.SQLSuggestions, t.ErrorMsg, nowStr(),
	)
	if err != nil {
		return fmt.Errorf("create trigger: %w", err)
	}
	return nil
}

func UpdateTriggerStatus(id, status, summary, mrLink, sqlSuggestions, errMsg string) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	var finishedAt *string
	if status == "success" || status == "failed" || status == "skipped" {
		finishedAt = &now
	}
	_, err := DB.Exec(`
		UPDATE triggers SET
			status=?, result_summary=?, mr_link=?, sql_suggestions=?, error_msg=?, finished_at=?
		WHERE id=?
	`, status, summary, mrLink, sqlSuggestions, errMsg, finishedAt, id)
	return err
}

// UpdateTriggerIntent 更新意图识别和路由匹配结果
func UpdateTriggerIntent(id, intent string, confidence float64, matchedProject string) error {
	_, err := DB.Exec(`UPDATE triggers SET intent=?, confidence=?, matched_project=? WHERE id=?`,
		intent, confidence, matchedProject, id)
	return err
}

func SetTriggerStarted(id string) error {
	now := nowStr()
	_, err := DB.Exec(`UPDATE triggers SET status='running', started_at=? WHERE id=?`, now, id)
	return err
}

func GetTrigger(id string) (*model.Trigger, error) {
	row := DB.QueryRow(`SELECT id, raw_message, sender_id, sender_name, chat_id, chat_type, message_id,
		intent, confidence, matched_project, status, result_summary, mr_link, sql_suggestions, error_msg,
		started_at, finished_at, created_at
		FROM triggers WHERE id = ?`, id)
	t, err := scanTrigger(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// 加载步骤
	steps, err := ListTriggerSteps(id)
	if err != nil {
		return nil, err
	}
	t.Steps = steps
	return t, nil
}

func ListTriggers(page, size int) ([]*model.Trigger, int, error) {
	var total int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM triggers`).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * size
	rows, err := DB.Query(`SELECT id, raw_message, sender_id, sender_name, chat_id, chat_type, message_id,
		intent, confidence, matched_project, status, result_summary, mr_link, sql_suggestions, error_msg,
		started_at, finished_at, created_at
		FROM triggers ORDER BY created_at DESC LIMIT ? OFFSET ?`, size, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var triggers []*model.Trigger
	for rows.Next() {
		t, err := scanTrigger(rows)
		if err != nil {
			return nil, 0, err
		}
		triggers = append(triggers, t)
	}
	return triggers, total, rows.Err()
}

func GetDashboardStats() (*model.DashboardStats, error) {
	stats := &model.DashboardStats{ServiceStatus: "running"}
	DB.QueryRow(`SELECT COUNT(*) FROM triggers`).Scan(&stats.TotalTriggers)                   //nolint
	DB.QueryRow(`SELECT COUNT(*) FROM triggers WHERE status='success'`).Scan(&stats.SuccessCount) //nolint
	DB.QueryRow(`SELECT COUNT(*) FROM triggers WHERE status='failed'`).Scan(&stats.FailedCount)   //nolint
	DB.QueryRow(`SELECT COUNT(*) FROM triggers WHERE status='pending' OR status='running'`).Scan(&stats.PendingCount) //nolint

	// 最近 10 条
	rows, err := DB.Query(`SELECT id, raw_message, sender_id, sender_name, chat_id, chat_type, message_id,
		intent, confidence, matched_project, status, result_summary, mr_link, sql_suggestions, error_msg,
		started_at, finished_at, created_at
		FROM triggers ORDER BY created_at DESC LIMIT 10`)
	if err != nil {
		return stats, nil
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanTrigger(rows)
		if err == nil {
			stats.RecentTriggers = append(stats.RecentTriggers, t)
		}
	}
	return stats, nil
}

func scanTrigger(row scanner) (*model.Trigger, error) {
	var t model.Trigger
	err := row.Scan(
		&t.ID, &t.RawMessage, &t.SenderID, &t.SenderName, &t.ChatID, &t.ChatType, &t.MessageID,
		&t.Intent, &t.Confidence, &t.MatchedProject, &t.Status, &t.ResultSummary, &t.MRLink,
		&t.SQLSuggestions, &t.ErrorMsg, &t.StartedAt, &t.FinishedAt, &t.CreatedAt,
	)
	return &t, err
}

// ─── TriggerStep ─────────────────────────────────────────────

func CreateTriggerStep(s *model.TriggerStep) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	now := nowStr()
	_, err := DB.Exec(`
		INSERT INTO trigger_steps (id, trigger_id, step_index, step_name, step_type, input_data, output_data, status, error_msg, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.ID, s.TriggerID, s.StepIndex, s.StepName, s.StepType, s.InputData, s.OutputData, s.Status, s.ErrorMsg, now)
	return err
}

func UpdateTriggerStep(id, status, output, errMsg string) error {
	now := nowStr()
	_, err := DB.Exec(`UPDATE trigger_steps SET status=?, output_data=?, error_msg=?, finished_at=? WHERE id=?`,
		status, output, errMsg, now, id)
	return err
}

func ListTriggerSteps(triggerID string) ([]*model.TriggerStep, error) {
	rows, err := DB.Query(`SELECT id, trigger_id, step_index, step_name, step_type, input_data, output_data,
		status, error_msg, started_at, finished_at
		FROM trigger_steps WHERE trigger_id = ? ORDER BY step_index`, triggerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []*model.TriggerStep
	for rows.Next() {
		var s model.TriggerStep
		err = rows.Scan(&s.ID, &s.TriggerID, &s.StepIndex, &s.StepName, &s.StepType,
			&s.InputData, &s.OutputData, &s.Status, &s.ErrorMsg, &s.StartedAt, &s.FinishedAt)
		if err != nil {
			return nil, err
		}
		steps = append(steps, &s)
	}
	return steps, rows.Err()
}

// ─── AuditLog ────────────────────────────────────────────────

func CreateAuditLog(l *model.AuditLog) error {
	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	_, err := DB.Exec(`
		INSERT INTO audit_logs (id, trigger_id, action, risk_level, detail, operator, result)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, l.ID, l.TriggerID, l.Action, l.RiskLevel, l.Detail, l.Operator, l.Result)
	return err
}

// ─── 去重 ────────────────────────────────────────────────────

// IsEventProcessed 检查飞书事件是否已处理（去重）
func IsEventProcessed(eventID string) (bool, error) {
	var count int
	err := DB.QueryRow(`SELECT COUNT(*) FROM processed_events WHERE event_id = ?`, eventID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// MarkEventProcessed 标记事件已处理
func MarkEventProcessed(eventID string) error {
	_, err := DB.Exec(`INSERT OR IGNORE INTO processed_events (event_id) VALUES (?)`, eventID)
	return err
}

// CleanOldEvents 清理 24 小时前的去重记录
func CleanOldEvents() error {
	_, err := DB.Exec(`DELETE FROM processed_events WHERE processed_at < datetime('now', '-24 hours')`)
	return err
}
