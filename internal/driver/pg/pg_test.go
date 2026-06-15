package pg

import "testing"

// 无真实 PG 时只测纯逻辑:Info 返回正确 driver 名。
func TestDriver_Info(t *testing.T) {
	d := &Driver{name: "mypg", desc: "test"}
	info := d.Info()
	if info.Driver != "postgres" || info.Name != "mypg" {
		t.Fatalf("Info = %+v", info)
	}
}
