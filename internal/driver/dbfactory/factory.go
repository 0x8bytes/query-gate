// Package dbfactory 按 driver 名构造并连接 driver.Driver。
// 单独成包以避免 driver 包与各 driver 子包之间的 import cycle
// (各 driver 子包已 import model 取共享类型)。
package dbfactory

import (
	"fmt"

	"github.com/0x8bytes/query-gate/internal/driver"
	"github.com/0x8bytes/query-gate/internal/driver/mongo"
	"github.com/0x8bytes/query-gate/internal/driver/mysql"
	"github.com/0x8bytes/query-gate/internal/driver/pg"
)

// OpenDriver 按 driver 名构造并连接一个 driver.Driver。空 drv 视为 mysql(向后兼容)。
func OpenDriver(name, drv, dsn, desc string) (driver.Driver, error) {
	switch drv {
	case "mysql", "":
		return mysql.New(name, dsn, desc)
	case "postgres", "pg", "postgresql":
		return pg.New(name, dsn, desc)
	case "mongodb", "mongo":
		return mongo.New(name, dsn, desc)
	default:
		return nil, fmt.Errorf("unsupported driver %q for db %q", drv, name)
	}
}
