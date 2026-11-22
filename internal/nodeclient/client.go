// Package nodeclient 封装 Agent 到本机 core-node 的 Unix socket gRPC 客户端。
// 每个请求施加 deadline（默认 32s），避免 core-node 卡住时无限等待。
package nodeclient

import (
	"context"
	"net"
	"time"

	corev1 "github.com/relayx-tech/contracts/gen/go/core/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DefaultTimeout 是 Agent → core-node 的默认单节点 deadline（design §3.1）。
const DefaultTimeout = 32 * time.Second

// RuntimeClient 是 Agent 依赖的 core-node 客户端接口（测试可注入 fake）。
type RuntimeClient interface {
	Apply(ctx context.Context, req *corev1.ApplyRequest) (*corev1.ApplyResponse, error)
	Rollback(ctx context.Context, req *corev1.RollbackRequest) (*corev1.RollbackResponse, error)
	KernelStatus(ctx context.Context, req *corev1.KernelStatusRequest) (*corev1.KernelStatusResponse, error)
	Perception(ctx context.Context, req *corev1.PerceptionRequest) (*corev1.PerceptionResponse, error)
	Version(ctx context.Context, req *corev1.VersionInfoRequest) (*corev1.VersionInfoResponse, error)
	Health(ctx context.Context, req *corev1.HealthCheckRequest) (*corev1.HealthCheckResponse, error)
	Close() error
}

// Client 是 RuntimeClient 的真实实现。
type Client struct {
	conn    *grpc.ClientConn
	rt      corev1.NodeRuntimeServiceClient
	ver     corev1.VersionServiceClient
	hc      corev1.HealthServiceClient
	timeout time.Duration
}

// Dial 通过 Unix socket 连接本机 core-node（默认 32s deadline）。
func Dial(socketPath string) (*Client, error) {
	return DialWithTimeout(socketPath, DefaultTimeout)
}

// DialWithTimeout 通过 Unix socket 连接并指定每请求 deadline。
func DialWithTimeout(socketPath string, timeout time.Duration) (*Client, error) {
	conn, err := grpc.NewClient(
		"passthrough:///core-node",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		}),
	)
	if err != nil {
		return nil, err
	}
	return NewClient(conn, timeout), nil
}

// NewClient 从已有连接构造（测试注入 bufconn）。
func NewClient(conn *grpc.ClientConn, timeout time.Duration) *Client {
	return &Client{
		conn:    conn,
		rt:      corev1.NewNodeRuntimeServiceClient(conn),
		ver:     corev1.NewVersionServiceClient(conn),
		hc:      corev1.NewHealthServiceClient(conn),
		timeout: timeout,
	}
}

// withDeadline 为请求附加 deadline。
func (c *Client) withDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.timeout)
}

func (c *Client) Apply(ctx context.Context, req *corev1.ApplyRequest) (*corev1.ApplyResponse, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	return c.rt.Apply(ctx, req)
}

func (c *Client) Rollback(ctx context.Context, req *corev1.RollbackRequest) (*corev1.RollbackResponse, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	return c.rt.Rollback(ctx, req)
}

func (c *Client) KernelStatus(ctx context.Context, req *corev1.KernelStatusRequest) (*corev1.KernelStatusResponse, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	return c.rt.KernelStatus(ctx, req)
}

func (c *Client) Perception(ctx context.Context, req *corev1.PerceptionRequest) (*corev1.PerceptionResponse, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	return c.rt.Perception(ctx, req)
}

func (c *Client) Version(ctx context.Context, req *corev1.VersionInfoRequest) (*corev1.VersionInfoResponse, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	return c.ver.Info(ctx, req)
}

func (c *Client) Health(ctx context.Context, req *corev1.HealthCheckRequest) (*corev1.HealthCheckResponse, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	return c.hc.Check(ctx, req)
}

func (c *Client) Close() error { return c.conn.Close() }
