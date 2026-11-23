package nodeclient_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/relayx-tech/agent/internal/nodeclient"
	corev1 "github.com/relayx-tech/contracts/gen/go/core/v1"
	"google.golang.org/grpc"
)

type unixRuntime struct {
	corev1.UnimplementedNodeRuntimeServiceServer
}

type unixVersion struct {
	corev1.UnimplementedVersionServiceServer
}

func (unixVersion) Info(context.Context, *corev1.VersionInfoRequest) (*corev1.VersionInfoResponse, error) {
	return &corev1.VersionInfoResponse{Version: &corev1.BuildVersion{
		CoreVersion: "1.0.0", ContractVersion: "1.0.0", XrayVersion: "26.11.9",
	}}, nil
}

// AC2 / AC44：Agent 通过真实 Unix socket gRPC 调 core-node（非 bufconn、非 HTTP）。
func TestDialUnixSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "rx")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "n.sock")

	lis, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("当前环境不支持 Unix socket: %v", err)
	}
	srv := grpc.NewServer()
	corev1.RegisterNodeRuntimeServiceServer(srv, unixRuntime{})
	corev1.RegisterVersionServiceServer(srv, unixVersion{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	c, err := nodeclient.DialWithTimeout(sock, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := c.Version(ctx, &corev1.VersionInfoRequest{})
	if err != nil {
		t.Fatalf("Unix socket gRPC 调用失败: %v", err)
	}
	if resp.GetVersion().GetXrayVersion() != "26.11.9" {
		t.Fatalf("version = %+v", resp.GetVersion())
	}
}
