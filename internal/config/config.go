// Package config 加载并校验 config.yaml。
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Auth      AuthConfig       `yaml:"auth"`
	Storage   StorageConfig    `yaml:"storage"`
	Log       LogConfig        `yaml:"log"`
	Databases []DatabaseConfig `yaml:"databases"`
}

// ServerConfig 持有运行参数。QueryTimeout 对外暴露为 time.Duration，
// 但 YAML 中以 "30s" 之类的字符串书写（yaml.v3 不支持直接解析进 Duration），
// 因此用 QueryTimeoutRaw 接收原始字符串，在 Load 中解析填充 QueryTimeout。
type ServerConfig struct {
	Port            int           `yaml:"port"`
	QueryTimeoutRaw string        `yaml:"query_timeout"`
	QueryTimeout    time.Duration `yaml:"-"`
	MaxRows         int           `yaml:"max_rows"`
	LogLevel        string        `yaml:"log_level"` // 预留：当前用标准库 log，尚未接入分级日志
}

// AuthConfig 持有 IP 白名单与 session JWT 签名密钥。
type AuthConfig struct {
	IPWhitelist []string `yaml:"ip_whitelist"`
	// JWTSecret 是 session JWT 的 HMAC 签名密钥。留空则启动时随机生成
	// （重启后所有登录失效）。生产建议配一个固定强随机串，保证重启不掉线。
	JWTSecret string `yaml:"jwt_secret"`
}

// StorageConfig 持有本地持久化路径配置。
type StorageConfig struct {
	SQLitePath string `yaml:"sqlite_path"`
}

// LogConfig 持有日志相关配置。
type LogConfig struct {
	RetentionDays int `yaml:"retention_days"`
}

type DatabaseConfig struct {
	Name        string `yaml:"name"`
	Driver      string `yaml:"driver"`
	DSN         string `yaml:"dsn"`
	Description string `yaml:"description"`
}

// Load 读取并解析 config 文件，应用默认值并校验。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.parseDurations(); err != nil {
		return nil, err
	}
	cfg.applyEnvOverrides()
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) parseDurations() error {
	if c.Server.QueryTimeoutRaw == "" {
		return nil
	}
	d, err := time.ParseDuration(c.Server.QueryTimeoutRaw)
	if err != nil {
		return fmt.Errorf("invalid query_timeout %q: %w", c.Server.QueryTimeoutRaw, err)
	}
	c.Server.QueryTimeout = d
	return nil
}

// applyEnvOverrides 用环境变量覆盖敏感/部署相关配置。
// 环境变量优先级高于 YAML，便于在 Railway 等平台不写入 config 文件即可注入密钥。
func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("JWT_SECRET"); v != "" {
		c.Auth.JWTSecret = v
	}
}

func (c *Config) applyDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.QueryTimeout == 0 {
		c.Server.QueryTimeout = 30 * time.Second
	}
	if c.Server.MaxRows == 0 {
		c.Server.MaxRows = 10000
	}
	if c.Server.LogLevel == "" {
		c.Server.LogLevel = "info"
	}
	if c.Storage.SQLitePath == "" {
		c.Storage.SQLitePath = "./querygate.db"
	}
	if c.Log.RetentionDays == 0 {
		c.Log.RetentionDays = 30
	}
}

func (c *Config) validate() error {
	seen := map[string]bool{}
	for _, d := range c.Databases {
		if d.Name == "" || d.DSN == "" {
			return fmt.Errorf("database name and dsn are required")
		}
		if seen[d.Name] {
			return fmt.Errorf("duplicate database name: %s", d.Name)
		}
		seen[d.Name] = true
	}
	return nil
}
