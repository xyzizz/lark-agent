// Package workflow 工作流编排（问题排查 / 需求编写）
package workflow

import (
	"context"
	"feishu-agent/internal/config"
	"encoding/json"
	"feishu-agent/internal/executor"
	"feishu-agent/internal/feishu"
	"feishu-agent/internal/intent"
	"feishu-agent/internal/model"
	"feishu-agent/internal/router"
	"feishu-agent/internal/store"
	"fmt"
	"log"

	"github.com/google/uuid"
)

// Runner 工作流总入口
type Runner struct {
	feishuClient *feishu.Client
	matcher      *router.Matcher
	recognizer   *intent.Recognizer
	issueWF      *IssueWorkflow
	requirementWF *RequirementWorkflow
}

func NewRunner(feishuClient *feishu.Client) *Runner {
	r := &Runner{
		feishuClient:  feishuClient,
		matcher:       router.NewMatcher(),
		recognizer:    intent.NewRecognizer(),
	}
	r.issueWF = NewIssueWorkflow(feishuClient)
	r.requirementWF = NewRequirementWorkflow(feishuClient)
	return r
}

// HandleMessage 处理飞书消息（异步）
func (r *Runner) HandleMessage(msg *model.FeishuMessage) {
	go func() {
		if err := r.run(context.Background(), msg); err != nil {
			log.Printf("[runner] error handling message %s: %v", msg.MessageID, err)
		}
	}()
}

// run 同步执行处理流程（内部）
func (r *Runner) run(ctx context.Context, msg *model.FeishuMessage) error {
	cfg := config.Get()

	// 1. 创建触发记录
	trigger := &model.Trigger{
		ID:         uuid.NewString(),
		RawMessage: msg.Content,
		SenderID:   msg.SenderID,
		SenderName: msg.SenderName,
		ChatID:     msg.ChatID,
		ChatType:   msg.ChatType,
		MessageID:  msg.MessageID,
		Status:     "pending",
	}
	if err := store.CreateTrigger(trigger); err != nil {
		return fmt.Errorf("create trigger: %w", err)
	}
	store.SetTriggerStarted(trigger.ID) //nolint

	wfCtx := &model.WorkflowContext{
		TriggerID:  trigger.ID,
		Message:    msg,
		DryRun:     cfg.Harness.DryRun,
		AutoCommit: cfg.Harness.AutoCommit,
		AutoPush:   cfg.Harness.AutoPush,
		AutoMR:     cfg.Harness.AutoCreateMR,
	}

	// 2. 意图识别
	log.Printf("[runner] trigger=%s recognizing intent for: %s", trigger.ID, truncate(msg.Content, 80))
	r.runnerLogStep(wfCtx, "intent_recognition", "llm", msg.Content)

	intentResult, err := r.recognizer.Recognize(ctx, msg.Content)
	if err != nil {
		r.finishWithError(ctx, trigger, wfCtx, "意图识别失败: "+err.Error())
		return err
	}
	wfCtx.Intent = intentResult
	trigger.Intent = intentResult.Intent
	trigger.Confidence = intentResult.Confidence

	log.Printf("[runner] trigger=%s intent=%s confidence=%.2f", trigger.ID, intentResult.Intent, intentResult.Confidence)

	// 3. 意图过滤
	switch intentResult.Intent {
	case model.IntentIgnore:
		store.UpdateTriggerStatus(trigger.ID, "skipped", "消息已忽略："+intentResult.Summary, "", "", "") //nolint
		return nil
	case model.IntentNeedMoreContext:
		// 回复用户需要更多信息
		r.sendReply(ctx, msg, "收到消息，但信息不足以处理。请提供更多细节："+intentResult.Summary)
		store.UpdateTriggerStatus(trigger.ID, "skipped", "信息不足", "", "", "") //nolint
		return nil
	case model.IntentRiskyAction:
		// 高风险动作，暂停等待确认
		if cfg.Harness.RequireConfirmOnRisky {
			r.sendReply(ctx, msg, fmt.Sprintf("⚠️ 检测到高风险操作（%s），需要人工确认后才能执行。请联系管理员确认。", intentResult.Summary))
			store.UpdateTriggerStatus(trigger.ID, "skipped", "高风险动作，等待人工确认", "", "", "") //nolint
			store.CreateAuditLog(&model.AuditLog{ //nolint
				TriggerID: trigger.ID,
				Action:    "risky_action_blocked",
				RiskLevel: "high",
				Detail:    intentResult.Summary,
				Operator:  "system",
				Result:    "blocked",
			})
			return nil
		}
	}

	// 4. 项目路由
	route, err := r.matcher.Match(msg.Content, intentResult)
	if err != nil {
		r.finishWithError(ctx, trigger, wfCtx, "路由匹配失败: "+err.Error())
		return err
	}
	wfCtx.Route = route
	if route != nil {
		trigger.MatchedProject = route.Name
	}

	// 5. 分发到对应工作流
	var summary, mrLink, sqlSuggestions string
	switch intentResult.Intent {
	case model.IntentIssueTroubleshooting:
		summary, mrLink, sqlSuggestions, err = r.issueWF.Run(ctx, wfCtx)
	case model.IntentRequirementWriting:
		summary, mrLink, sqlSuggestions, err = r.requirementWF.Run(ctx, wfCtx)
	default:
		summary = "未知意图，跳过处理"
	}

	// 6. 更新最终状态
	status := "success"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
		log.Printf("[runner] trigger=%s workflow error: %v", trigger.ID, err)
	}
	store.UpdateTriggerStatus(trigger.ID, status, summary, mrLink, sqlSuggestions, errMsg) //nolint

	// 7. 通过飞书机器人发送结果
	r.sendResult(ctx, msg, trigger.ID, intentResult.Intent, summary, mrLink, sqlSuggestions, err == nil)

	return err
}

// runnerLogStep Runner 专用步骤记录（避免与 issue.go 包级 logStep 冲突）
func (r *Runner) runnerLogStep(wfCtx *model.WorkflowContext, name, stepType, input string) *model.TriggerStep {
	return logStep(wfCtx, name, stepType, input)
}

// finishWithError 整体失败处理
func (r *Runner) finishWithError(ctx context.Context, trigger *model.Trigger, wfCtx *model.WorkflowContext, errMsg string) {
	store.UpdateTriggerStatus(trigger.ID, "failed", "", "", "", errMsg) //nolint
	r.sendReply(ctx, wfCtx.Message, "❌ 处理失败："+errMsg)
}

// sendReply 发送飞书消息回复
func (r *Runner) sendReply(ctx context.Context, msg *model.FeishuMessage, text string) {
	if r.feishuClient == nil {
		log.Printf("[runner] feishu reply (no client): %s", text)
		return
	}
	if err := r.feishuClient.SendTextMessage(ctx, msg.ChatID, text); err != nil {
		log.Printf("[runner] send reply error: %v", err)
	}
}

// sendResult 发送卡片消息结果
func (r *Runner) sendResult(ctx context.Context, msg *model.FeishuMessage, triggerID, intentType, summary, mrLink, sqlSuggestions string, success bool) {
	if r.feishuClient == nil {
		log.Printf("[runner] result (no feishu client): intent=%s summary=%s mr=%s", intentType, truncate(summary, 100), mrLink)
		return
	}

	// 解析 SQL 建议
	var sqls []string
	if sqlSuggestions != "" {
		// sqlSuggestions 是 JSON array 或纯文本
		_ = parseJSONArray(sqlSuggestions, &sqls)
		if len(sqls) == 0 {
			sqls = []string{sqlSuggestions}
		}
	}

	title := "✅ 处理完成"
	if !success {
		title = "❌ 处理失败"
	}

	card := feishu.BuildResultCard(title, intentType, summary, mrLink, sqls, success)
	if err := r.feishuClient.SendCardMessage(ctx, msg.ChatID, card); err != nil {
		// 降级为文本消息
		text := fmt.Sprintf("%s\n意图：%s\n摘要：%s", title, intentType, summary)
		if mrLink != "" {
			text += "\nMR: " + mrLink
		}
		r.feishuClient.SendTextMessage(ctx, msg.ChatID, text) //nolint
	}
}

// ─── 工具函数 ─────────────────────────────────────────────────

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func parseJSONArray(s string, out *[]string) error {
	return json.Unmarshal([]byte(s), out)
}

// AuditAction 记录审计日志
func AuditAction(triggerID, action, riskLevel, detail, result string) {
	store.CreateAuditLog(&model.AuditLog{ //nolint
		TriggerID: triggerID,
		Action:    action,
		RiskLevel: riskLevel,
		Detail:    detail,
		Operator:  "agent",
		Result:    result,
	})
}

// BuildGitExecutor 根据路由配置创建 git 执行器
func BuildGitExecutor(route *model.ProjectRoute) *executor.GitExecutor {
	if route == nil || route.RepoPath == "" {
		return nil
	}
	return executor.NewGitExecutor(route.RepoPath)
}
