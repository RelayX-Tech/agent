//go:build windows

package status

import "golang.org/x/sys/windows"

func diskFreeBytes() uint64 {
	path, err := windows.UTF16PtrFromString(`C:\`)
	if err != nil {
		return 0
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(path, &free, &total, &totalFree); err != nil {
		return 0
	}
	return free
}

// Windows 无标准负载均值 / 轻量 CPU 采样，返回 0；生产（Linux）由 resources_linux.go 填充。
func loadAverageCenti() uint64 { return 0 }

func cpuPercent() float64 { return 0 }
