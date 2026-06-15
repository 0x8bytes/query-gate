package guard

import (
	"reflect"
	"testing"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"逗号点括号粘连", "SELECT t.mobile,name FROM(u)", []string{"select", "t", "mobile", "name", "from", "u"}},
		{"反引号标识符", "SELECT `mobile` FROM t", []string{"select", "mobile", "from", "t"}},
		{"子查询别名", "SELECT m FROM (SELECT mobile AS m FROM u) x", []string{"select", "m", "from", "select", "mobile", "as", "m", "from", "u", "x"}},
		{"字符串字面量剥除", "SELECT id FROM t WHERE name='mobile'", []string{"select", "id", "from", "t", "where", "name"}},
		{"块注释剥除", "SELECT/**/mobile FROM t", []string{"select", "mobile", "from", "t"}},
		{"行注释剥除", "SELECT id -- mobile\nFROM t", []string{"select", "id", "from", "t"}},
		{"转小写", "SELECT MOBILE FROM T", []string{"select", "mobile", "from", "t"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Tokenize(c.sql)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Tokenize(%q) = %v, want %v", c.sql, got, c.want)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	g := New([]string{"mobile", "id_card"}) // 敏感列
	cases := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{"正常 select 放行", "SELECT id, name FROM users WHERE id=1", false},
		{"禁裸星号", "SELECT * FROM users", true},
		{"禁表点星号", "SELECT u.* FROM users u", true},
		{"敏感列直查", "SELECT mobile FROM users", true},
		{"敏感列子查询", "SELECT m FROM (SELECT mobile AS m FROM u) t", true},
		{"敏感列逗号粘连", "SELECT id,mobile FROM u", true},
		{"敏感列点粘连", "SELECT t.mobile FROM u t", true},
		{"危险词 sleep", "SELECT sleep(5)", true},
		{"危险词 load_file", "SELECT load_file('/etc/passwd')", true},
		{"危险词 outfile", "SELECT id FROM t INTO OUTFILE '/x'", true},
		{"多语句", "SELECT id FROM t; DROP TABLE t", true},
		{"首词非只读", "UPDATE t SET x=1", true},
		{"with 放行", "WITH c AS (SELECT id FROM t) SELECT id FROM c", false},
		{"show 拒绝", "SHOW TABLES", true},
		{"describe 拒绝", "DESCRIBE users", true},
		{"explain select 拒绝", "EXPLAIN SELECT id FROM users", true},
		{"explain update 拒绝", "EXPLAIN UPDATE users SET name='x'", true},
		{"insert 拒绝", "INSERT INTO users(id) VALUES(1)", true},
		{"delete 拒绝", "DELETE FROM users WHERE id=1", true},
		{"create 拒绝", "CREATE TABLE x(id int)", true},
		{"alter 拒绝", "ALTER TABLE users ADD COLUMN x int", true},
		{"drop 拒绝", "DROP TABLE users", true},
		{"grant 拒绝", "GRANT SELECT ON db.* TO u", true},
		{"set 拒绝", "SET role admin", true},
		{"call 拒绝", "CALL dangerous_proc()", true},
		{"lock 拒绝", "LOCK TABLE users WRITE", true},
		{"begin 拒绝", "BEGIN", true},
		{"commit 拒绝", "COMMIT", true},
		{"rollback 拒绝", "ROLLBACK", true},
		{"with delete 拒绝", "WITH x AS (DELETE FROM users RETURNING id) SELECT id FROM x", true},
		{"pg_sleep 拒绝", "SELECT pg_sleep(10)", true},
		{"explain analyze 拒绝", "EXPLAIN ANALYZE SELECT id FROM users", true},
		{"敏感词在字符串里不误伤", "SELECT id FROM t WHERE note='mobile'", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := g.Check(c.sql)
			if (err != nil) != c.wantErr {
				t.Fatalf("Check(%q) err=%v, wantErr=%v", c.sql, err, c.wantErr)
			}
		})
	}
}

func TestCheck_BackslashEscapeNoLeak(t *testing.T) {
	g := New([]string{"mobile"})
	cases := []string{
		`SELECT name FROM users WHERE bio='O\'Hara' AND mobile LIKE '%5%'`,
		`SELECT id FROM t WHERE note='I sell mobile\'s' AND mobile=1`,
	}
	for _, sql := range cases {
		if err := g.Check(sql); err == nil {
			t.Fatalf("expected reject (mobile referenced), got nil for: %s", sql)
		}
	}
}

func TestCheck_NonASCIISensitiveColumn(t *testing.T) {
	g := New([]string{"手机号"})
	if err := g.Check("SELECT id, 手机号 FROM users"); err == nil {
		t.Fatal("expected reject: 手机号 is a sensitive column referenced in query")
	}
	// 不含敏感列应放行
	if err := g.Check("SELECT id, name FROM users"); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestCheckMongo(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		sensitive []string
		wantErr   bool
	}{
		{"普通 find 放行", `{"collection":"orders","find":{"status":"active"}}`, nil, false},
		{"aggregate pipeline 放行", `{"collection":"orders","pipeline":[{"$match":{"a":1}}]}`, nil, false},
		{"lookup 联表放行", `{"collection":"orders","pipeline":[{"$lookup":{"from":"users","localField":"user_id","foreignField":"_id","as":"user"}}]}`, nil, false},
		{"$out 写拦截", `{"collection":"o","pipeline":[{"$out":"dump"}]}`, nil, true},
		{"$merge 写拦截", `{"collection":"o","pipeline":[{"$merge":{"into":"x"}}]}`, nil, true},
		{"敏感列拦截", `{"collection":"users","find":{"mobile":"139"}}`, []string{"mobile"}, true},
		{"drop 写拦截", `{"collection":"users","drop":true}`, nil, true},
		{"find where 拦截", `{"collection":"users","find":{"$where":"function(){return true}"}}`, nil, true},
		{"pipeline function 拦截", `{"collection":"users","pipeline":[{"$project":{"x":{"$function":{"body":"function(){return 1}","args":[],"lang":"js"}}}}]}`, nil, true},
		{"find pipeline 同时出现拒绝", `{"collection":"users","find":{},"pipeline":[{"$match":{}}]}`, nil, true},
		{"缺 collection 拒绝", `{"find":{}}`, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckMongo(c.query, c.sensitive)
			if (err != nil) != c.wantErr {
				t.Fatalf("CheckMongo(%q) err=%v wantErr=%v", c.query, err, c.wantErr)
			}
		})
	}
}
