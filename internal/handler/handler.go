// Package handler 实现 HTTP 接口,连接 driver registry、guard、data。
package handler

import (
	"context"
	"sync"
	"time"

	"github.com/0x8bytes/query-gate/internal/data"
	"github.com/0x8bytes/query-gate/internal/driver"
)

const maxSchemaTables = 20

// Handler 持有 handler 依赖。
type Handler struct {
	Registry     *driver.Registry
	ExecRegistry *driver.Registry // 写连接(exec_dsn);仅配了 exec_dsn 的库在此注册
	Store        *data.Store
	QueryTimeout time.Duration
	MaxRows      int
	IPWhitelist  []string
	ServerSecret []byte     // 用于签名 / 校验 session JWT
	adminMu      sync.Mutex // 序列化 admin DB 写操作(create/update/delete)
}

// contextWithTimeout 基于给定 context 派生带超时的 context。
func contextWithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}
