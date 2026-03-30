package executor

import (
	"context"
	"encoding/json"
	"feishu-agent/internal/intent"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"fmt"
	"strings"
)

// ClaudeCodeExecutor 调用 Claude API 执行代码分析与修改任务
type ClaudeCodeExecutor struct{}

func NewClaudeCodeExecutor() *ClaudeCodeExecutor {
	return &ClaudeCodeExecutor{}
}

// Execute 执行代码任务（分析、修改建议）
// 注意：实际的文件修改由 Claude Code CLI 执行，这里只做分析和计划生成
func (e *ClaudeCodeExecutor) Execute(ctx context.Context, req *model.ClaudeExecRequest) (*model.ClaudeExecResult, error) {
	// 1. 先读取仓库状态
	repoInfo, err := getRepoInfo(ctx, req.RepoPath)
	if err != nil {
		repoInfo = "无法读取仓库信息: " + err.Error()
	}

	// 2. 构建提示词
	systemPrompt := req.SystemPrompt
	if systemPrompt == "" {
		if sysTpl, _ := store.GetPromptByType("system"); sysTpl != nil {
			systemPrompt = sysTpl.Content
		} else {
			systemPrompt = "你是一个资深工程师，专注于代码分析和修改。输出必须是 JSON 格式。"
		}
	}

	userPrompt := buildCodeTaskPrompt(req, repoInfo)

	// 3. 调用 LLM
	raw, err := intent.CallLLMRaw(ctx, systemPrompt, userPrompt)
	if err != nil {
		return &model.ClaudeExecResult{
			Success: false,
			Error:   fmt.Sprintf("LLM 调用失败: %v", err),
		}, err
	}

	// 4. 解析结果
	result, err := parseCodeResult(raw)
	if err != nil {
		// 解析失败，仍然返回原始输出
		return &model.ClaudeExecResult{
			Success: true,
			Plan:    raw,
			Summary: "LLM 响应解析失败，原始结果见 Plan 字段",
			Logs:    []string{"parse error: " + err.Error()},
		}, nil
	}

	// 5. dry-run 模式：只返回计划，不执行
	if req.DryRun {
		result.Logs = append(result.Logs, "[dry-run] 计划已生成，实际修改已跳过")
		return result, nil
	}

	// 6. 如果需要实际修改代码，调用 claude-code CLI
	if req.RepoPath != "" && !req.DryRun {
		if err := runClaudeCodeCLI(ctx, req.RepoPath, result.Plan); err != nil {
			result.Logs = append(result.Logs, "claude-code CLI 执行: "+err.Error())
			// 不视为失败，计划已生成
		} else {
			result.Logs = append(result.Logs, "claude-code CLI 执行完成")
		}
	}

	return result, nil
}

// getRepoInfo 获取仓库基本信息（最近提交、文件树等）
func getRepoInfo(ctx context.Context, repoPath string) (string, error) {
	if repoPath == "" {
		return "", nil
	}
	// git log
	logResult, err := RunShell(ctx, repoPath, "git", "log", "--max-count=5", "--oneline")
	if err != nil {
		return "", err
	}
	// 文件树（只看第一层）
	lsResult, _ := RunShell(ctx, repoPath, "ls", "-la")
	return fmt.Sprintf("最近提交:\n%s\n\n目录结构:\n%s", logResult.Stdout, lsResult.Stdout), nil
}

// buildCodeTaskPrompt 构建代码任务提示词
func buildCodeTaskPrompt(req *model.ClaudeExecRequest, repoInfo string) string {
	var sb strings.Builder
	sb.WriteString("任务类型: " + req.TaskType + "\n\n")
	sb.WriteString("任务描述:\n" + req.UserPrompt + "\n\n")
	if repoInfo != "" {
		sb.WriteString("仓库信息:\n" + repoInfo + "\n\n")
	}
	if req.Context != "" {
		sb.WriteString("补充上下文:\n" + req.Context + "\n\n")
	}
	sb.WriteString(`
请输出以下 JSON 格式的执行计划：
{
  "plan": "<详细的修改计划，分步骤描述>",
  "summary": "<一句话摘要>",
  "files_changed": ["<预计修改的文件路径>"],
  "sql_suggestions": ["<涉及的SQL操作建议>"],
  "logs": ["<备注信息>"]
}`)
	return sb.String()
}

// parseCodeResult 解析 LLM 代码任务结果
func parseCodeResult(raw string) (*model.ClaudeExecResult, error) {
	// 提取 JSON
	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		jsonStr = raw
	}

	var result model.ClaudeExecResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("parse code result: %w", err)
	}
	result.Success = true
	return &result, nil
}

// runClaudeCodeCLI 调用 claude-code CLI 执行实际代码修改
// 前提：系统已安装 claude-code CLI
func runClaudeCodeCLI(ctx context.Context, repoPath, plan string) error {
	// 将计划写入临时文件，传给 claude-code
	// claude-code 命令格式：claude --print "<prompt>" 或通过 stdin
	// 这里用 --print 模式（非交互）
	prompt := "请根据以下计划修改代码：\n\n" + plan
	result, err := RunShell(ctx, repoPath, "claude", "--print", prompt)
	if err != nil {
		return fmt.Errorf("claude CLI: %w (stdout: %s)", err, truncateStr(result.Stdout, 200))
	}
	return nil
}

// extractJSON 从文本提取 JSON（重用 parser 逻辑）
func extractJSON(text string) string {
	// 简单实现：找第一个 { 到最后一个 }
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || start >= end {
		return ""
	}
	return text[start : end+1]
}
