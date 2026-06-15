package mongo

import "testing"

func TestDriver_Info(t *testing.T) {
	d := &Driver{name: "mymongo", desc: "test"}
	if info := d.Info(); info.Driver != "mongodb" || info.Name != "mymongo" {
		t.Fatalf("Info = %+v", info)
	}
}

func TestParseQuery_Pipeline(t *testing.T) {
	q, err := parseQuery(`{"collection":"orders","pipeline":[{"$match":{"a":1}}]}`)
	if err != nil || q.Collection != "orders" || len(q.Pipeline) != 1 {
		t.Fatalf("pipeline parse: %+v err=%v", q, err)
	}
}

func TestParseQuery_Find(t *testing.T) {
	q2, err := parseQuery(`{"collection":"users","find":{"age":18}}`)
	if err != nil || q2.Collection != "users" || q2.Find == nil {
		t.Fatalf("find parse: %+v err=%v", q2, err)
	}
}

func TestParseQuery_MongoshError(t *testing.T) {
	if _, err := parseQuery(`db.users.find()`); err == nil {
		t.Fatal("mongosh 原生语句本期应报错(需 JSON)")
	}
}

func TestParseQuery_MissingCollection(t *testing.T) {
	if _, err := parseQuery(`{"find":{}}`); err == nil {
		t.Fatal("缺 collection 应报错")
	}
}

func TestDatabaseFromURI_WithDB(t *testing.T) {
	// mongodb://host:27017/mydb → mydb
	if got := databaseFromURI("mongodb://h:27017/mydb"); got != "mydb" {
		t.Fatalf("got %q want %q", got, "mydb")
	}
}

func TestDatabaseFromURI_WithAuthAndQuery(t *testing.T) {
	// mongodb://u:p@h:27017/mydb?x=1 → mydb
	if got := databaseFromURI("mongodb://u:p@h:27017/mydb?x=1"); got != "mydb" {
		t.Fatalf("got %q want %q", got, "mydb")
	}
}

func TestDatabaseFromURI_NoDB(t *testing.T) {
	// mongodb://h:27017 → ""
	if got := databaseFromURI("mongodb://h:27017"); got != "" {
		t.Fatalf("got %q want %q", got, "")
	}
}

func TestDatabaseFromURI_SRV(t *testing.T) {
	// mongodb+srv://h/mydb → mydb
	if got := databaseFromURI("mongodb+srv://h/mydb"); got != "mydb" {
		t.Fatalf("got %q want %q", got, "mydb")
	}
}
