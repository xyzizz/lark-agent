package store

import (
	"feishu-agent/internal/model"
	"fmt"
)

// GetAllSettings 获取所有配置项（返回 map）
func GetAllSettings() (map[string]string, error) {
	rows, err := DB.Query(`SELECT key, value FROM app_settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("query settings: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var s model.AppSetting
		if err = rows.Scan(&s.Key, &s.Value); err != nil {
			return nil, err
		}
		result[s.Key] = s.Value
	}
	return result, rows.Err()
}

// GetSetting 获取单个配置值
func GetSetting(key string) (string, bool, error) {
	var value string
	err := DB.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value)
	if err != nil {
		// database/sql: no rows
		if err.Error() == "sql: no rows in result set" {
			return "", false, nil
		}
		return "", false, fmt.Errorf("get setting %s: %w", key, err)
	}
	return value, true, nil
}

// SetSetting 写入或更新单个配置
func SetSetting(key, value string) error {
	_, err := DB.Exec(`
		INSERT INTO app_settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, nowStr())
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}

// SetSettings 批量写入配置
func SetSettings(kvs map[string]string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint

	stmt, err := tx.Prepare(`
		INSERT INTO app_settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := nowStr()
	for k, v := range kvs {
		if _, err = stmt.Exec(k, v, now); err != nil {
			return fmt.Errorf("set setting %s: %w", k, err)
		}
	}
	return tx.Commit()
}
