package store

import (
	"database/sql"
	"errors"
	"feishu-agent/internal/model"
	"fmt"

	"github.com/google/uuid"
)

func ListTools(enabledOnly bool) ([]*model.ToolConfig, error) {
	query := `SELECT id, name, tool_type, description, command, args_template, enabled, created_at, updated_at
		FROM tool_configs`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY name`

	rows, err := DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	defer rows.Close()

	var tools []*model.ToolConfig
	for rows.Next() {
		t, err := scanTool(rows)
		if err != nil {
			return nil, err
		}
		tools = append(tools, t)
	}
	return tools, rows.Err()
}

func GetTool(id string) (*model.ToolConfig, error) {
	row := DB.QueryRow(`SELECT id, name, tool_type, description, command, args_template, enabled, created_at, updated_at
		FROM tool_configs WHERE id = ?`, id)
	t, err := scanTool(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func GetToolByName(name string) (*model.ToolConfig, error) {
	row := DB.QueryRow(`SELECT id, name, tool_type, description, command, args_template, enabled, created_at, updated_at
		FROM tool_configs WHERE name = ?`, name)
	t, err := scanTool(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func CreateTool(t *model.ToolConfig) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	now := nowStr()
	_, err := DB.Exec(`
		INSERT INTO tool_configs (id, name, tool_type, description, command, args_template, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, t.ID, t.Name, t.ToolType, t.Description, t.Command, t.ArgsTemplate, boolToInt(t.Enabled), now, now)
	if err != nil {
		return fmt.Errorf("create tool: %w", err)
	}
	return nil
}

func UpdateTool(t *model.ToolConfig) error {
	_, err := DB.Exec(`
		UPDATE tool_configs SET
			name=?, tool_type=?, description=?, command=?, args_template=?, enabled=?, updated_at=?
		WHERE id=?
	`, t.Name, t.ToolType, t.Description, t.Command, t.ArgsTemplate, boolToInt(t.Enabled), nowStr(), t.ID)
	if err != nil {
		return fmt.Errorf("update tool: %w", err)
	}
	return nil
}

func DeleteTool(id string) error {
	_, err := DB.Exec(`DELETE FROM tool_configs WHERE id = ?`, id)
	return err
}

func scanTool(row scanner) (*model.ToolConfig, error) {
	var t model.ToolConfig
	var enabled int
	err := row.Scan(&t.ID, &t.Name, &t.ToolType, &t.Description, &t.Command, &t.ArgsTemplate, &enabled, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.Enabled = enabled == 1
	return &t, nil
}
