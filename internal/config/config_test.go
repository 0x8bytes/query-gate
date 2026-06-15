package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_ParsesAndAppliesDefaults(t *testing.T) {
	p := writeTemp(t, `
auth:
  ip_whitelist: ["*"]
databases:
  - name: prod
    driver: mysql
    dsn: "u:p@tcp(h:3306)/d"
    description: 生产库
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("default port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Server.QueryTimeout != 30*time.Second {
		t.Errorf("default timeout = %v, want 30s", cfg.Server.QueryTimeout)
	}
	if cfg.Server.MaxRows != 10000 {
		t.Errorf("default max_rows = %d, want 10000", cfg.Server.MaxRows)
	}
	if len(cfg.Databases) != 1 || cfg.Databases[0].Name != "prod" {
		t.Errorf("databases not parsed: %+v", cfg.Databases)
	}
}

// TestLoad_AllowsEmptyDatabases 校验：databases 为空列表时允许加载（连接可全走 admin API 动态添加）。
func TestLoad_AllowsEmptyDatabases(t *testing.T) {
	p := writeTemp(t, `
auth:
  ip_whitelist: ["*"]
databases: []
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("empty databases should be allowed, got: %v", err)
	}
	if len(cfg.Databases) != 0 {
		t.Fatalf("expected 0 databases, got %d", len(cfg.Databases))
	}
}

// TestLoad_JWTSecretEnvOverridesConfig 校验：环境变量 JWT_SECRET 优先于 config 文件值。
func TestLoad_JWTSecretEnvOverridesConfig(t *testing.T) {
	t.Setenv("JWT_SECRET", "from-env")
	p := writeTemp(t, `
auth:
  ip_whitelist: ["*"]
  jwt_secret: "from-config"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.JWTSecret != "from-env" {
		t.Errorf("jwt_secret = %q, want env value %q", cfg.Auth.JWTSecret, "from-env")
	}
}

// TestLoad_JWTSecretFallsBackToConfig 校验：环境变量为空时回退用 config 文件值。
func TestLoad_JWTSecretFallsBackToConfig(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	p := writeTemp(t, `
auth:
  ip_whitelist: ["*"]
  jwt_secret: "from-config"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.JWTSecret != "from-config" {
		t.Errorf("jwt_secret = %q, want config value %q", cfg.Auth.JWTSecret, "from-config")
	}
}

func TestLoad_RejectsInvalidDuration(t *testing.T) {
	p := writeTemp(t, `
server:
  query_timeout: "abc"
auth:
  ip_whitelist: ["*"]
databases:
  - name: prod
    driver: mysql
    dsn: "u:p@tcp(h:3306)/d"
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error on invalid query_timeout")
	}
}

func TestLoad_ParsesCustomDuration(t *testing.T) {
	p := writeTemp(t, `
server:
  query_timeout: "5s"
auth:
  ip_whitelist: ["*"]
databases:
  - name: prod
    driver: mysql
    dsn: "u:p@tcp(h:3306)/d"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.QueryTimeout != 5*time.Second {
		t.Errorf("query_timeout = %v, want 5s", cfg.Server.QueryTimeout)
	}
}

func TestLoad_RejectsDuplicateDBName(t *testing.T) {
	p := writeTemp(t, `
auth:
  ip_whitelist: ["*"]
databases:
  - {name: prod, driver: mysql, dsn: "a"}
  - {name: prod, driver: mysql, dsn: "b"}
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error on duplicate db name")
	}
}

func TestLoad_StorageLog(t *testing.T) {
	p := writeTemp(t, `
storage:
  sqlite_path: ./qn.db
log:
  retention_days: 7
auth:
  ip_whitelist: ["*"]
databases:
  - name: prod
    driver: mysql
    dsn: "user:pass@tcp(h:3306)/prod"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.SQLitePath != "./qn.db" {
		t.Fatalf("sqlite path = %q", cfg.Storage.SQLitePath)
	}
	if cfg.Log.RetentionDays != 7 {
		t.Fatalf("retention = %d", cfg.Log.RetentionDays)
	}
}

func TestLoad_DefaultsRetentionAndSQLitePath(t *testing.T) {
	p := writeTemp(t, `
auth:
  ip_whitelist: ["*"]
databases:
  - name: prod
    driver: mysql
    dsn: "user:pass@tcp(h:3306)/prod"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.SQLitePath != "./querygate.db" {
		t.Fatalf("default sqlite path = %q", cfg.Storage.SQLitePath)
	}
	if cfg.Log.RetentionDays != 30 {
		t.Fatalf("default retention = %d", cfg.Log.RetentionDays)
	}
}
