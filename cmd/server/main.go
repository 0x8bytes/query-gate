// Command server 启动 querygate REST 服务。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/0x8bytes/query-gate/internal/config"
	"github.com/0x8bytes/query-gate/internal/data"
	"github.com/0x8bytes/query-gate/internal/driver"
	"github.com/0x8bytes/query-gate/internal/driver/dbfactory"
	"github.com/0x8bytes/query-gate/internal/handler"
	"github.com/0x8bytes/query-gate/internal/router"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	st, err := data.Open(cfg.Storage.SQLitePath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	reg := driver.NewRegistry()
	execReg := driver.NewRegistry()

	// 1. YAML 种子库(只读)
	seedNames := map[string]bool{}
	for _, dc := range cfg.Databases {
		drv, err := dbfactory.OpenDriver(dc.Name, dc.Driver, dc.DSN, dc.Description)
		if err != nil {
			log.Fatalf("connect seed db %s: %v", dc.Name, err)
		}
		reg.Register(dc.Name, drv, driver.SourceSeed)
		seedNames[dc.Name] = true
		log.Printf("registered seed %s instance %q", dc.Driver, dc.Name)
	}
	// 2. SQLite 动态库;同名冲突报错
	dynDBs, err := st.ListDatabases()
	if err != nil {
		log.Fatalf("load dynamic dbs: %v", err)
	}
	for _, r := range dynDBs {
		if seedNames[r.Name] {
			log.Fatalf("database name conflict between YAML seed and SQLite: %s", r.Name)
		}
		drv, err := dbfactory.OpenDriver(r.Name, r.Driver, r.DSN, r.Description)
		if err != nil {
			log.Printf("WARN skip dynamic db %s: %v", r.Name, err)
			continue
		}
		reg.Register(r.Name, drv, driver.SourceDynamic)
		log.Printf("registered dynamic %s instance %q", r.Driver, r.Name)
		if r.ExecDSN != "" {
			edrv, err := dbfactory.OpenDriver(r.Name, r.Driver, r.ExecDSN, r.Description)
			if err != nil {
				log.Printf("WARN skip exec conn for db %s: %v", r.Name, err)
			} else {
				execReg.Register(r.Name, edrv, driver.SourceDynamic)
				log.Printf("registered exec conn for %q", r.Name)
			}
		}
	}
	defer reg.Close()
	defer execReg.Close()

	// 3. 后台定时清理过期查询日志(每天一次)
	go pruneLoop(st, cfg.Log.RetentionDays)

	// JWT 签名密钥:必须由环境变量 JWT_SECRET 或 config 的 auth.jwt_secret 提供
	// (环境变量优先,已在 config 加载时处理)。两者皆空则拒绝启动。
	if cfg.Auth.JWTSecret == "" {
		log.Fatal("auth.jwt_secret 未配置:请设置环境变量 JWT_SECRET 或在 config 配置 auth.jwt_secret (生成: openssl rand -hex 32)")
	}
	secret := []byte(cfg.Auth.JWTSecret)

	h := &handler.Handler{
		Registry:     reg,
		ExecRegistry: execReg,
		Store:        st,
		QueryTimeout: cfg.Server.QueryTimeout,
		MaxRows:      cfg.Server.MaxRows,
		IPWhitelist:  cfg.Auth.IPWhitelist,
		ServerSecret: secret,
	}
	engine := router.Setup(h)

	// Railway 注入 PORT 环境变量时优先用它,否则用配置端口。
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	log.Printf("querygate listening on %s", addr)
	if err := engine.Run(addr); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// pruneLoop 每天清理一次过期的查询日志。
func pruneLoop(st *data.Store, retentionDays int) {
	prune := func() {
		cutoff := time.Now().AddDate(0, 0, -retentionDays).UTC().Format(time.RFC3339)
		if n, err := st.PruneQueryLogs(cutoff); err != nil {
			log.Printf("prune logs: %v", err)
		} else if n > 0 {
			log.Printf("pruned %d old query logs", n)
		}
	}
	prune() // 启动时立即清理一次
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for range t.C {
		prune()
	}
}
