package workflow

import (
	"context"
	"feishu-agent/internal/executor"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"fmt"
	"log"

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

// buildPrompt 构建问题排查的 Claude Code 提示词（从文件加载模板）
func (w *IssueWorkflow) buildPrompt(wfCtx *model.WorkflowContext) string {
	data := map[string]string{
		"Message": wfCtx.Message.Content,
	}
	if wfCtx.Route != nil {
		data["ProjectName"] = wfCtx.Route.Name
		data["Repos"] = buildReposText(wfCtx.Route)
	}

	content, err := store.LoadPrompt("issue")
	if err != nil {
		log.Printf("[issue] load prompt file failed: %v, using raw message", err)
		return wfCtx.Message.Content
	}
	rendered, err := renderPromptTemplate(content, data)
	if err != nil {
		log.Printf("[issue] render template failed: %v, using raw message", err)
		return wfCtx.Message.Content
	}
	return rendered
}
