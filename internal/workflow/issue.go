package workflow

import (
	"context"
	"feishu-agent/internal/executor"
	"feishu-agent/internal/model"
	"fmt"
	"log"
	"strings"

	larkfeishu "feishu-agent/internal/feishu"
)

// IssueWorkflow 问题排查工作流
type IssueWorkflow struct {
	feishuClient *larkfeishu.Client
}

func NewIssueWorkflow(feishuClient *larkfeishu.Client) *IssueWorkflow {
	return &IssueWorkflow{feishuClient: feishuClient}
}

// Run 执行问题排查工作流，返回（摘要, MR链接, SQL建议, error）
func (w *IssueWorkflow) Run(ctx context.Context, wfCtx *model.WorkflowContext) (string, string, string, error) {
	log.Printf("[issue] trigger=%s start", wfCtx.TriggerID)

	// Step 1: 构建 prompt，调用 Claude Code 完成分析和修复
	prompt := w.buildPrompt(wfCtx)
	step1 := logStep(wfCtx, "claude_code_analysis", "llm", truncate(prompt, 500))

	claudeExec := executor.NewClaudeCodeExecutor()
	workDir := primaryRepoPath(wfCtx.Route)

	result, err := claudeExec.Execute(ctx, &model.ClaudeExecRequest{
		RepoPath:   workDir,
		TaskType:   "issue",
		TriggerID:  wfCtx.TriggerID,
		UserPrompt: prompt,
		DryRun:     wfCtx.DryRun,
	})
	if err != nil {
		finishStep(step1, "", err.Error())
		return "", "", "", fmt.Errorf("claude code: %w", err)
	}
	finishStep(step1, truncate(result.Summary, 1000), "")

	summary := result.Summary
	return summary, "", "", nil
}

// buildPrompt 构建问题排查的 Claude Code 提示词
func (w *IssueWorkflow) buildPrompt(wfCtx *model.WorkflowContext) string {
	var sb strings.Builder
	sb.WriteString("你是一个资深后端工程师，请排查以下问题并给出分析和修复方案。\n\n")
	sb.WriteString("## 问题描述\n\n")
	sb.WriteString(wfCtx.Message.Content)
	sb.WriteString("\n\n")

	if wfCtx.Route != nil {
		sb.WriteString("## 项目信息\n\n")
		sb.WriteString(fmt.Sprintf("- 项目: %s\n", wfCtx.Route.Name))
		for _, r := range wfCtx.Route.Repos {
			desc := ""
			if r.Description != "" {
				desc = " — " + r.Description
			}
			sb.WriteString(fmt.Sprintf("- 仓库: %s%s\n", r.Path, desc))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## 要求\n\n")
	sb.WriteString("1. 分析问题根因（配置/数据/代码/其他）\n")
	sb.WriteString("2. 给出详细分析过程\n")
	sb.WriteString("3. 如果是代码问题，给出具体的修复方案和代码变更建议\n")
	sb.WriteString("4. 如果涉及 SQL 操作，给出 SQL 建议\n")
	sb.WriteString("\n注意：只输出分析和建议，不要修改任何文件。\n")

	return sb.String()
}
