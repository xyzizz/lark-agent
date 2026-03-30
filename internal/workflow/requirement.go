package workflow

import (
	"context"
	"encoding/json"
	"feishu-agent/internal/executor"
	"feishu-agent/internal/intent"
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

	// Step 1: 拉取相关文档
	step1 := logStep(wfCtx, "fetch_doc", "info", "")
	docContent := ""
	if wfCtx.Route != nil && wfCtx.Route.DocSource != "" {
		if w.feishuClient != nil {
			// 尝试读取飞书文档（docSource 可以是 token 或 URL）
			docToken := extractDocToken(wfCtx.Route.DocSource)
			content, err := w.feishuClient.GetDocContent(ctx, docToken)
			if err != nil {
				docContent = "文档读取失败: " + err.Error()
			} else {
				docContent = content
			}
		}
	}
	finishStep(step1, truncate(docContent, 500), "")

	// Step 2: 生成实现方案
	step2 := logStep(wfCtx, "generate_plan", "llm", wfCtx.Message.Content)
	plan, err := w.generatePlan(ctx, wfCtx, docContent)
	if err != nil {
		finishStep(step2, "", err.Error())
		return "", "", "", fmt.Errorf("generate plan: %w", err)
	}
	finishStep(step2, truncate(planToText(plan), 500), "")

	log.Printf("[requirement] trigger=%s plan generated", wfCtx.TriggerID)

	// 将方案写入触发记录（summary）
	planText := planToText(plan)
	sqlJSON, _ := json.Marshal(plan.SQLSuggestions)

	// Step 3: 如果无仓库，直接返回方案
	if wfCtx.Route == nil || wfCtx.Route.RepoPath == "" {
		step3 := logStep(wfCtx, "no_repo_skip", "info", "")
		finishStep(step3, "无仓库配置，仅输出方案", "")
		return planText, "", string(sqlJSON), nil
	}

	// Step 4: 创建分支
	git := executor.NewGitExecutor(wfCtx.Route.RepoPath)
	branchName := executor.GenerateBranchName("feat", wfCtx.TriggerID)
	step4 := logStep(wfCtx, "create_branch", "git", branchName)
	AuditAction(wfCtx.TriggerID, "git.create_branch", "low", branchName, "")
	gitR, err := git.CreateBranch(ctx, branchName)
	if err != nil {
		finishStep(step4, "", err.Error())
		return planText + "\n\n（分支创建失败，未修改代码）", "", string(sqlJSON), nil
	}
	finishStep(step4, gitR.Output, "")

	// Step 5: Claude Code 生成/修改代码
	step5 := logStep(wfCtx, "generate_code", "llm", "")
	if !wfCtx.DryRun {
		codePrompt := fmt.Sprintf("根据以下需求实现方案生成代码：\n\n%s\n\n原始需求：%s",
			planText, wfCtx.Message.Content)
		req := &model.ClaudeExecRequest{
			RepoPath:   wfCtx.Route.RepoPath,
			TaskType:   "requirement",
			UserPrompt: codePrompt,
			Context:    docContent,
			DryRun:     false,
		}
		result, err := executor.NewClaudeCodeExecutor().Execute(ctx, req)
		if err != nil {
			finishStep(step5, "", err.Error())
		} else {
			finishStep(step5, result.Plan, "")
		}
	} else {
		finishStep(step5, "[dry-run] 跳过代码生成", "")
	}

	// Step 6: 检查
	step6 := logStep(wfCtx, "run_checks", "shell", "go test ./...")
	if !wfCtx.DryRun {
		checkR, err := executor.RunGoTest(ctx, wfCtx.Route.RepoPath)
		if err != nil {
			finishStep(step6, checkR.Stdout, err.Error())
		} else {
			finishStep(step6, checkR.Stdout, "")
		}
	} else {
		finishStep(step6, "[dry-run] 跳过检查", "")
	}

	// Step 7: commit
	mrLink := ""
	if wfCtx.AutoCommit && !wfCtx.DryRun {
		step7 := logStep(wfCtx, "git_commit", "git", "")
		AuditAction(wfCtx.TriggerID, "git.commit", "low", branchName, "")
		git.Add(ctx) //nolint
		cMsg := fmt.Sprintf("feat: %s [agent]", truncate(wfCtx.Message.Content, 60))
		gitR, _ = git.Commit(ctx, cMsg)
		finishStep(step7, gitR.Output, "")

		// Step 8: push
		if wfCtx.AutoPush {
			step8 := logStep(wfCtx, "git_push", "git", branchName)
			AuditAction(wfCtx.TriggerID, "git.push", "medium", branchName, "")
			gitR, pushErr := git.PushSetUpstream(ctx, "origin", branchName)
			finishErrStr := ""
			if pushErr != nil {
				finishErrStr = pushErr.Error()
			}
			finishStep(step8, gitR.Output, finishErrStr)

			// Step 9: 创建 MR
			if wfCtx.AutoMR && finishErrStr == "" {
				step9 := logStep(wfCtx, "create_mr", "git", "")
				AuditAction(wfCtx.TriggerID, "mr.create", "medium", branchName, "")
				mrCreator := &executor.NoopMRCreator{}
				mrReq := &model.MRRequest{
					RepoPath:    wfCtx.Route.RepoPath,
					RemoteURL:   wfCtx.Route.RemoteURL,
					Branch:      branchName,
					BaseBranch:  "main",
					Title:       fmt.Sprintf("[Agent] feat: %s", truncate(wfCtx.Message.Content, 50)),
					Description: planText,
				}
				mrResult, merr := mrCreator.CreateMR(ctx, mrReq)
				if merr == nil {
					mrLink = mrResult.URL
					finishStep(step9, mrLink, "")
				} else {
					finishStep(step9, "", merr.Error())
				}
			}
		}
	}

	finalSummary := planText
	if wfCtx.DryRun {
		finalSummary += "\n\n（dry-run 模式，仅生成方案，未实际修改）"
	}
	return finalSummary, mrLink, string(sqlJSON), nil
}

// ImplementationPlan 实现方案结构
type ImplementationPlan struct {
	Background      string   `json:"background"`
	Objective       string   `json:"objective"`
	Scope           string   `json:"scope"`
	TechnicalPlan   string   `json:"technical_plan"`
	DBChanges       []string `json:"db_changes"`
	SQLSuggestions  []string `json:"sql_suggestions"`
	Risks           []string `json:"risks"`
	TestSuggestions []string `json:"test_suggestions"`
	EstimatedFiles  []string `json:"estimated_files"`
}

// generatePlan 调用 LLM 生成实现方案
func (w *RequirementWorkflow) generatePlan(ctx context.Context, wfCtx *model.WorkflowContext, docContent string) (*ImplementationPlan, error) {
	tpl, _ := store.GetPromptByType("requirement")
	systemPrompt := "你是一个资深技术负责人，正在为新需求编写实现方案，输出必须是 JSON。"
	if tpl != nil {
		systemPrompt = tpl.Content
	}

	projectInfo := ""
	if wfCtx.Route != nil {
		projectInfo = fmt.Sprintf("项目: %s, 仓库: %s", wfCtx.Route.Name, wfCtx.Route.RepoPath)
	}

	userPrompt := fmt.Sprintf(`需求描述：%s

项目信息：%s

相关文档：
%s

请输出以下 JSON 格式的实现方案：
{
  "background": "<背景>",
  "objective": "<目标>",
  "scope": "<范围>",
  "technical_plan": "<技术方案>",
  "db_changes": ["<数据库变更>"],
  "sql_suggestions": ["<建议SQL>"],
  "risks": ["<风险点>"],
  "test_suggestions": ["<测试建议>"],
  "estimated_files": ["<预计修改文件>"]
}`,
		wfCtx.Message.Content, projectInfo, truncate(docContent, 3000))

	raw, err := intent.CallLLMRaw(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		jsonStr = raw
	}
	var plan ImplementationPlan
	if err = json.Unmarshal([]byte(jsonStr), &plan); err != nil {
		return &ImplementationPlan{
			Background:    raw,
			TechnicalPlan: "（解析失败，原始响应见 Background 字段）",
		}, nil
	}
	return &plan, nil
}

// planToText 将方案转换为可读文本（用于飞书消息）
func planToText(plan *ImplementationPlan) string {
	var sb strings.Builder
	sb.WriteString("## 实现方案\n\n")
	if plan.Background != "" {
		sb.WriteString("**背景：** " + plan.Background + "\n\n")
	}
	if plan.Objective != "" {
		sb.WriteString("**目标：** " + plan.Objective + "\n\n")
	}
	if plan.Scope != "" {
		sb.WriteString("**范围：** " + plan.Scope + "\n\n")
	}
	if plan.TechnicalPlan != "" {
		sb.WriteString("**技术方案：**\n" + plan.TechnicalPlan + "\n\n")
	}
	if len(plan.Risks) > 0 {
		sb.WriteString("**风险点：**\n")
		for _, r := range plan.Risks {
			sb.WriteString("- " + r + "\n")
		}
		sb.WriteString("\n")
	}
	if len(plan.TestSuggestions) > 0 {
		sb.WriteString("**测试建议：**\n")
		for _, t := range plan.TestSuggestions {
			sb.WriteString("- " + t + "\n")
		}
	}
	return sb.String()
}

// extractDocToken 从飞书文档 URL 中提取 token
func extractDocToken(docSource string) string {
	// 飞书文档 URL 格式：https://xxx.feishu.cn/docx/{token}
	parts := strings.Split(docSource, "/")
	for i, p := range parts {
		if (p == "docx" || p == "doc" || p == "wiki") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	// 如果不是 URL，直接当 token 用
	return docSource
}
