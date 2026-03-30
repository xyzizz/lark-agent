// Package model 定义所有核心数据结构
package model

import "time"

// ─── 数据库实体 ──────────────────────────────────────────────

// AppSetting 全局配置项（KV 存储）
type AppSetting struct {
	Key       string    `json:"key" db:"key"`
	Value     string    `json:"value" db:"value"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// ProjectRoute 项目路由配置
type ProjectRoute struct {
	ID        string    `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Keywords  []string  `json:"keywords"`  // 序列化为 JSON 存储
	RepoPath  string    `json:"repo_path" db:"repo_path"`
	RemoteURL string    `json:"remote_url" db:"remote_url"`
	DocSource string    `json:"doc_source" db:"doc_source"`
	MCPList   []string  `json:"mcp_list"`   // 序列化为 JSON 存储
	SkillList []string  `json:"skill_list"` // 序列化为 JSON 存储
	Priority  int       `json:"priority" db:"priority"`
	Enabled   bool      `json:"enabled" db:"enabled"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// ToolConfig MCP / shell / skill 工具配置
type ToolConfig struct {
	ID           string    `json:"id" db:"id"`
	Name         string    `json:"name" db:"name"`
	ToolType     string    `json:"tool_type" db:"tool_type"` // mcp | skill | shell
	Description  string    `json:"description" db:"description"`
	Command      string    `json:"command" db:"command"`
	ArgsTemplate string    `json:"args_template" db:"args_template"` // JSON 字符串
	Enabled      bool      `json:"enabled" db:"enabled"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// PromptTemplate 提示词模板
type PromptTemplate struct {
	ID           string    `json:"id" db:"id"`
	Name         string    `json:"name" db:"name"`
	TemplateType string    `json:"template_type" db:"template_type"` // system | intent | issue | requirement
	Content      string    `json:"content" db:"content"`
	Enabled      bool      `json:"enabled" db:"enabled"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// Trigger 每次消息触发的主记录
type Trigger struct {
	ID             string     `json:"id" db:"id"`
	RawMessage     string     `json:"raw_message" db:"raw_message"`
	SenderID       string     `json:"sender_id" db:"sender_id"`
	SenderName     string     `json:"sender_name" db:"sender_name"`
	ChatID         string     `json:"chat_id" db:"chat_id"`
	ChatType       string     `json:"chat_type" db:"chat_type"`     // group | p2p
	MessageID      string     `json:"message_id" db:"message_id"`   // 飞书消息 ID，用于去重
	Intent         string     `json:"intent" db:"intent"`           // 意图类型
	Confidence     float64    `json:"confidence" db:"confidence"`
	MatchedProject string     `json:"matched_project" db:"matched_project"`
	Status         string     `json:"status" db:"status"`           // pending | running | success | failed | skipped
	ResultSummary  string     `json:"result_summary" db:"result_summary"`
	MRLink         string     `json:"mr_link" db:"mr_link"`
	SQLSuggestions string     `json:"sql_suggestions" db:"sql_suggestions"` // JSON array
	ErrorMsg       string     `json:"error_msg" db:"error_msg"`
	StartedAt      *time.Time `json:"started_at" db:"started_at"`
	FinishedAt     *time.Time `json:"finished_at" db:"finished_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	// 关联字段（非 db 字段）
	Steps []*TriggerStep `json:"steps,omitempty" db:"-"`
}

// TriggerStep 执行步骤明细
type TriggerStep struct {
	ID         string     `json:"id" db:"id"`
	TriggerID  string     `json:"trigger_id" db:"trigger_id"`
	StepIndex  int        `json:"step_index" db:"step_index"`
	StepName   string     `json:"step_name" db:"step_name"`
	StepType   string     `json:"step_type" db:"step_type"` // info | llm | tool_call | shell | git | feishu
	InputData  string     `json:"input_data" db:"input_data"`
	OutputData string     `json:"output_data" db:"output_data"`
	Status     string     `json:"status" db:"status"` // pending | running | success | failed | skipped
	ErrorMsg   string     `json:"error_msg" db:"error_msg"`
	StartedAt  *time.Time `json:"started_at" db:"started_at"`
	FinishedAt *time.Time `json:"finished_at" db:"finished_at"`
}

// AuditLog 审计日志（高风险动作强制记录）
type AuditLog struct {
	ID        string    `json:"id" db:"id"`
	TriggerID string    `json:"trigger_id" db:"trigger_id"`
	Action    string    `json:"action" db:"action"`       // 如 git.push / mr.create / db.write
	RiskLevel string    `json:"risk_level" db:"risk_level"` // low | medium | high | critical
	Detail    string    `json:"detail" db:"detail"`
	Operator  string    `json:"operator" db:"operator"`
	Result    string    `json:"result" db:"result"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ─── 意图识别结构 ─────────────────────────────────────────────

// IntentResult LLM 意图识别结果（要求模型严格输出 JSON）
type IntentResult struct {
	Intent           string   `json:"intent"`            // issue_troubleshooting | requirement_writing | ignore | need_more_context | risky_action
	Confidence       float64  `json:"confidence"`        // 0.0 ~ 1.0
	MatchedKeywords  []string `json:"matched_keywords"`  // 命中的关键词
	SuspectedProject string   `json:"suspected_project"` // 猜测的项目名
	NeedRepoAccess   bool     `json:"need_repo_access"`
	NeedDocAccess    bool     `json:"need_doc_access"`
	NeedDBQuery      bool     `json:"need_db_query"`
	RiskLevel        string   `json:"risk_level"` // low | medium | high | critical
	Summary          string   `json:"summary"`    // 一句话摘要
}

// IntentType 意图类型常量
const (
	IntentIssueTroubleshooting = "issue_troubleshooting"
	IntentRequirementWriting   = "requirement_writing"
	IntentIgnore               = "ignore"
	IntentNeedMoreContext      = "need_more_context"
	IntentRiskyAction          = "risky_action"
)

// ─── 工作流上下文 ─────────────────────────────────────────────

// WorkflowContext 工作流执行上下文，贯穿整个处理过程
type WorkflowContext struct {
	TriggerID   string
	Message     *FeishuMessage
	Intent      *IntentResult
	Route       *ProjectRoute
	Steps       []*TriggerStep
	StepIndex   int
	DryRun      bool
	AutoCommit  bool
	AutoPush    bool
	AutoMR      bool
}

// FeishuMessage 飞书消息（解析后的结构）
type FeishuMessage struct {
	MessageID  string    `json:"message_id"`
	SenderID   string    `json:"sender_id"`
	SenderName string    `json:"sender_name"`
	ChatID     string    `json:"chat_id"`
	ChatType   string    `json:"chat_type"` // group | p2p
	Content    string    `json:"content"`   // 消息纯文本内容
	AtBotID    string    `json:"at_bot_id"` // 被 @ 的机器人 ID
	Timestamp  time.Time `json:"timestamp"`
}

// ─── MCP 相关 ────────────────────────────────────────────────

// MCPRequest MCP 工具调用请求
type MCPRequest struct {
	ToolName string         `json:"tool_name"`
	Method   string         `json:"method"`
	Params   map[string]any `json:"params"`
}

// MCPResponse MCP 工具调用结果
type MCPResponse struct {
	Success bool           `json:"success"`
	Data    map[string]any `json:"data"`
	Error   string         `json:"error"`
}

// ─── Claude Code 执行器 ───────────────────────────────────────

// ClaudeExecRequest Claude Code 执行请求
type ClaudeExecRequest struct {
	RepoPath    string `json:"repo_path"`
	TaskType    string `json:"task_type"`   // issue | requirement
	SystemPrompt string `json:"system_prompt"`
	UserPrompt  string `json:"user_prompt"`
	Context     string `json:"context"`     // 附加上下文
	DryRun      bool   `json:"dry_run"`
}

// ClaudeExecResult Claude Code 执行结果
type ClaudeExecResult struct {
	Plan         string   `json:"plan"`          // 修改计划
	Summary      string   `json:"summary"`       // 执行摘要
	FilesChanged []string `json:"files_changed"` // 修改的文件列表
	SQLSuggestions []string `json:"sql_suggestions"` // SQL 建议
	Logs         []string `json:"logs"`          // 执行日志
	Success      bool     `json:"success"`
	Error        string   `json:"error"`
}

// ─── Git 操作 ────────────────────────────────────────────────

// GitResult git 操作结果
type GitResult struct {
	Output string `json:"output"`
	Error  string `json:"error"`
	OK     bool   `json:"ok"`
}

// MRRequest 创建 MR/PR 的请求
type MRRequest struct {
	RepoPath    string `json:"repo_path"`
	RemoteURL   string `json:"remote_url"`
	Branch      string `json:"branch"`
	BaseBranch  string `json:"base_branch"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// MRResult 创建 MR/PR 的结果
type MRResult struct {
	URL   string `json:"url"`
	ID    string `json:"id"`
	Error string `json:"error"`
}

// ─── API 请求/响应通用结构 ────────────────────────────────────

// APIResponse 统一 API 响应
type APIResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// PageResult 分页结果
type PageResult struct {
	Total int `json:"total"`
	Page  int `json:"page"`
	Size  int `json:"size"`
	Items any `json:"items"`
}

// DashboardStats 首页仪表盘统计
type DashboardStats struct {
	ServiceStatus  string `json:"service_status"`   // running | stopped
	TotalTriggers  int    `json:"total_triggers"`
	SuccessCount   int    `json:"success_count"`
	FailedCount    int    `json:"failed_count"`
	PendingCount   int    `json:"pending_count"`
	RecentTriggers []*Trigger `json:"recent_triggers"`
}
