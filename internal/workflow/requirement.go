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

// RequirementWorkflow 需求编写工作流
type RequirementWorkflow struct {
	feishuClient *larkfeishu.Client
}

func NewRequirementWorkflow(feishuClient *larkfeishu.Client) *RequirementWorkflow {
	return &RequirementWorkflow{feishuClient: feishuClient}
}

// Run 执行需求编写工作流
func (w *RequirementWorkflow) Run(ctx context.Context, wfCtx *model.WorkflowContext) (string, string, string, error) {
	log.Printf("[requirement] trigger=%s start", wfCtx.TriggerID)

	// Step 1: 拉取相关文档（如有配置）
	docContent := ""
	if wfCtx.Route != nil && wfCtx.Route.DocSource != "" && w.feishuClient != nil {
		step := logStep(wfCtx, "fetch_doc", "info", wfCtx.Route.DocSource)
		docToken := extractDocToken(wfCtx.Route.DocSource)
		content, err := w.feishuClient.GetDocContent(ctx, docToken)
		if err != nil {
			docContent = "文档读取失败: " + err.Error()
			finishStep(step, docContent, err.Error())
		} else {
			docContent = content
			finishStep(step, truncate(docContent, 500), "")
		}
	}

	// Step 2: 构建 prompt，调用 Claude Code 完成方案生成和代码实现
	prompt := w.buildPrompt(wfCtx, docContent)
	step2 := logStep(wfCtx, "claude_code_implement", "llm", truncate(prompt, 500))

	claudeExec := executor.NewClaudeCodeExecutor()
	workDir := primaryRepoPath(wfCtx.Route)

	result, err := claudeExec.Execute(ctx, &model.ClaudeExecRequest{
		RepoPath:   workDir,
		TaskType:   "requirement",
		TriggerID:  wfCtx.TriggerID,
		UserPrompt: prompt,
		DryRun:     wfCtx.DryRun,
	})
	if err != nil {
		finishStep(step2, "", err.Error())
		return "", "", "", fmt.Errorf("claude code: %w", err)
	}
	finishStep(step2, truncate(result.Summary, 1000), "")

	summary := result.Summary
	return summary, "", "", nil
}

// buildPrompt 构建需求编写的 Claude Code 提示词
func (w *RequirementWorkflow) buildPrompt(wfCtx *model.WorkflowContext, docContent string) string {
	var sb strings.Builder
	sb.WriteString("你是一个资深技术负责人，请根据以下需求完成技术方案设计和代码实现。\n\n")
	sb.WriteString("## 需求描述\n\n")
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

	if docContent != "" {
		sb.WriteString("## 相关文档\n\n")
		sb.WriteString(docContent)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## 要求\n\n")
	sb.WriteString("1. 输出技术方案（背景、目标、实现思路、风险点）\n")
	sb.WriteString("2. 给出具体的代码变更建议（涉及哪些文件、怎么改）\n")
	sb.WriteString("3. 如果涉及数据库变更，给出 SQL 建议\n")
	sb.WriteString("\n注意：只输出分析和方案，不要修改任何文件。\n")

	return sb.String()
}

// extractDocToken 从飞书文档 URL 中提取 token
func extractDocToken(docSource string) string {
	parts := strings.Split(docSource, "/")
	for i, p := range parts {
		if (p == "docx" || p == "doc" || p == "wiki") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return docSource
}
