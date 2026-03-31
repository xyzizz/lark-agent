// Package workflow 工作流编排（问题排查 / 需求编写）
package workflow

import (
	"context"
	"encoding/json"
	"feishu-agent/internal/config"
	"feishu-agent/internal/executor"
	"feishu-agent/internal/feishu"
	"feishu-agent/internal/intent"
	"feishu-agent/internal/model"
	"feishu-agent/internal/router"
	"feishu-agent/internal/store"
	"fmt"
	"log"
	"strings"
	"time"

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
func (r *Runner) run(ctx context.Context, msg *model.FeishuMessage) (retErr error) {
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

	// 10 分钟超时 + 手动取消支持
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	executor.RegisterJob(trigger.ID, cancel)
	defer executor.UnregisterJob(trigger.ID)

	// 安全网：确保 trigger 状态一定会被更新，防止卡在 running
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[runner] trigger=%s panic: %v", trigger.ID, r)
			store.UpdateTriggerStatus(trigger.ID, "failed", "", "", "", fmt.Sprintf("panic: %v", r)) //nolint
		}
		// 检查是否仍是 running 状态
		if t, _ := store.GetTrigger(trigger.ID); t != nil && (t.Status == "pending" || t.Status == "running") {
			errMsg := "执行异常中断"
			if ctx.Err() == context.DeadlineExceeded {
				errMsg = "执行超时（10分钟）"
			} else if ctx.Err() == context.Canceled {
				errMsg = "已手动取消"
			}
			store.UpdateTriggerStatus(trigger.ID, "cancelled", "", "", "", errMsg) //nolint
		}
	}()

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
	step := logStep(wfCtx, "intent_recognition", "llm", msg.Content)

	intentResult, err := r.recognizer.Recognize(ctx, msg.Content)
	if err != nil {
		finishStep(step, "", err.Error())
		r.finishWithError(ctx, trigger, wfCtx, "意图识别失败: "+err.Error())
		return err
	}
	finishStep(step, fmt.Sprintf("intent=%s confidence=%.2f", intentResult.Intent, intentResult.Confidence), "")
	wfCtx.Intent = intentResult
	trigger.Intent = intentResult.Intent
	trigger.Confidence = intentResult.Confidence
	store.UpdateTriggerIntent(trigger.ID, intentResult.Intent, intentResult.Confidence, "") //nolint

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
		store.UpdateTriggerIntent(trigger.ID, trigger.Intent, trigger.Confidence, route.Name) //nolint
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
	if !r.isSendAllowed(msg) {
		log.Printf("[runner] send blocked: chat_id=%s sender=%s not in whitelist", msg.ChatID, msg.SenderID)
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
	if !r.isSendAllowed(msg) {
		log.Printf("[runner] send result blocked: chat_id=%s sender=%s not in whitelist", msg.ChatID, msg.SenderID)
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

// isSendAllowed 检查是否允许向该消息的来源发送回复
// 如果配置了白名单，只允许向白名单中的用户发送消息
func (r *Runner) isSendAllowed(msg *model.FeishuMessage) bool {
	settings, _ := store.GetAllSettings()
	allowed := settings["feishu_allowed_senders"]
	if allowed == "" {
		return true // 未配置白名单，允许所有
	}
	// 检查发送者是否在白名单中
	if msg.SenderID == "" {
		return false
	}
	for _, id := range splitAndTrim(allowed) {
		if id == msg.SenderID {
			return true
		}
	}
	return false
}

// splitAndTrim 按逗号分隔并去空格
func splitAndTrim(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		if v := strings.TrimSpace(part); v != "" {
			result = append(result, v)
		}
	}
	return result
}

// ─── 工具函数 ─────────────────────────────────────────────────

func parseJSONArray(s string, out *[]string) error {
	return json.Unmarshal([]byte(s), out)
}
