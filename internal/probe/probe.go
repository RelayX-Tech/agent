// Package probe 实现 Agent 本地 TCP / HTTP 探测（Panel 发起，Agent 执行）。
package probe

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// TCP 探测 host:port，返回成功、延迟（毫秒）、错误信息。
func TCP(ctx context.Context, target string, timeout time.Duration) (bool, uint32, string) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", target)
	if err != nil {
		return false, uint32(time.Since(start).Milliseconds()), err.Error()
	}
	_ = conn.Close()
	return true, uint32(time.Since(start).Milliseconds()), ""
}

// HTTP 探测 URL，返回成功、延迟（毫秒）、状态码、错误信息。
func HTTP(ctx context.Context, target string, timeout time.Duration) (bool, uint32, int32, string) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // 仅探测可达性
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, 0, 0, err.Error()
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return false, uint32(time.Since(start).Milliseconds()), 0, err.Error()
	}
	defer resp.Body.Close()
	ok := resp.StatusCode >= 200 && resp.StatusCode < 400
	return ok, uint32(time.Since(start).Milliseconds()), int32(resp.StatusCode), ""
}
