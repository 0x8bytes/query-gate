package data

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/0x8bytes/query-gate/internal/model"
)

func TestOpen_CreatesSchemaIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	// 再开一次同文件,不应报错(建表用 IF NOT EXISTS)
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	s2.Close()
}

// TestOpen_UpgradesLegacyDatabasesTable 模拟旧库(databases 表无 exec_dsn 列):
// Open 应幂等补列,历史行经 COALESCE 读回 ExecDSN="",重复 Open 不报 duplicate column。
func TestOpen_UpgradesLegacyDatabasesTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	// 1) 手工建一个无 exec_dsn 列的旧表 + 1 条历史行。
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(
		`CREATE TABLE databases (name TEXT PRIMARY KEY, driver TEXT NOT NULL, dsn TEXT NOT NULL, description TEXT);
		 INSERT INTO databases(name,driver,dsn,description) VALUES('old','mysql','read-dsn','d');`); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	_ = raw.Close()

	// 2) Open 升级:补 exec_dsn 列,历史行可读且 ExecDSN 为空。
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	recs, err := s.ListDatabases()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(recs) != 1 || recs[0].Name != "old" || recs[0].DSN != "read-dsn" || recs[0].ExecDSN != "" {
		t.Fatalf("legacy row wrong: %+v", recs)
	}
	s.Close()

	// 3) 再次 Open 同库:列已存在,不应报 duplicate column。
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen upgraded: %v", err)
	}
	s2.Close()
}

func TestDatabases_UpsertListDelete(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rec := model.DatabaseRecord{Name: "prod", Driver: "mysql", DSN: "dsn1", Description: "p"}
	if err := s.UpsertDatabase(rec); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	rec.DSN = "dsn2"
	if err := s.UpsertDatabase(rec); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	list, err := s.ListDatabases()
	if err != nil || len(list) != 1 || list[0].DSN != "dsn2" {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	if err := s.DeleteDatabase("prod"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if list, _ := s.ListDatabases(); len(list) != 0 {
		t.Fatalf("after delete len = %d", len(list))
	}
}

func TestSensitiveColumns_CRUD(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.AddSensitiveColumn("crm", "mobile"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.AddSensitiveColumn("crm", "mobile"); err != nil { // 幂等
		t.Fatalf("add dup: %v", err)
	}
	if err := s.AddSensitiveColumn("crm", "id_card"); err != nil {
		t.Fatalf("add2: %v", err)
	}
	list, err := s.ListSensitiveColumns()
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	if err := s.DeleteSensitiveColumn("crm", "mobile"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if list, _ := s.ListSensitiveColumns(); len(list) != 1 {
		t.Fatalf("after delete len = %d", len(list))
	}
}

func TestSensitiveColumns_ByDB(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.AddSensitiveColumn("crm", "mobile"); err != nil {
		t.Fatalf("add crm/mobile: %v", err)
	}
	if err := s.AddSensitiveColumn("crm", "id_card"); err != nil {
		t.Fatalf("add crm/id_card: %v", err)
	}
	if err := s.AddSensitiveColumn("other", "x"); err != nil {
		t.Fatalf("add other/x: %v", err)
	}

	crm, err := s.ListSensitiveColumnsByDB("crm")
	if err != nil {
		t.Fatalf("ListSensitiveColumnsByDB crm: %v", err)
	}
	if len(crm) != 2 {
		t.Fatalf("crm 期望 2 条，got %d: %+v", len(crm), crm)
	}

	none, err := s.ListSensitiveColumnsByDB("none")
	if err != nil {
		t.Fatalf("ListSensitiveColumnsByDB none: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("none 期望 0 条，got %d: %+v", len(none), none)
	}
}

func TestQueryLogs_InsertListPrune(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	old := model.QueryLog{TS: "2026-01-01T00:00:00Z", APIKeyName: "a", DB: "prod", Query: "SELECT id FROM t", RowCount: 1, ElapsedMs: 5, Status: "ok"}
	recent := model.QueryLog{TS: "2026-06-14T00:00:00Z", APIKeyName: "a", DB: "prod", Query: "SELECT id FROM u", RowCount: 2, ElapsedMs: 7, Status: "ok"}
	if err := s.InsertQueryLog(old); err != nil {
		t.Fatalf("insert old: %v", err)
	}
	if err := s.InsertQueryLog(recent); err != nil {
		t.Fatalf("insert recent: %v", err)
	}
	logs, err := s.ListQueryLogs(model.QueryLogFilter{Limit: 10})
	if err != nil || len(logs) != 2 || logs[0].Query != "SELECT id FROM u" {
		t.Fatalf("list = %+v err=%v", logs, err)
	}
	n, err := s.PruneQueryLogs("2026-02-01T00:00:00Z")
	if err != nil || n != 1 {
		t.Fatalf("prune n=%d err=%v", n, err)
	}
	if logs, _ := s.ListQueryLogs(model.QueryLogFilter{Limit: 10}); len(logs) != 1 {
		t.Fatalf("after prune len = %d", len(logs))
	}
}

func TestUsers_CRUD(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if n, _ := st.CountUsers(); n != 0 {
		t.Fatalf("want 0 users, got %d", n)
	}
	u := model.User{Username: "alice", PasswordHash: "h", APIKey: "qn_a", Role: model.RoleSuperAdmin, Status: "enabled", CreatedAt: "t", UpdatedAt: "t"}
	if err := st.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.CountUsers(); n != 1 {
		t.Fatalf("want 1, got %d", n)
	}
	if err := st.CreateUser(u); err == nil {
		t.Fatal("duplicate username must error")
	}
	got, ok, _ := st.GetUserByName("alice")
	if !ok || got.APIKey != "qn_a" || got.Role != model.RoleSuperAdmin {
		t.Fatalf("GetUserByName mismatch: %+v ok=%v", got, ok)
	}
	byKey, ok, _ := st.GetUserByAPIKey("qn_a")
	if !ok || byKey.Username != "alice" {
		t.Fatalf("GetUserByAPIKey mismatch: %+v ok=%v", byKey, ok)
	}
	list, _ := st.ListUsers()
	if len(list) != 1 {
		t.Fatalf("want 1 in list, got %d", len(list))
	}
	u.PasswordHash = "h2"
	u.Role = model.RoleUser
	u.Status = "disabled"
	u.UpdatedAt = "t2"
	if err := st.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	got2, _, _ := st.GetUserByName("alice")
	if got2.PasswordHash != "h2" || got2.Role != model.RoleUser || got2.Status != "disabled" {
		t.Fatalf("UpdateUser not applied: %+v", got2)
	}
	if err := st.RenameUser("alice", "alice2", "t3"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.GetUserByName("alice2"); !ok {
		t.Fatal("rename failed")
	}
	if err := st.DeleteUser("alice2"); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.CountUsers(); n != 0 {
		t.Fatalf("want 0 after delete, got %d", n)
	}
}

func TestUpsertDatabase_ExecDSNRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	if err := st.UpsertDatabase(model.DatabaseRecord{
		Name: "mydb", Driver: "mysql", DSN: "read-dsn", ExecDSN: "write-dsn", Description: "d",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	recs, err := st.ListDatabases()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(recs) != 1 || recs[0].ExecDSN != "write-dsn" || recs[0].DSN != "read-dsn" {
		t.Fatalf("round-trip wrong: %+v", recs)
	}
}

func TestCountEnabledSuperAdmins(t *testing.T) {
	st, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	_ = st.CreateUser(model.User{Username: "a", APIKey: "k1", Role: model.RoleSuperAdmin, Status: "enabled", CreatedAt: "t", UpdatedAt: "t"})
	_ = st.CreateUser(model.User{Username: "b", APIKey: "k2", Role: model.RoleUser, Status: "enabled", CreatedAt: "t", UpdatedAt: "t"})
	n, _ := st.CountEnabledSuperAdmins()
	if n != 1 {
		t.Fatalf("want 1 enabled super_admin, got %d", n)
	}
}
