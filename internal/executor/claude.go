package executor

import (
	"bufio"
	"context"
	"feishu-agent/internal/model"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const logsDir = "./logs/claude"

// ─── Job 注册表（跟踪活跃的 Claude Code 进程）─────────────────

// ActiveJob 活跃任务信息
type ActiveJob struct {
	TriggerID string             `json:"trigger_id"`
	Cancel    context.CancelFunc `json:"-"`
	StartedAt time.Time          `json:"started_at"`
}

var (
	jobsMu     sync.Mutex
	activeJobs = make(map[string]*ActiveJob)
)

func RegisterJob(triggerID string, cancel context.CancelFunc) {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	activeJobs[triggerID] = &ActiveJob{
		TriggerID: triggerID,
		Cancel:    cancel,
		StartedAt: time.Now(),
	}
}

func UnregisterJob(triggerID string) {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	delete(activeJobs, triggerID)
}

func CancelJob(triggerID string) bool {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	job, ok := activeJobs[triggerID]
	if !ok {
		return false
	}
	job.Cancel()
	delete(activeJobs, triggerID)
	return true
}

func ListActiveJobs() []string {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	ids := make([]string, 0, len(activeJobs))
	for id := range activeJobs {
		ids = append(ids, id)
	}
	return ids
}

// GetJob 获取活跃任务（供终端页判断状态）
func GetJob(triggerID string) *ActiveJob {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	return activeJobs[triggerID]
}

// ─── Claude Code 执行器 ─────────────────────────────────────────

type ClaudeCodeExecutor struct{}

func NewClaudeCodeExecutor() *ClaudeCodeExecutor {
	return &ClaudeCodeExecutor{}
}

// LogFilePath 返回指定 trigger 的日志文件路径
func LogFilePath(triggerID string) string {
	return filepath.Join(logsDir, triggerID+".log")
}

// Execute 调用 claude -p（分析模式，不修改文件），逐行写入日志文件
func (e *ClaudeCodeExecutor) Execute(ctx context.Context, req *model.ClaudeExecRequest) (*model.ClaudeExecResult, error) {
	if req.DryRun {
		return &model.ClaudeExecResult{
			Success: true,
			Summary: "[dry-run] 跳过实际执行",
			Logs:    []string{"dry-run mode"},
		}, nil
	}

	// 准备日志文件
	os.MkdirAll(logsDir, 0755) //nolint
	var logFile *os.File
	if req.TriggerID != "" {
		if f, err := os.Create(LogFilePath(req.TriggerID)); err == nil {
			logFile = f
			defer logFile.Close()
		}
	}

	// 构建命令（pipe 模式，只分析不修改）
	dir := req.RepoPath
	if dir != "" {
		dir = expandHome(dir)
	}
	cmd := exec.CommandContext(ctx, "claude", "-p", req.UserPrompt)
	if dir != "" {
		cmd.Dir = dir
	}

	// 管道读取 stdout，逐行写入日志
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return &model.ClaudeExecResult{Success: false, Error: err.Error()}, err
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return &model.ClaudeExecResult{
			Success: false,
			Error:   fmt.Sprintf("claude CLI 启动失败: %v", err),
		}, err
	}

	log.Printf("[claude] trigger=%s started (dir=%s)", req.TriggerID, dir)

	var outputBuf strings.Builder
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		outputBuf.WriteString(line)
		outputBuf.WriteString("\n")
		if logFile != nil {
			fmt.Fprintln(logFile, line)
			logFile.Sync() //nolint
		}
	}
	if scanner.Err() != nil {
		remaining, _ := io.ReadAll(stdoutPipe)
		if len(remaining) > 0 {
			outputBuf.Write(remaining)
			if logFile != nil {
				logFile.Write(remaining) //nolint
			}
		}
	}

	waitErr := cmd.Wait()

	if ctx.Err() != nil {
		cancelMsg := "执行被取消"
		if ctx.Err() == context.DeadlineExceeded {
			cancelMsg = "执行超时"
		}
		if logFile != nil {
			fmt.Fprintf(logFile, "\n[CANCELLED] %s\n", cancelMsg)
		}
		return &model.ClaudeExecResult{
			Success: false,
			Error:   cancelMsg,
			Plan:    outputBuf.String(),
		}, ctx.Err()
	}

	if waitErr != nil {
		errMsg := fmt.Sprintf("claude CLI: %v (stderr: %s)", waitErr, truncateStr(stderrBuf.String(), 300))
		if logFile != nil {
			fmt.Fprintln(logFile, "\n[ERROR] "+errMsg)
		}
		return &model.ClaudeExecResult{
			Success: false,
			Error:   errMsg,
			Summary: stderrBuf.String(),
			Plan:    outputBuf.String(),
		}, waitErr
	}

	output := strings.TrimSpace(outputBuf.String())
	summary := output
	if len([]rune(summary)) > 500 {
		summary = string([]rune(summary)[:500]) + "..."
	}

	if logFile != nil {
		fmt.Fprintln(logFile, "\n[DONE]")
	}

	return &model.ClaudeExecResult{
		Success: true,
		Summary: summary,
		Plan:    output,
		Logs:    []string{"claude CLI 执行完成"},
	}, nil
}
