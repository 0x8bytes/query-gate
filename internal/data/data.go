// Package data 封装 SQLite 持久化:数据库连接、users、敏感列、查询日志。
package data

import (
	"database/sql"
	"fmt"

	"github.com/0x8bytes/query-gate/internal/model"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动,无需 CGO
)

// Store 持有 SQLite 连接。所有方法并发安全(SQLite 单连接 + 内部串行)。
type Store struct {
	db *sql.DB
}

// Open 打开(或创建)SQLite 文件并建表。建表用 IF NOT EXISTS,可重复调用。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // SQLite 写串行化,单连接避免 "database is locked"
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS databases (
	name TEXT PRIMARY KEY, driver TEXT NOT NULL, dsn TEXT NOT NULL, description TEXT
);
CREATE TABLE IF NOT EXISTS users (
	username TEXT PRIMARY KEY, password_hash TEXT NOT NULL, api_key TEXT NOT NULL UNIQUE,
	role TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'enabled',
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sensitive_columns (
	db TEXT NOT NULL, column_name TEXT NOT NULL, PRIMARY KEY(db, column_name)
);
CREATE TABLE IF NOT EXISTS query_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT, ts TEXT NOT NULL, api_key_name TEXT,
	db TEXT, query TEXT, row_count INTEGER, elapsed_ms INTEGER, status TEXT, error TEXT
);`
	_, err := s.db.Exec(ddl)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := s.ensureExecDSNColumn(); err != nil {
		return fmt.Errorf("migrate exec_dsn: %w", err)
	}
	return nil
}

// ensureExecDSNColumn 幂等地为 databases 表补 exec_dsn 列(旧库升级用)。
func (s *Store) ensureExecDSNColumn() error {
	rows, err := s.db.Query(`PRAGMA table_info(databases)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "exec_dsn" {
			return rows.Err() // 已存在
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE databases ADD COLUMN exec_dsn TEXT`)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) UpsertDatabase(r model.DatabaseRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO databases(name,driver,dsn,exec_dsn,description) VALUES(?,?,?,?,?)
		 ON CONFLICT(name) DO UPDATE SET driver=excluded.driver,dsn=excluded.dsn,exec_dsn=excluded.exec_dsn,description=excluded.description`,
		r.Name, r.Driver, r.DSN, r.ExecDSN, r.Description)
	return err
}

func (s *Store) ListDatabases() ([]model.DatabaseRecord, error) {
	rows, err := s.db.Query(`SELECT name,driver,dsn,COALESCE(exec_dsn,''),description FROM databases ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DatabaseRecord
	for rows.Next() {
		var r model.DatabaseRecord
		if err := rows.Scan(&r.Name, &r.Driver, &r.DSN, &r.ExecDSN, &r.Description); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDatabase(name string) error {
	_, err := s.db.Exec(`DELETE FROM databases WHERE name=?`, name)
	return err
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CountEnabledSuperAdmins() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role=? AND status='enabled'`, model.RoleSuperAdmin).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(u model.User) error {
	_, err := s.db.Exec(
		`INSERT INTO users(username,password_hash,api_key,role,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		u.Username, u.PasswordHash, u.APIKey, u.Role, u.Status, u.CreatedAt, u.UpdatedAt)
	return err
}

func (s *Store) UpdateUser(u model.User) error {
	_, err := s.db.Exec(
		`UPDATE users SET password_hash=?, api_key=?, role=?, status=?, updated_at=? WHERE username=?`,
		u.PasswordHash, u.APIKey, u.Role, u.Status, u.UpdatedAt, u.Username)
	return err
}

func (s *Store) RenameUser(oldName, newName, updatedAt string) error {
	_, err := s.db.Exec(`UPDATE users SET username=?, updated_at=? WHERE username=?`, newName, updatedAt, oldName)
	return err
}

func (s *Store) GetUserByName(username string) (model.User, bool, error) {
	return s.scanUser(`SELECT username,password_hash,api_key,role,status,created_at,updated_at FROM users WHERE username=?`, username)
}

func (s *Store) GetUserByAPIKey(key string) (model.User, bool, error) {
	return s.scanUser(`SELECT username,password_hash,api_key,role,status,created_at,updated_at FROM users WHERE api_key=?`, key)
}

func (s *Store) scanUser(q, arg string) (model.User, bool, error) {
	var u model.User
	err := s.db.QueryRow(q, arg).Scan(&u.Username, &u.PasswordHash, &u.APIKey, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return model.User{}, false, nil
	}
	if err != nil {
		return model.User{}, false, err
	}
	return u, true, nil
}

func (s *Store) ListUsers() ([]model.User, error) {
	rows, err := s.db.Query(`SELECT username,password_hash,api_key,role,status,created_at,updated_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.Username, &u.PasswordHash, &u.APIKey, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) DeleteUser(username string) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE username=?`, username)
	return err
}

// AddSensitiveColumn 幂等新增(已存在则忽略)。
func (s *Store) AddSensitiveColumn(db, column string) error {
	_, err := s.db.Exec(
		`INSERT INTO sensitive_columns(db,column_name) VALUES(?,?) ON CONFLICT DO NOTHING`,
		db, column)
	return err
}

func (s *Store) ListSensitiveColumns() ([]model.SensitiveColumn, error) {
	rows, err := s.db.Query(`SELECT db,column_name FROM sensitive_columns ORDER BY db,column_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SensitiveColumn
	for rows.Next() {
		var c model.SensitiveColumn
		if err := rows.Scan(&c.DB, &c.Column); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSensitiveColumn(db, column string) error {
	_, err := s.db.Exec(`DELETE FROM sensitive_columns WHERE db=? AND column_name=?`, db, column)
	return err
}

// ListSensitiveColumnsByDB 返回指定 db 的敏感列(走 (db,column_name) 主键索引)。
func (s *Store) ListSensitiveColumnsByDB(db string) ([]model.SensitiveColumn, error) {
	rows, err := s.db.Query(`SELECT db,column_name FROM sensitive_columns WHERE db=? ORDER BY column_name`, db)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SensitiveColumn
	for rows.Next() {
		var c model.SensitiveColumn
		if err := rows.Scan(&c.DB, &c.Column); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) InsertQueryLog(l model.QueryLog) error {
	_, err := s.db.Exec(
		`INSERT INTO query_logs(ts,api_key_name,db,query,row_count,elapsed_ms,status,error)
		 VALUES(?,?,?,?,?,?,?,?)`,
		l.TS, l.APIKeyName, l.DB, l.Query, l.RowCount, l.ElapsedMs, l.Status, l.Error)
	return err
}

func (s *Store) ListQueryLogs(f model.QueryLogFilter) ([]model.QueryLog, error) {
	q := `SELECT ts,api_key_name,db,query,row_count,elapsed_ms,status,error FROM query_logs WHERE 1=1`
	var args []any
	if f.DB != "" {
		q += ` AND db=?`
		args = append(args, f.DB)
	}
	if f.APIKey != "" {
		q += ` AND api_key_name=?`
		args = append(args, f.APIKey)
	}
	if f.Before != "" {
		q += ` AND ts<?`
		args = append(args, f.Before)
	}
	q += ` ORDER BY ts DESC, id DESC LIMIT ?`
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	} else if limit > 500 {
		limit = 500
	}
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.QueryLog
	for rows.Next() {
		var l model.QueryLog
		if err := rows.Scan(&l.TS, &l.APIKeyName, &l.DB, &l.Query, &l.RowCount, &l.ElapsedMs, &l.Status, &l.Error); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// PruneQueryLogs 删除 ts < cutoff 的记录,返回删除条数。
func (s *Store) PruneQueryLogs(cutoff string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM query_logs WHERE ts<?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
