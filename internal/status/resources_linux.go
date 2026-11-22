//go:build linux

package status

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func diskFreeBytes() uint64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}

// loadAverageCenti 返回 1 分钟负载 ×100。
func loadAverageCenti() uint64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || v < 0 {
		return 0
	}
	return uint64(v * 100)
}

// cpuPercent 通过两次采样 /proc/self/stat 估算进程 CPU 使用率。
func cpuPercent() float64 {
	u1, s1 := readSelfStat()
	start := time.Now()
	time.Sleep(50 * time.Millisecond)
	u2, s2 := readSelfStat()
	wall := time.Since(start).Seconds()
	if wall <= 0 {
		return 0
	}
	const clockTicks = 100.0 // SC_CLK_TCK 常见值
	cpuSeconds := float64((u2+s2)-(u1+s1)) / clockTicks
	return cpuSeconds / wall * 100
}

func readSelfStat() (utime, stime uint64) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, 0
	}
	content := string(data)
	idx := strings.LastIndex(content, ")")
	if idx < 0 || idx+2 >= len(content) {
		return 0, 0
	}
	fields := strings.Fields(content[idx+2:])
	// fields[0]=state(3) ... utime 为第 14 字段 → fields[11]，stime 第 15 → fields[12]
	if len(fields) < 13 {
		return 0, 0
	}
	u, _ := strconv.ParseUint(fields[11], 10, 64)
	s, _ := strconv.ParseUint(fields[12], 10, 64)
	return u, s
}
