package nodeclient_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/relayx-tech/agent/internal/nodeclient"
	corev1 "github.com/relayx-tech/contracts/gen/go/core/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type slowRuntime struct {
	corev1.UnimplementedNodeRuntimeServiceServer
	delay time.Duration
}

func (s slowRuntime) Apply(ctx context.Context, _ *corev1.ApplyRequest) (*corev1.ApplyResponse, error) {
	select {
	case <-time.After(s.delay):
		return &corev1.ApplyResponse{Accepted: true}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func dialBufconn(t *testing.T, delay time.Duration) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	corev1.RegisterNodeRuntimeServiceServer(srv, slowRuntime{delay: delay})
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
	})
	return conn
}

// BE-26：超过每请求 deadline 返回 DeadlineExceeded。
func TestApplyDeadlineExceeded(t *testing.T) {
	conn := dialBufconn(t, 500*time.Millisecond)
	client := nodeclient.NewClient(conn, 50*time.Millisecond)

	_, err := client.Apply(context.Background(), &corev1.ApplyRequest{})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("code = %v, 期望 DeadlineExceeded", status.Code(err))
	}
}

// BE-26：充足 deadline 时正常返回。
func TestApplyWithinDeadline(t *testing.T) {
	conn := dialBufconn(t, 10*time.Millisecond)
	client := nodeclient.NewClient(conn, 2*time.Second)

	resp, err := client.Apply(context.Background(), &corev1.ApplyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.GetAccepted() {
		t.Fatal("应返回 accepted")
	}
}
