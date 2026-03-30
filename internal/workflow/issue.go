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

	"github.com/google/uuid"

	larkfeishu "feishu-agent/internal/feishu"
)

// IssueWorkflow 问题排查工作流
type IssueWorkflow struct {
	feishuClient *larkfeishu.Client
	claudeExec   *executor.ClaudeCodeExecutor
}

func NewIssueWorkflow(feishuClient *larkfeishu.Client) *IssueWorkflow {
	return &IssueWorkflow{
		feishuClient: feishuClient,
		claudeExec:   executor.NewClaudeCodeExecutor(),
	}
}

// Run 执行问题排查工作流，返回（摘要, MR链接, SQL建议, error）
func (w *IssueWorkflow) Run(ctx context.Context, wfCtx *model.WorkflowContext) (string, string, string, error) {
	log.Printf("[issue] trigger=%s start", wfCtx.TriggerID)

	// Step 1: 查询数据库 / 调用 MCP 工具
	step1 := logStep(wfCtx, "query_db_and_tools", "tool_call", wfCtx.Message.Content)
	queryResults, err := w.queryTools(ctx, wfCtx)
	if err != nil {
		finishStep(step1, "", err.Error())
		// 查询失败不中断，继续分析
		queryResults = "工具查询失败: " + err.Error()
	} else {
		finishStep(step1, queryResults, "")
	}

	// Step 2: LLM 排查分析
	step2 := logStep(wfCtx, "issue_analysis", "llm", queryResults)
	analysis, err := w.analyze(ctx, wfCtx, queryResults)
	if err != nil {
		finishStep(step2, "", err.Error())
		return "", "", "", fmt.Errorf("analysis failed: %w", err)
	}
	finishStep(step2, fmt.Sprintf("root_cause_type=%s", analysis.RootCauseType), "")

	log.Printf("[issue] trigger=%s root_cause=%s", wfCtx.TriggerID, analysis.RootCauseType)

	// Step 3: 根据根因类型决定下一步
	sqlJSON, _ := json.Marshal(analysis.SQLSuggestions)
	summary := fmt.Sprintf("【%s】%s\n\n修复建议：%s",
		causeTypeLabel(analysis.RootCauseType), analysis.Analysis, analysis.FixSuggestion)

	if analysis.RootCauseType != "code" {
		// 非代码问题：直接发送分析结论
		step3 := logStep(wfCtx, "send_non_code_result", "info", summary)
		finishStep(step3, "直接返回分析结论（非代码问题）", "")
		return summary, "", string(sqlJSON), nil
	}

	// Step 4: 代码问题 -> 创建分支、修改代码、提交
	return w.handleCodeFix(ctx, wfCtx, analysis, summary, string(sqlJSON))
}

// queryTools 查询 MCP 工具获取上下文信息
func (w *IssueWorkflow) queryTools(ctx context.Context, wfCtx *model.WorkflowContext) (string, error) {
	if wfCtx.Route == nil || len(wfCtx.Route.MCPList) == 0 {
		return "无配置 MCP 工具，跳过查询", nil
	}

	var results []string
	for _, mcpName := range wfCtx.Route.MCPList {
		resp, err := executor.CallMCP(ctx, mcpName, "query", map[string]any{
			"message": wfCtx.Message.Content,
			"intent":  wfCtx.Intent,
		})
		if err != nil {
			results = append(results, fmt.Sprintf("[%s] error: %v", mcpName, err))
			continue
		}
		data, _ := json.Marshal(resp.Data)
		results = append(results, fmt.Sprintf("[%s] %s", mcpName, string(data)))
	}
	return strings.Join(results, "\n\n"), nil
}

// IssueAnalysis LLM 排查结论
type IssueAnalysis struct {
	RootCauseType  string   `json:"root_cause_type"` // config | data | code | unknown
	Analysis       string   `json:"analysis"`
	FixSuggestion  string   `json:"fix_suggestion"`
	SQLSuggestions []string `json:"sql_suggestions"`
	Confidence     float64  `json:"confidence"`
}

// analyze 调用 LLM 进行排查分析
func (w *IssueWorkflow) analyze(ctx context.Context, wfCtx *model.WorkflowContext, queryResults string) (*IssueAnalysis, error) {
	// 读取排查提示词
	tpl, _ := store.GetPromptByType("issue")
	var systemPrompt string
	if tpl != nil {
		systemPrompt = tpl.Content
	} else {
		systemPrompt = "你是一个资深后端工程师，正在排查线上问题，输出必须是 JSON。"
	}

	projectInfo := ""
	if wfCtx.Route != nil {
		projectInfo = fmt.Sprintf("项目: %s, 仓库: %s", wfCtx.Route.Name, wfCtx.Route.RepoPath)
	}

	userPrompt := fmt.Sprintf(`消息内容：%s

项目信息：%s

工具查询结果：
%s

请输出以下 JSON 格式：
{
  "root_cause_type": "<config|data|code|unknown>",
  "analysis": "<详细分析>",
  "fix_suggestion": "<修复建议>",
  "sql_suggestions": ["<SQL1>"],
  "confidence": <0.0-1.0>
}`,
		wfCtx.Message.Content, projectInfo, queryResults)

	raw, err := intent.CallLLMRaw(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	// 解析
	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		jsonStr = raw
	}
	var analysis IssueAnalysis
	if err = json.Unmarshal([]byte(jsonStr), &analysis); err != nil {
		// 兜底
		return &IssueAnalysis{
			RootCauseType: "unknown",
			Analysis:      raw,
			FixSuggestion: "请人工分析",
			Confidence:    0.3,
		}, nil
	}
	return &analysis, nil
}

// handleCodeFix 代码问题处理：创建分支、生成计划、修改代码、提交、推送、创建 MR
func (w *IssueWorkflow) handleCodeFix(ctx context.Context, wfCtx *model.WorkflowContext, analysis *IssueAnalysis, summary, sqlJSON string) (string, string, string, error) {
	if wfCtx.Route == nil || wfCtx.Route.RepoPath == "" {
		return summary + "\n\n（无仓库路径，跳过代码修改）", "", sqlJSON, nil
	}

	git := executor.NewGitExecutor(wfCtx.Route.RepoPath)
	branchName := executor.GenerateBranchName("fix", wfCtx.TriggerID)

	// Step 4: 创建分支
	step4 := logStep(wfCtx, "create_branch", "git", branchName)
	AuditAction(wfCtx.TriggerID, "git.create_branch", "low", branchName, "")
	gitR, err := git.CreateBranch(ctx, branchName)
	if err != nil {
		finishStep(step4, gitR.Output, err.Error())
		return summary, "", sqlJSON, fmt.Errorf("create branch: %w", err)
	}
	finishStep(step4, gitR.Output, "")

	// Step 5: 生成修复计划并调用 Claude Code 修改
	step5 := logStep(wfCtx, "code_fix", "llm", analysis.FixSuggestion)
	if !wfCtx.DryRun {
		req := &model.ClaudeExecRequest{
			RepoPath: wfCtx.Route.RepoPath,
			TaskType: "issue",
			UserPrompt: fmt.Sprintf("根据以下排查结论修复代码：\n%s\n\n修复建议：%s",
				analysis.Analysis, analysis.FixSuggestion),
			DryRun: false,
		}
		result, err := executor.NewClaudeCodeExecutor().Execute(ctx, req)
		if err != nil {
			finishStep(step5, "", err.Error())
			// 不中断，继续提交
		} else {
			finishStep(step5, result.Plan, "")
		}
	} else {
		finishStep(step5, "[dry-run] 跳过实际代码修改", "")
	}

	// Step 6: 执行检查
	step6 := logStep(wfCtx, "run_checks", "shell", "go test ./...")
	if !wfCtx.DryRun {
		checkR, err := executor.RunGoTest(ctx, wfCtx.Route.RepoPath)
		if err != nil {
			log.Printf("[issue] go test failed: %v", err)
			finishStep(step6, checkR.Stdout+checkR.Stderr, err.Error())
			// 测试失败不中断流程，但记录
		} else {
			finishStep(step6, checkR.Stdout, "")
		}
	} else {
		finishStep(step6, "[dry-run] 跳过检查", "")
	}

	// Step 7: git add + commit
	mrLink := ""
	if wfCtx.AutoCommit && !wfCtx.DryRun {
		step7 := logStep(wfCtx, "git_commit", "git", "")
		AuditAction(wfCtx.TriggerID, "git.commit", "low", branchName, "")
		git.Add(ctx)                                                    //nolint
		cMsg := fmt.Sprintf("fix: %s [agent]", truncate(wfCtx.Message.Content, 60))
		gitR, err = git.Commit(ctx, cMsg)
		if err != nil {
			finishStep(step7, gitR.Output, err.Error())
		} else {
			finishStep(step7, gitR.Output, "")
		}

		// Step 8: git push
		if wfCtx.AutoPush {
			step8 := logStep(wfCtx, "git_push", "git", branchName)
			AuditAction(wfCtx.TriggerID, "git.push", "medium", branchName, "")
			gitR, err = git.PushSetUpstream(ctx, "origin", branchName)
			finishStatus := ""
			if err != nil {
				finishStatus = err.Error()
			}
			finishStep(step8, gitR.Output, finishStatus)

			// Step 9: 创建 MR
			if wfCtx.AutoMR && err == nil {
				step9 := logStep(wfCtx, "create_mr", "git", "")
				AuditAction(wfCtx.TriggerID, "mr.create", "medium", branchName, "")
				mrCreator := &executor.NoopMRCreator{}
				if wfCtx.Route.RemoteURL != "" {
					mrCreator = &executor.NoopMRCreator{} // 这里按实际平台替换
				}
				mrReq := &model.MRRequest{
					RepoPath:    wfCtx.Route.RepoPath,
					RemoteURL:   wfCtx.Route.RemoteURL,
					Branch:      branchName,
					BaseBranch:  "main",
					Title:       fmt.Sprintf("[Agent] fix: %s", truncate(wfCtx.Message.Content, 50)),
					Description: summary,
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

	summary = "【代码问题】" + summary
	if wfCtx.DryRun {
		summary += "\n\n（dry-run 模式，仅生成计划，未实际修改）"
	}
	return summary, mrLink, sqlJSON, nil
}

func causeTypeLabel(t string) string {
	switch t {
	case "config":
		return "配置问题"
	case "data":
		return "数据问题"
	case "code":
		return "代码问题"
	default:
		return "暂时无法确认"
	}
}

// logStep 和 finishStep 是包级别的工具，复用 runner 中的 store 调用
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

func finishStep(step *model.TriggerStep, output, errMsg string) {
	status := "success"
	if errMsg != "" {
		status = "failed"
	}
	store.UpdateTriggerStep(step.ID, status, truncate(output, 5000), errMsg) //nolint
}

func extractJSON(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || start >= end {
		return ""
	}
	return text[start : end+1]
}
