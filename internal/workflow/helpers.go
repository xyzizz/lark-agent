package workflow

import (
	"bytes"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"fmt"
	"text/template"

	"github.com/google/uuid"
)

// logStep 记录工作流步骤
func logStep(wfCtx *model.WorkflowContext, name, stepType, input string) *model.TriggerStep {
	wfCtx.StepIndex++
	step := &model.TriggerStep{
		ID:        uuid.NewString(),
		TriggerID: wfCtx.TriggerID,
		StepIndex: wfCtx.StepIndex,
		StepName:  name,
		StepType:  stepType,
		InputData: truncate(input, 2000),
		Status:    "running",
	}
	store.CreateTriggerStep(step) //nolint
	wfCtx.Steps = append(wfCtx.Steps, step)
	return step
}

// finishStep 完成步骤记录
func finishStep(step *model.TriggerStep, output, errMsg string) {
	status := "success"
	if errMsg != "" {
		status = "failed"
	}
	store.UpdateTriggerStep(step.ID, status, truncate(output, 5000), errMsg) //nolint
}

// AuditAction 记录审计日志
func AuditAction(triggerID, action, riskLevel, detail, result string) {
	store.CreateAuditLog(&model.AuditLog{
		TriggerID: triggerID,
		Action:    action,
		RiskLevel: riskLevel,
		Detail:    detail,
		Operator:  "system",
		Result:    result,
	}) //nolint
}

// truncate 截断字符串（rune 安全）
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// renderPromptTemplate 渲染提示词模板（Go text/template）
func renderPromptTemplate(tmplStr string, data map[string]string) (string, error) {
	t, err := template.New("").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err = t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// buildReposText 将路由的仓库列表格式化为文本
func buildReposText(route *model.ProjectRoute) string {
	if route == nil || len(route.Repos) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for _, r := range route.Repos {
		desc := ""
		if r.Description != "" {
			desc = " — " + r.Description
		}
		buf.WriteString(fmt.Sprintf("- 仓库: %s%s\n", r.Path, desc))
	}
	return buf.String()
}

// ─── 多仓库工具函数 ─────────────────────────────────────────────

// primaryRepoPath 返回第一个仓库路径（Claude Code 工作目录）
func primaryRepoPath(route *model.ProjectRoute) string {
	if route == nil || len(route.Repos) == 0 {
		return ""
	}
	return route.Repos[0].Path
}

