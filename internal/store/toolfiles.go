package store

import (
	"encoding/json"
	"feishu-agent/internal/model"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// toolsBaseDir 工具文件根目录
const toolsBaseDir = "./tools"

// PromptInfo 提示词文件信息
type PromptInfo struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ListPrompts 列出 tools/prompt/ 下所有 .md 文件（跳过下划线开头）
func ListPrompts() ([]PromptInfo, error) {
	dir := filepath.Join(toolsBaseDir, "prompt")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var prompts []PromptInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		content, err := LoadPrompt(name)
		if err != nil {
			continue
		}
		prompts = append(prompts, PromptInfo{Name: name, Content: content})
	}
	return prompts, nil
}

// LoadPrompt 从 tools/prompt/{name}.md 读取提示词内容
func LoadPrompt(name string) (string, error) {
	path := filepath.Join(toolsBaseDir, "prompt", name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("load prompt %s: %w", name, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// SavePrompt 保存提示词到 tools/prompt/{name}.md
func SavePrompt(name, content string) error {
	dir := filepath.Join(toolsBaseDir, "prompt")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir prompt: %w", err)
	}
	path := filepath.Join(dir, name+".md")
	return os.WriteFile(path, []byte(content+"\n"), 0644)
}

// toolFileJSON MCP/Shell 的 JSON 文件格式
type toolFileJSON struct {
	Description  string          `json:"description"`
	Command      string          `json:"command"`
	ArgsTemplate json.RawMessage `json:"args_template,omitempty"`
	Enabled      bool            `json:"enabled"`
}

// LoadToolsFromFiles 扫描 tools/ 目录，返回所有文件定义的工具
// mcp/ shell/ → .json 文件；skill/ → .md 文件（Markdown + frontmatter）
func LoadToolsFromFiles() ([]*model.ToolConfig, error) {
	var tools []*model.ToolConfig

	for _, toolType := range []string{"mcp", "skill"} {
		dir := filepath.Join(toolsBaseDir, toolType)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read dir %s: %w", dir, err)
		}

		ext := ".json"
		if toolType == "skill" {
			ext = ".md"
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ext) {
				continue
			}
			if strings.HasPrefix(entry.Name(), "_") {
				continue
			}

			name := strings.TrimSuffix(entry.Name(), ext)
			path := filepath.Join(dir, entry.Name())

			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("[toolfiles] read %s: %v", path, err)
				continue
			}

			var tc *model.ToolConfig
			if toolType == "skill" {
				tc = parseSkillMarkdown(name, string(data))
			} else {
				tc = parseToolJSON(name, toolType, data)
			}
			if tc != nil {
				tools = append(tools, tc)
			}
		}
	}

	return tools, nil
}

// parseToolJSON 解析 MCP/Shell 的 JSON 配置
func parseToolJSON(name, toolType string, data []byte) *model.ToolConfig {
	var fd toolFileJSON
	if err := json.Unmarshal(data, &fd); err != nil {
		log.Printf("[toolfiles] parse %s.json: %v", name, err)
		return nil
	}
	argsStr := "{}"
	if len(fd.ArgsTemplate) > 0 {
		argsStr = string(fd.ArgsTemplate)
	}
	return &model.ToolConfig{
		Name:         name,
		ToolType:     toolType,
		Description:  fd.Description,
		Command:      fd.Command,
		ArgsTemplate: argsStr,
		Enabled:      fd.Enabled,
	}
}

// parseSkillMarkdown 解析 Skill 的 Markdown 文件（YAML frontmatter + body）
// 格式：
//
//	---
//	description: "..."
//	command: "skill-id"
//	enabled: true
//	---
//	Markdown body（可选，存入 args_template 供后续使用）
func parseSkillMarkdown(name, content string) *model.ToolConfig {
	tc := &model.ToolConfig{
		Name:     name,
		ToolType: "skill",
		Enabled:  true,
	}

	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		// 无 frontmatter，整个文件当描述
		tc.Description = content
		tc.Command = name
		return tc
	}

	// 分离 frontmatter 和 body
	rest := content[3:] // 跳过开头 ---
	endIdx := strings.Index(rest, "\n---")
	if endIdx < 0 {
		tc.Description = content
		tc.Command = name
		return tc
	}

	frontmatter := rest[:endIdx]
	body := strings.TrimSpace(rest[endIdx+4:]) // 跳过 \n---

	// 解析 frontmatter（简单 KV，不引入 yaml 依赖）
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, "\"'") // 去掉引号

		switch k {
		case "description":
			tc.Description = v
		case "command":
			tc.Command = v
		case "enabled":
			tc.Enabled = v != "false"
		}
	}

	if tc.Command == "" {
		tc.Command = name
	}

	// body 存入 ArgsTemplate，作为 skill 的详细说明/提示词
	if body != "" {
		tc.ArgsTemplate = body
	}

	return tc
}

// SyncToolsFromFiles 将文件中的工具同步到 DB（以文件为主，name 去重）
func SyncToolsFromFiles() error {
	fileTools, err := LoadToolsFromFiles()
	if err != nil {
		return err
	}

	for _, ft := range fileTools {
		existing, err := GetToolByName(ft.Name)
		if err != nil {
			log.Printf("[toolfiles] check existing %s: %v", ft.Name, err)
			continue
		}
		if existing == nil {
			if err := CreateTool(ft); err != nil {
				log.Printf("[toolfiles] create %s: %v", ft.Name, err)
			} else {
				log.Printf("[toolfiles] loaded: %s (%s)", ft.Name, ft.ToolType)
			}
		}
	}
	return nil
}

// SaveToolFile 将工具保存为文件（MCP/Shell → JSON，Skill → Markdown）
func SaveToolFile(t *model.ToolConfig) error {
	dir := filepath.Join(toolsBaseDir, t.ToolType)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	if t.ToolType == "skill" {
		return saveSkillMarkdown(t)
	}
	return saveToolJSON(t)
}

func saveToolJSON(t *model.ToolConfig) error {
	argsRaw := json.RawMessage("{}")
	if t.ArgsTemplate != "" {
		if json.Valid([]byte(t.ArgsTemplate)) {
			argsRaw = json.RawMessage(t.ArgsTemplate)
		} else {
			quoted, _ := json.Marshal(t.ArgsTemplate)
			argsRaw = quoted
		}
	}

	fd := toolFileJSON{
		Description:  t.Description,
		Command:      t.Command,
		ArgsTemplate: argsRaw,
		Enabled:      t.Enabled,
	}

	data, err := json.MarshalIndent(fd, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(toolsBaseDir, t.ToolType, t.Name+".json")
	return os.WriteFile(path, data, 0644)
}

func saveSkillMarkdown(t *model.ToolConfig) error {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("description: \"%s\"\n", t.Description))
	sb.WriteString(fmt.Sprintf("command: \"%s\"\n", t.Command))
	sb.WriteString(fmt.Sprintf("enabled: %t\n", t.Enabled))
	sb.WriteString("---\n")

	// ArgsTemplate 作为 skill body（提示词/说明）
	if t.ArgsTemplate != "" && t.ArgsTemplate != "{}" {
		sb.WriteString("\n")
		sb.WriteString(t.ArgsTemplate)
		sb.WriteString("\n")
	}

	path := filepath.Join(toolsBaseDir, "skill", t.Name+".md")
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// DeleteToolFile 删除工具文件
func DeleteToolFile(toolType, name string) error {
	ext := ".json"
	if toolType == "skill" {
		ext = ".md"
	}
	path := filepath.Join(toolsBaseDir, toolType, name+ext)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
