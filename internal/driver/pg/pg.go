// Package pg 是 driver.Driver 的 PostgreSQL 实现。
package pg

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/0x8bytes/query-gate/internal/model"
)

type Driver struct {
	name string
	desc string
	db   *sql.DB
}

// New 打开 PG 连接池。dsn 形如 postgres://user:pass@host:5432/dbname?sslmode=require
func New(name, dsn, desc string) (*Driver, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open pg %s: %w", name, err)
	}
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping pg %s: %w", name, err)
	}
	return &Driver{name: name, desc: desc, db: conn}, nil
}

func (d *Driver) Info() model.DatabaseInfo {
	return model.DatabaseInfo{Name: d.name, Driver: "postgres", Description: d.desc}
}

// Tables 返回 public schema 下的表名与备注。
func (d *Driver) Tables(ctx context.Context) ([]model.TableInfo, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT c.relname,
       COALESCE(obj_description(c.oid), '')
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r','v','m') AND n.nspname = 'public'
ORDER BY c.relname`)
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

// Schema 返回每个表的列定义文本(PG 无 SHOW CREATE TABLE,自行拼列清单)。
func (d *Driver) Schema(ctx context.Context, tables []string) (map[string]string, []string, error) {
	ddl := map[string]string{}
	var notFound []string
	for _, t := range tables {
		rows, err := d.db.QueryContext(ctx, `
SELECT column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_schema='public' AND table_name=$1
ORDER BY ordinal_position`, t)
		if err != nil {
			return nil, nil, err
		}
		var cols []string
		for rows.Next() {
			var name, typ, nullable string
			if err := rows.Scan(&name, &typ, &nullable); err != nil {
				rows.Close()
				return nil, nil, err
			}
			null := "NOT NULL"
			if nullable == "YES" {
				null = "NULL"
			}
			cols = append(cols, fmt.Sprintf("  %s %s %s", name, typ, null))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		if len(cols) == 0 {
			notFound = append(notFound, t)
			continue
		}
		ddl[t] = fmt.Sprintf("TABLE %s (\n%s\n)", t, joinLines(cols))
	}
	return ddl, notFound, nil
}

// Query 执行只读 SQL。limit/maxRows 行数截断逻辑与 MySQL 一致。
func (d *Driver) Query(ctx context.Context, query string, limit, maxRows int) (*model.QueryResult, error) {
	rowCap := maxRows
	if limit > 0 && limit < rowCap {
		rowCap = limit
	}
	if rowCap <= 0 {
		rowCap = 10000 // 兜底:maxRows 未配置时用默认上限,避免 0 行+Truncated 的陷阱
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

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += ",\n"
		}
		out += l
	}
	return out
}
