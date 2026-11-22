//go:build !linux && !windows

package status

// 其他平台（如 darwin）暂不采集；返回 0，保持契约字段存在。
func diskFreeBytes() uint64    { return 0 }
func loadAverageCenti() uint64 { return 0 }
func cpuPercent() float64      { return 0 }
