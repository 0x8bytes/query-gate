//go:build integration

package mysql

import (
	"context"
	"testing"
	"time"

	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
)

func startMySQL(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcmysql.Run(ctx, "mysql:8.0",
		tcmysql.WithDatabase("testdb"),
		tcmysql.WithUsername("root"),
		tcmysql.WithPassword("rootpw"),
		tcmysql.WithScripts(),
	)
	if err != nil {
		t.Skipf("cannot start mysql container (docker required): %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}

func TestMySQL_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration in -short")
	}
	dsn := startMySQL(t)
	d, err := New("prod", dsn, "测试库")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := d.db.ExecContext(ctx,
		"CREATE TABLE orders (id INT PRIMARY KEY, amount DECIMAL(10,2))"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.ExecContext(ctx, "INSERT INTO orders VALUES (1, 99.50)"); err != nil {
		t.Fatal(err)
	}

	tables, err := d.Tables(ctx)
	if err != nil || len(tables) != 1 || tables[0].Name != "orders" {
		t.Fatalf("Tables = %v, err = %v", tables, err)
	}

	ddl, notFound, err := d.Schema(ctx, []string{"orders", "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ddl["orders"]; !ok {
		t.Errorf("ddl missing orders: %v", ddl)
	}
	if len(notFound) != 1 || notFound[0] != "nope" {
		t.Errorf("notFound = %v, want [nope]", notFound)
	}

	res, err := d.Query(ctx, "SELECT id, amount FROM orders", 0, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if res.RowCount != 1 || res.Columns[0] != "id" {
		t.Errorf("query result = %+v", res)
	}

	if _, err := d.db.ExecContext(ctx, "INSERT INTO orders VALUES (2, 1.00)"); err != nil {
		t.Fatal(err)
	}
	res, err = d.Query(ctx, "SELECT * FROM orders", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || res.RowCount != 1 {
		t.Errorf("truncation failed: %+v", res)
	}
}
