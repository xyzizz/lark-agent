package workflow

import (
	"context"
	"feishu-agent/internal/executor"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
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

// buildPrompt 构建需求编写的 Claude Code 提示词（从文件加载模板）
func (w *RequirementWorkflow) buildPrompt(wfCtx *model.WorkflowContext, docContent string) string {
	data := map[string]string{
		"Message":    wfCtx.Message.Content,
		"DocContent": docContent,
	}
	if wfCtx.Route != nil {
		data["ProjectName"] = wfCtx.Route.Name
		data["Repos"] = buildReposText(wfCtx.Route)
	}

	content, err := store.LoadPrompt("requirement")
	if err != nil {
		log.Printf("[requirement] load prompt file failed: %v, using raw message", err)
		return wfCtx.Message.Content
	}
	rendered, err := renderPromptTemplate(content, data)
	if err != nil {
		log.Printf("[requirement] render template failed: %v, using raw message", err)
		return wfCtx.Message.Content
	}
	return rendered
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
