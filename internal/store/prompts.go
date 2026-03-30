package store

import (
	"database/sql"
	"errors"
	"feishu-agent/internal/model"
	"fmt"

	"github.com/google/uuid"
)

func ListPrompts() ([]*model.PromptTemplate, error) {
	rows, err := DB.Query(`SELECT id, name, template_type, content, enabled, created_at, updated_at
		FROM prompt_templates ORDER BY template_type, name`)
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}
	defer rows.Close()

	var prompts []*model.PromptTemplate
	for rows.Next() {
		p, err := scanPrompt(rows)
		if err != nil {
			return nil, err
		}
		prompts = append(prompts, p)
	}
	return prompts, rows.Err()
}

func GetPromptByType(templateType string) (*model.PromptTemplate, error) {
	row := DB.QueryRow(`SELECT id, name, template_type, content, enabled, created_at, updated_at
		FROM prompt_templates WHERE template_type = ? AND enabled = 1
		ORDER BY updated_at DESC LIMIT 1`, templateType)
	p, err := scanPrompt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func CreatePrompt(p *model.PromptTemplate) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	now := nowStr()
	_, err := DB.Exec(`
		INSERT INTO prompt_templates (id, name, template_type, content, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, p.ID, p.Name, p.TemplateType, p.Content, boolToInt(p.Enabled), now, now)
	if err != nil {
		return fmt.Errorf("create prompt: %w", err)
	}
	return nil
}

func UpdatePrompt(p *model.PromptTemplate) error {
	_, err := DB.Exec(`
		UPDATE prompt_templates SET name=?, template_type=?, content=?, enabled=?, updated_at=?
		WHERE id=?
	`, p.Name, p.TemplateType, p.Content, boolToInt(p.Enabled), nowStr(), p.ID)
	if err != nil {
		return fmt.Errorf("update prompt: %w", err)
	}
	return nil
}

func DeletePrompt(id string) error {
	_, err := DB.Exec(`DELETE FROM prompt_templates WHERE id = ?`, id)
	return err
}

func scanPrompt(row scanner) (*model.PromptTemplate, error) {
	var p model.PromptTemplate
	var enabled int
	err := row.Scan(&p.ID, &p.Name, &p.TemplateType, &p.Content, &enabled, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.Enabled = enabled == 1
	return &p, nil
}
