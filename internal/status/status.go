// Package status 采集 Agent 侧资源与网卡信息。
package status

import (
	"net"
	"runtime"

	agentv1 "github.com/relayx-tech/contracts/gen/go/agent/v1"
)

// Interfaces 返回节点网卡清单。
func Interfaces() []*agentv1.NetworkInterface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]*agentv1.NetworkInterface, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs := make([]string, 0)
		if ifaceAddrs, err := iface.Addrs(); err == nil {
			for _, a := range ifaceAddrs {
				addrs = append(addrs, a.String())
			}
		}
		out = append(out, &agentv1.NetworkInterface{
			Name:      iface.Name,
			Mac:       iface.HardwareAddr.String(),
			Mtu:       uint32(iface.MTU),
			Up:        iface.Flags&net.FlagUp != 0,
			Addresses: addrs,
		})
	}
	return out
}

// Resources 采集进程 / 主机资源；平台相关项由 resources_<os>.go 提供。
// load_average 以「负载 ×100」的定点整数表示（避免浮点丢精度）。
func Resources() *agentv1.ResourceStatus {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return &agentv1.ResourceStatus{
		CpuPercent:    cpuPercent(),
		MemoryBytes:   m.Alloc,
		DiskFreeBytes: diskFreeBytes(),
		LoadAverage:   loadAverageCenti(),
	}
}
