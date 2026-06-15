package driver

import (
	"fmt"
	"sync"

	"github.com/0x8bytes/query-gate/internal/model"
)

// Source 标注一个实例的来源:YAML 种子(只读)或运行时动态添加。
type Source int

const (
	SourceSeed Source = iota
	SourceDynamic
)

// Registry 持有别名 → Driver 的映射,并发安全(运行时可增删)。
type Registry struct {
	mu      sync.RWMutex
	drivers map[string]Driver
	source  map[string]Source
	order   []string
}

func NewRegistry() *Registry {
	return &Registry{drivers: map[string]Driver{}, source: map[string]Source{}}
}

func (r *Registry) Register(name string, d Driver, src Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.drivers[name]; !exists {
		r.order = append(r.order, name)
	}
	r.drivers[name] = d
	r.source[name] = src
}

// Unregister 移除一个实例并关闭其连接。不存在返回错误。
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.drivers[name]
	if !ok {
		return fmt.Errorf("database %q not found", name)
	}
	_ = d.Close()
	delete(r.drivers, name)
	delete(r.source, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	return nil
}

func (r *Registry) Get(name string) (Driver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.drivers[name]
	return d, ok
}

// Source 返回实例来源。
func (r *Registry) Source(name string) (Source, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.source[name]
	return s, ok
}

// List 按注册顺序返回所有实例信息。
func (r *Registry) List() []model.DatabaseInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.DatabaseInfo, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.drivers[name].Info())
	}
	return out
}

// Close 关闭所有 driver。
func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.drivers {
		_ = d.Close()
	}
}
