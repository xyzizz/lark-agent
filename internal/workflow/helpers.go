package workflow

import (
	"context"
	"feishu-agent/internal/executor"
	"feishu-agent/internal/model"
	"feishu-agent/internal/store"
	"fmt"

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

// truncate 截断字符串
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ─── 多仓库工具函数 ─────────────────────────────────────────────

// primaryRepoPath 返回第一个仓库路径（Claude Code 工作目录）
func primaryRepoPath(route *model.ProjectRoute) string {
	if route == nil || len(route.Repos) == 0 {
		return ""
	}
	return route.Repos[0].Path
}

// repoPaths 返回路由的所有仓库路径
func repoPaths(route *model.ProjectRoute) []string {
	if route == nil {
		return nil
	}
	var paths []string
	for _, r := range route.Repos {
		if r.Path != "" {
			paths = append(paths, r.Path)
		}
	}
	return paths
}

// handleGitOps 对单个仓库执行 git 操作（创建分支、提交、推送、MR）
func handleGitOps(ctx context.Context, wfCtx *model.WorkflowContext, repoPath, prefix, summary string) string {
	git := executor.NewGitExecutor(repoPath)
	branchName := executor.GenerateBranchName(prefix, wfCtx.TriggerID)

	// 检查是否有改动
	statusR, _ := git.Status(ctx)
	if statusR != nil && statusR.Output == "" {
		return "" // 无改动，跳过
	}

	// 创建分支
	step := logStep(wfCtx, "create_branch", "git", fmt.Sprintf("%s @ %s", branchName, repoPath))
	AuditAction(wfCtx.TriggerID, "git.create_branch", "low", branchName, "")
	gitR, err := git.CreateBranch(ctx, branchName)
	if err != nil {
		finishStep(step, gitR.Output, err.Error())
		return ""
	}
	finishStep(step, gitR.Output, "")

	mrLink := ""
	if wfCtx.AutoCommit {
		step = logStep(wfCtx, "git_commit", "git", repoPath)
		AuditAction(wfCtx.TriggerID, "git.commit", "low", branchName, "")
		git.Add(ctx) //nolint
		cMsg := fmt.Sprintf("%s: %s [agent]", prefix, truncate(wfCtx.Message.Content, 60))
		gitR, err = git.Commit(ctx, cMsg)
		if err != nil {
			finishStep(step, gitR.Output, err.Error())
			return ""
		}
		finishStep(step, gitR.Output, "")

		if wfCtx.AutoPush {
			step = logStep(wfCtx, "git_push", "git", branchName)
			AuditAction(wfCtx.TriggerID, "git.push", "medium", branchName, "")
			gitR, err = git.PushSetUpstream(ctx, "origin", branchName)
			if err != nil {
				finishStep(step, gitR.Output, err.Error())
				return ""
			}
			finishStep(step, gitR.Output, "")

			if wfCtx.AutoMR {
				step = logStep(wfCtx, "create_mr", "git", repoPath)
				AuditAction(wfCtx.TriggerID, "mr.create", "medium", branchName, "")
				mrCreator := &executor.NoopMRCreator{}
				mrReq := &model.MRRequest{
					RepoPath:   repoPath,
					Branch:     branchName,
					BaseBranch: "main",
					Title:      fmt.Sprintf("[Agent] %s: %s", prefix, truncate(wfCtx.Message.Content, 50)),
					Description: summary,
				}
				mrResult, merr := mrCreator.CreateMR(ctx, mrReq)
				if merr == nil {
					mrLink = mrResult.URL
					finishStep(step, mrLink, "")
				} else {
					finishStep(step, "", merr.Error())
				}
			}
		}
	}
	return mrLink
}
