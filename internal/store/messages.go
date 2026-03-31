package store

import (
	"feishu-agent/internal/model"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SaveChatMessage 保存一条对话消息
func SaveChatMessage(m *model.ChatMessage) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	_, err := DB.Exec(`
		INSERT INTO chat_messages
		(id, trigger_id, direction, chat_id, chat_type, sender_id, message_id, msg_type, content, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		m.ID, m.TriggerID, m.Direction, m.ChatID, m.ChatType,
		m.SenderID, m.MessageID, m.MsgType, m.Content, nowStr(),
	)
	if err != nil {
		return fmt.Errorf("save chat message: %w", err)
	}
	return nil
}

// ListChatMessages 分页查询对话消息，按时间倒序
func ListChatMessages(page, size int) ([]model.ChatMessage, int, error) {
	var total int
	err := DB.QueryRow(`SELECT COUNT(*) FROM chat_messages`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count chat messages: %w", err)
	}

	offset := (page - 1) * size
	rows, err := DB.Query(`
		SELECT id, trigger_id, direction, chat_id, chat_type, sender_id,
		       message_id, msg_type, content, created_at
		FROM chat_messages
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, size, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list chat messages: %w", err)
	}
	defer rows.Close()

	var msgs []model.ChatMessage
	for rows.Next() {
		var m model.ChatMessage
		var createdAt string
		if err := rows.Scan(&m.ID, &m.TriggerID, &m.Direction, &m.ChatID,
			&m.ChatType, &m.SenderID, &m.MessageID, &m.MsgType, &m.Content,
			&createdAt); err != nil {
			return nil, 0, err
		}
		m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		msgs = append(msgs, m)
	}
	return msgs, total, nil
}
