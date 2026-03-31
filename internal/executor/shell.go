// Package executor 提供 shell、git、MCP、Claude Code 等执行能力
package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ShellResult shell 执行结果
type ShellResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// RunShell 在指定目录执行 shell 命令
func RunShell(ctx context.Context, dir, command string, args ...string) (*ShellResult, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, command, args...)
	if dir != "" {
		cmd.Dir = expandHome(dir)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &ShellResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		// 非零退出码不一定是错误，交给调用方判断
		return result, fmt.Errorf("cmd %s: %w (stderr: %s)", command, err, truncateStr(stderr.String(), 300))
	}
	return result, nil
}

// RunShellScript 执行 bash 脚本字符串
func RunShellScript(ctx context.Context, dir, script string) (*ShellResult, error) {
	return RunShell(ctx, dir, "bash", "-c", script)
}

// RunGoTest 在指定目录执行 go test
func RunGoTest(ctx context.Context, repoPath string) (*ShellResult, error) {
	return RunShell(ctx, repoPath, "go", "test", "./...")
}

// RunCustomCheck 执行自定义检查命令（从配置读取）
func RunCustomCheck(ctx context.Context, repoPath, command string) (*ShellResult, error) {
	if command == "" {
		return &ShellResult{Stdout: "no check command configured", ExitCode: 0}, nil
	}
	parts := strings.Fields(command)
	if len(parts) == 1 {
		return RunShell(ctx, repoPath, parts[0])
	}
	return RunShell(ctx, repoPath, parts[0], parts[1:]...)
}

// expandHome 展开路径中的 ~ 为用户主目录
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	return path
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
