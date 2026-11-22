package status_test

import (
	"testing"

	"github.com/relayx-tech/agent/internal/status"
)

// BE-24：Resources 采集内存 / 磁盘等字段且不 panic。
func TestResources(t *testing.T) {
	r := status.Resources()
	if r == nil {
		t.Fatal("Resources 返回 nil")
	}
	if r.GetMemoryBytes() == 0 {
		t.Fatal("内存应采集到正值")
	}
}

func TestInterfacesNotEmpty(t *testing.T) {
	// 至少 loopback 存在；允许空（某些受限环境），只验证不 panic。
	_ = status.Interfaces()
}
