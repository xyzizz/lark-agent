// Package config 负责配置文件加载与全局访问
package config

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config 全量配置结构（对应 config.yaml）
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Feishu   FeishuConfig   `yaml:"feishu"`
	LLM      LLMConfig      `yaml:"llm"`
	Database DatabaseConfig `yaml:"database"`
	Harness  HarnessConfig  `yaml:"harness"`
	Log      LogConfig      `yaml:"log"`
}

type ServerConfig struct {
	Host  string `yaml:"host"`
	Port  int    `yaml:"port"`
	Debug bool   `yaml:"debug"`
}

type FeishuConfig struct {
	AppID             string `yaml:"app_id"`
	AppSecret         string `yaml:"app_secret"`
	VerificationToken string `yaml:"verification_token"`
	EncryptKey        string `yaml:"encrypt_key"`
	BotWebhook        string `yaml:"bot_webhook"`
	EventMode         string `yaml:"event_mode"` // webhook | websocket
}

type LLMConfig struct {
	APIKey         string `yaml:"api_key"`
	BaseURL        string `yaml:"base_url"`
	Model          string `yaml:"model"`
	MaxTokens      int    `yaml:"max_tokens"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// HarnessConfig Agent 执行约束配置
type HarnessConfig struct {
	AutoCommit           bool `yaml:"auto_commit"`
	AutoPush             bool `yaml:"auto_push"`
	AutoCreateMR         bool `yaml:"auto_create_mr"`
	AllowDBWrite         bool `yaml:"allow_db_write"`
	AllowAutoMerge       bool `yaml:"allow_auto_merge"`
	RequirePlanBeforeExec bool `yaml:"require_plan_before_exec"`
	DryRun               bool `yaml:"dry_run"`
	MaxSteps             int  `yaml:"max_steps"`
	RequireConfirmOnRisky bool `yaml:"require_confirm_on_risky"`
}

type LogConfig struct {
	Level string `yaml:"level"`
	Path  string `yaml:"path"`
}

var (
	global *Config
	once   sync.Once
	mu     sync.RWMutex
)

// Load 从文件加载配置（只加载一次，后续用 Get 获取）
func Load(path string) (*Config, error) {
	var loadErr error
	once.Do(func() {
		cfg := defaultConfig()
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			loadErr = err
			return
		}
		if err == nil {
			if err = yaml.Unmarshal(data, cfg); err != nil {
				loadErr = err
				return
			}
		}
		mu.Lock()
		global = cfg
		mu.Unlock()
	})
	return global, loadErr
}

// Get 获取全局配置（线程安全）
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	if global == nil {
		return defaultConfig()
	}
	return global
}

// Update 运行时更新部分配置（如从 SQLite 加载动态配置）
func Update(fn func(*Config)) {
	mu.Lock()
	defer mu.Unlock()
	if global == nil {
		global = defaultConfig()
	}
	fn(global)
}

func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:  "0.0.0.0",
			Port:  8080,
			Debug: false,
		},
		LLM: LLMConfig{
			BaseURL:        "https://api.anthropic.com",
			Model:          "claude-opus-4-6",
			MaxTokens:      4096,
			TimeoutSeconds: 60,
		},
		Database: DatabaseConfig{
			Path: "./feishu-agent.db",
		},
		Harness: HarnessConfig{
			AutoCommit:            false,
			AutoPush:              false,
			AutoCreateMR:          false,
			AllowDBWrite:          false,
			AllowAutoMerge:        false,
			RequirePlanBeforeExec: true,
			DryRun:                false,
			MaxSteps:              20,
			RequireConfirmOnRisky: true,
		},
		Log: LogConfig{
			Level: "info",
			Path:  "./logs/feishu-agent.log",
		},
	}
}
