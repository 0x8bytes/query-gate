// Package driver 定义数据库驱动抽象与实例注册表。
package driver

import (
	"context"

	"github.com/0x8bytes/query-gate/internal/model"
)

// Driver 是一个数据库实例的查询能力抽象。MySQL 为首个实现，
// 未来 PostgreSQL/MongoDB 各自实现本接口即可接入，核心不变。
type Driver interface {
	// Info 返回不含敏感信息的实例元数据。
	Info() model.DatabaseInfo
	// Tables 返回所有表的名称与备注。spec 4.3。
	Tables(ctx context.Context) ([]model.TableInfo, error)
	// Schema 返回每个表的原始 DDL（SHOW CREATE TABLE 文本）；
	// 不存在的表名放入 notFound。spec 4.4。
	Schema(ctx context.Context, tables []string) (ddl map[string]string, notFound []string, err error)
	// Query 执行只读查询,maxRows 为强制行数上限。query 为原始查询文本(SQL 或
	// driver 自定义语法),内容与语法由调用方编写。spec 4.5。
	Query(ctx context.Context, query string, limit, maxRows int) (*model.QueryResult, error)
	// Exec 执行写操作(INSERT/UPDATE/DELETE/DDL 等),返回受影响行数。
	// 不加 guard,完全依赖底层连接(exec_dsn 写账号)的权限。
	Exec(ctx context.Context, command string) (rowsAffected int64, err error)
	// Close 释放底层连接。
	Close() error
}
