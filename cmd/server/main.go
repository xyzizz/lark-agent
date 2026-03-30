// Package main 飞书消息驱动的自动问题排查 / 需求编写系统入口
package main

import (
	"feishu-agent/internal/config"
	"feishu-agent/internal/executor"
	"feishu-agent/internal/httpserver"
	"feishu-agent/internal/store"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// 解析命令行参数
	configPath := flag.String("config", "./config.yaml", "配置文件路径")
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Printf("[main] feishu-agent starting...")

	// 1. 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[main] load config: %v", err)
	}
	log.Printf("[main] config loaded from %s", *configPath)

	// 2. 初始化数据库
	if err = store.Init(cfg.Database.Path); err != nil {
		log.Fatalf("[main] init db: %v", err)
	}
	defer store.Close() //nolint

	// 3. 从 SQLite 加载所有运行时配置，合并进全局 Config（DB 优先于 YAML）
	settings, _ := store.GetAllSettings()
	applyDBSettings(settings)

	cfg = config.Get() // 重新获取合并后的配置
	appID := firstNonEmpty(settings["feishu_app_id"], cfg.Feishu.AppID)
	appSecret := firstNonEmpty(settings["feishu_app_secret"], cfg.Feishu.AppSecret)

	// 4. 初始化 MCP 客户端
	if err = executor.InitMCPClients(); err != nil {
		log.Printf("[main] init mcp clients warning: %v", err)
	}

	// 5. 创建 HTTP 服务
	srv := httpserver.New(cfg.Server.Debug)
	srv.InitFeishu(appID, appSecret)

	// 6. 启动服务
	addr := httpserver.Addr(cfg.Server.Host, cfg.Server.Port)
	log.Printf("[main] server listening on http://%s", addr)
	log.Printf("[main] 飞书 Webhook URL: http://<your-host>:%d/webhook/feishu", cfg.Server.Port)
	log.Printf("[main] 管理后台: http://localhost:%d", cfg.Server.Port)

	// 优雅退出
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Printf("[main] shutting down...")
		store.Close() //nolint
		os.Exit(0)
	}()

	if err = srv.Run(addr); err != nil {
		log.Fatalf("[main] server error: %v", err)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// applyDBSettings 将数据库中的配置合并进全局 Config
// 每次启动时调用一次，保证 DB 里的配置重启后生效
func applyDBSettings(s map[string]string) {
	config.Update(func(c *config.Config) {
		// 飞书
		if v := s["feishu_app_id"]; v != "" {
			c.Feishu.AppID = v
		}
		if v := s["feishu_app_secret"]; v != "" {
			c.Feishu.AppSecret = v
		}
		if v := s["feishu_verification_token"]; v != "" {
			c.Feishu.VerificationToken = v
		}
		if v := s["feishu_encrypt_key"]; v != "" {
			c.Feishu.EncryptKey = v
		}
		if v := s["feishu_bot_webhook"]; v != "" {
			c.Feishu.BotWebhook = v
		}
		// LLM
		if v := s["llm_api_key"]; v != "" {
			c.LLM.APIKey = v
		}
		if v := s["llm_base_url"]; v != "" {
			c.LLM.BaseURL = v
		}
		if v := s["llm_model"]; v != "" {
			c.LLM.Model = v
		}
		if v := s["llm_max_tokens"]; v != "" {
			var n int
			fmt.Sscanf(v, "%d", &n)
			if n > 0 {
				c.LLM.MaxTokens = n
			}
		}
		// Harness
		if v := s["harness_auto_commit"]; v != "" {
			c.Harness.AutoCommit = v == "true"
		}
		if v := s["harness_auto_push"]; v != "" {
			c.Harness.AutoPush = v == "true"
		}
		if v := s["harness_auto_create_mr"]; v != "" {
			c.Harness.AutoCreateMR = v == "true"
		}
		if v := s["harness_allow_db_write"]; v != "" {
			c.Harness.AllowDBWrite = v == "true"
		}
		if v := s["harness_allow_auto_merge"]; v != "" {
			c.Harness.AllowAutoMerge = v == "true"
		}
		if v := s["harness_require_confirm_on_risky"]; v != "" {
			c.Harness.RequireConfirmOnRisky = v == "true"
		}
		if v := s["harness_require_plan_before_exec"]; v != "" {
			c.Harness.RequirePlanBeforeExec = v == "true"
		}
		if v := s["harness_dry_run"]; v != "" {
			c.Harness.DryRun = v == "true"
		}
		if v := s["harness_max_steps"]; v != "" {
			var n int
			fmt.Sscanf(v, "%d", &n)
			if n > 0 {
				c.Harness.MaxSteps = n
			}
		}
	})
}
