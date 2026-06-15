// Package mysql 是 driver.Driver 的 MySQL 实现。
package mysql

import (
	"context"
	"database/sql"
	"fmt"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/0x8bytes/query-gate/internal/model"
)

type Driver struct {
	name string
	desc string
	db   *sql.DB
}

// New 打开一个 MySQL 连接池。
func New(name, dsn, desc string) (*Driver, error) {
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql %s: %w", name, err)
	}
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping mysql %s: %w", name, err)
	}
	return &Driver{name: name, desc: desc, db: conn}, nil
}

func (d *Driver) Info() model.DatabaseInfo {
	return model.DatabaseInfo{Name: d.name, Driver: "mysql", Description: d.desc}
}

func (d *Driver) Tables(ctx context.Context) ([]model.TableInfo, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT TABLE_NAME, IFNULL(TABLE_COMMENT, '') FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() ORDER BY TABLE_NAME")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.TableInfo
	for rows.Next() {
		var ti model.TableInfo
		if err := rows.Scan(&ti.Name, &ti.Comment); err != nil {
			return nil, err
		}
		out = append(out, ti)
	}
	return out, rows.Err()
}

func (d *Driver) Schema(ctx context.Context, tables []string) (map[string]string, []string, error) {
	ddl := map[string]string{}
	var notFound []string
	for _, t := range tables {
		var name, create string
		err := d.db.QueryRowContext(ctx, "SHOW CREATE TABLE `"+sanitizeIdent(t)+"`").Scan(&name, &create)
		if err == sql.ErrNoRows {
			notFound = append(notFound, t)
			continue
		}
		if err != nil {
			if me, ok := err.(*gomysql.MySQLError); ok && me.Number == 1146 {
				notFound = append(notFound, t)
				continue
			}
			return nil, nil, err
		}
		ddl[t] = create
	}
	return ddl, notFound, nil
}

func (d *Driver) Query(ctx context.Context, query string, limit, maxRows int) (*model.QueryResult, error) {
	rowCap := maxRows
	if limit > 0 && limit < rowCap {
		rowCap = limit
	}
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	res := &model.QueryResult{Columns: cols, Rows: [][]any{}}
	for rows.Next() {
		if len(res.Rows) >= rowCap {
			res.Truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		res.Rows = append(res.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.RowCount = len(res.Rows)
	return res, nil
}

// Exec 执行写语句,返回受影响行数。
func (d *Driver) Exec(ctx context.Context, command string) (int64, error) {
	res, err := d.db.ExecContext(ctx, command)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *Driver) Close() error { return d.db.Close() }

// sanitizeIdent 简单转义反引号，防 SHOW CREATE TABLE 的标识符注入。
func sanitizeIdent(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '`' {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
