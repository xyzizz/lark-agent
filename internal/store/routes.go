package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"feishu-agent/internal/model"
	"fmt"

	"github.com/google/uuid"
)

// ListRoutes 获取所有项目路由（按优先级降序）
func ListRoutes(enabledOnly bool) ([]*model.ProjectRoute, error) {
	query := `SELECT id, name, keywords, repos, doc_source,
		mcp_list, skill_list, priority, enabled, created_at, updated_at
		FROM project_routes`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY priority DESC, created_at ASC`

	rows, err := DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	defer rows.Close()

	var routes []*model.ProjectRoute
	for rows.Next() {
		r, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, r)
	}
	return routes, rows.Err()
}

// GetRoute 按 ID 获取路由
func GetRoute(id string) (*model.ProjectRoute, error) {
	row := DB.QueryRow(`SELECT id, name, keywords, repos, doc_source,
		mcp_list, skill_list, priority, enabled, created_at, updated_at
		FROM project_routes WHERE id = ?`, id)
	r, err := scanRoute(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// CreateRoute 创建路由
func CreateRoute(r *model.ProjectRoute) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	keywords, _ := json.Marshal(r.Keywords)
	reposJSON, _ := json.Marshal(r.Repos)
	mcpList, _ := json.Marshal(r.MCPList)
	skillList, _ := json.Marshal(r.SkillList)
	now := nowStr()
	_, err := DB.Exec(`
		INSERT INTO project_routes
		(id, name, keywords, repos, doc_source, mcp_list, skill_list, priority, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.Name, string(keywords), string(reposJSON), r.DocSource,
		string(mcpList), string(skillList), r.Priority, boolToInt(r.Enabled), now, now)
	if err != nil {
		return fmt.Errorf("create route: %w", err)
	}
	return nil
}

// UpdateRoute 更新路由
func UpdateRoute(r *model.ProjectRoute) error {
	keywords, _ := json.Marshal(r.Keywords)
	reposJSON, _ := json.Marshal(r.Repos)
	mcpList, _ := json.Marshal(r.MCPList)
	skillList, _ := json.Marshal(r.SkillList)
	_, err := DB.Exec(`
		UPDATE project_routes SET
			name=?, keywords=?, repos=?, doc_source=?,
			mcp_list=?, skill_list=?, priority=?, enabled=?, updated_at=?
		WHERE id=?
	`, r.Name, string(keywords), string(reposJSON), r.DocSource,
		string(mcpList), string(skillList), r.Priority, boolToInt(r.Enabled), nowStr(), r.ID)
	if err != nil {
		return fmt.Errorf("update route: %w", err)
	}
	return nil
}

// DeleteRoute 删除路由
func DeleteRoute(id string) error {
	_, err := DB.Exec(`DELETE FROM project_routes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete route: %w", err)
	}
	return nil
}

// scanner 同时支持 *sql.Row 和 *sql.Rows
type scanner interface {
	Scan(dest ...any) error
}

func scanRoute(row scanner) (*model.ProjectRoute, error) {
	var r model.ProjectRoute
	var keywords, reposJSON, mcpList, skillList string
	var enabled int
	err := row.Scan(
		&r.ID, &r.Name, &keywords, &reposJSON, &r.DocSource,
		&mcpList, &skillList, &r.Priority, &enabled, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	r.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(keywords), &r.Keywords)
	_ = json.Unmarshal([]byte(reposJSON), &r.Repos)
	_ = json.Unmarshal([]byte(mcpList), &r.MCPList)
	_ = json.Unmarshal([]byte(skillList), &r.SkillList)
	if r.Keywords == nil {
		r.Keywords = []string{}
	}
	if r.Repos == nil {
		r.Repos = []model.RouteRepo{}
	}
	if r.MCPList == nil {
		r.MCPList = []string{}
	}
	if r.SkillList == nil {
		r.SkillList = []string{}
	}
	return &r, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
