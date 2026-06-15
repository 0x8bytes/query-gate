package driver

import (
	"context"
	"testing"

	"github.com/0x8bytes/query-gate/internal/model"
)

type fakeDriver struct{ name string }

func (f *fakeDriver) Info() model.DatabaseInfo {
	return model.DatabaseInfo{Name: f.name, Driver: "fake"}
}
func (f *fakeDriver) Tables(context.Context) ([]model.TableInfo, error) { return nil, nil }
func (f *fakeDriver) Schema(context.Context, []string) (map[string]string, []string, error) {
	return nil, nil, nil
}
func (f *fakeDriver) Query(context.Context, string, int, int) (*model.QueryResult, error) {
	return nil, nil
}
func (f *fakeDriver) Exec(context.Context, string) (int64, error) { return 0, nil }
func (f *fakeDriver) Close() error                                { return nil }

func TestRegistry_GetAndList(t *testing.T) {
	r := NewRegistry()
	r.Register("prod", &fakeDriver{name: "prod"}, SourceSeed)
	r.Register("staging", &fakeDriver{name: "staging"}, SourceDynamic)

	d, ok := r.Get("prod")
	if !ok || d.Info().Name != "prod" {
		t.Fatalf("Get(prod) failed: %v %v", d, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("Get(missing) should be false")
	}
	if got := len(r.List()); got != 2 {
		t.Fatalf("List len = %d, want 2", got)
	}
}

func TestRegistry_RegisterUnregisterSource(t *testing.T) {
	r := NewRegistry()
	r.Register("seed1", &fakeDriver{name: "seed1"}, SourceSeed)
	r.Register("dyn1", &fakeDriver{name: "dyn1"}, SourceDynamic)

	if src, ok := r.Source("seed1"); !ok || src != SourceSeed {
		t.Fatalf("Source(seed1) = %v %v", src, ok)
	}
	if err := r.Unregister("dyn1"); err != nil {
		t.Fatalf("unregister dyn1: %v", err)
	}
	if _, ok := r.Get("dyn1"); ok {
		t.Fatal("dyn1 should be gone")
	}
	if err := r.Unregister("nope"); err == nil {
		t.Fatal("unregister missing should error")
	}
}
