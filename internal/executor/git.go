package executor

import (
	"context"
	"feishu-agent/internal/model"
	"fmt"
	"strings"
	"time"
)

// GitExecutor 封装 git 操作
type GitExecutor struct {
	RepoPath string
}

func NewGitExecutor(repoPath string) *GitExecutor {
	return &GitExecutor{RepoPath: repoPath}
}

// Status 获取 git 状态
func (g *GitExecutor) Status(ctx context.Context) (*model.GitResult, error) {
	r, err := RunShell(ctx, g.RepoPath, "git", "status", "--porcelain")
	return toGitResult(r, err), nil
}

// CurrentBranch 获取当前分支名
func (g *GitExecutor) CurrentBranch(ctx context.Context) (string, error) {
	r, err := RunShell(ctx, g.RepoPath, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(r.Stdout), nil
}

// Checkout 切换分支（分支不存在时报错）
func (g *GitExecutor) Checkout(ctx context.Context, branch string) (*model.GitResult, error) {
	r, err := RunShell(ctx, g.RepoPath, "git", "checkout", branch)
	return toGitResult(r, err), err
}

// CreateBranch 基于当前 HEAD 创建并切换到新分支
func (g *GitExecutor) CreateBranch(ctx context.Context, branch string) (*model.GitResult, error) {
	r, err := RunShell(ctx, g.RepoPath, "git", "checkout", "-b", branch)
	return toGitResult(r, err), err
}

// GenerateBranchName 根据触发 ID 和类型生成分支名
// 格式：agent/{type}/{date}-{shortID}
func GenerateBranchName(taskType, triggerID string) string {
	date := time.Now().Format("0102") // MMDD
	short := triggerID
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("agent/%s/%s-%s", taskType, date, short)
}

// Add 暂存文件（空则 add all）
func (g *GitExecutor) Add(ctx context.Context, files ...string) (*model.GitResult, error) {
	args := []string{"add"}
	if len(files) == 0 {
		args = append(args, ".")
	} else {
		args = append(args, files...)
	}
	r, err := RunShell(ctx, g.RepoPath, "git", args...)
	return toGitResult(r, err), err
}

// Commit 提交变更
func (g *GitExecutor) Commit(ctx context.Context, message string) (*model.GitResult, error) {
	r, err := RunShell(ctx, g.RepoPath, "git", "commit", "-m", message)
	return toGitResult(r, err), err
}

// Push 推送到远端
func (g *GitExecutor) Push(ctx context.Context, remote, branch string) (*model.GitResult, error) {
	r, err := RunShell(ctx, g.RepoPath, "git", "push", remote, branch)
	return toGitResult(r, err), err
}

// PushSetUpstream 推送并设置上游
func (g *GitExecutor) PushSetUpstream(ctx context.Context, remote, branch string) (*model.GitResult, error) {
	r, err := RunShell(ctx, g.RepoPath, "git", "push", "--set-upstream", remote, branch)
	return toGitResult(r, err), err
}

// Diff 获取变更 diff
func (g *GitExecutor) Diff(ctx context.Context) (*model.GitResult, error) {
	r, err := RunShell(ctx, g.RepoPath, "git", "diff", "--stat")
	return toGitResult(r, err), nil
}

// Log 获取最近提交记录
func (g *GitExecutor) Log(ctx context.Context, n int) (*model.GitResult, error) {
	r, err := RunShell(ctx, g.RepoPath, "git", "log",
		fmt.Sprintf("--max-count=%d", n), "--oneline")
	return toGitResult(r, err), nil
}

// Pull 拉取最新代码
func (g *GitExecutor) Pull(ctx context.Context) (*model.GitResult, error) {
	r, err := RunShell(ctx, g.RepoPath, "git", "pull")
	return toGitResult(r, err), err
}

// MRCreator MR/PR 创建接口（平台无关抽象）
type MRCreator interface {
	CreateMR(ctx context.Context, req *model.MRRequest) (*model.MRResult, error)
}

// GitLabMRCreator GitLab MR 创建器（示例实现）
type GitLabMRCreator struct {
	BaseURL string
	Token   string
}

func (g *GitLabMRCreator) CreateMR(ctx context.Context, req *model.MRRequest) (*model.MRResult, error) {
	// TODO: 调用 GitLab API 创建 MR
	// POST /api/v4/projects/:id/merge_requests
	// 此处返回占位结果，后续按需实现
	return &model.MRResult{
		URL:   fmt.Sprintf("%s/-/merge_requests/new?merge_request[source_branch]=%s", req.RemoteURL, req.Branch),
		Error: "",
	}, nil
}

// GithubPRCreator GitHub PR 创建器（示例实现）
type GithubPRCreator struct {
	Token string
}

func (g *GithubPRCreator) CreateMR(ctx context.Context, req *model.MRRequest) (*model.MRResult, error) {
	// TODO: 调用 GitHub API 创建 PR
	// POST /repos/{owner}/{repo}/pulls
	return &model.MRResult{
		URL: fmt.Sprintf("%s/compare/%s...%s?expand=1", req.RemoteURL, req.BaseBranch, req.Branch),
	}, nil
}

// NoopMRCreator 空实现（dry-run 模式或未配置时使用）
type NoopMRCreator struct{}

func (n *NoopMRCreator) CreateMR(ctx context.Context, req *model.MRRequest) (*model.MRResult, error) {
	return &model.MRResult{
		URL: fmt.Sprintf("[dry-run] branch: %s -> %s", req.Branch, req.BaseBranch),
	}, nil
}

func toGitResult(r *ShellResult, err error) *model.GitResult {
	if r == nil {
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		return &model.GitResult{Error: msg, OK: false}
	}
	output := strings.TrimSpace(r.Stdout)
	if r.Stderr != "" {
		output += "\n" + strings.TrimSpace(r.Stderr)
	}
	return &model.GitResult{
		Output: output,
		Error:  func() string { if err != nil { return err.Error() }; return "" }(),
		OK:     err == nil && r.ExitCode == 0,
	}
}
