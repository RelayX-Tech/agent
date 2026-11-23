package middleware_test

import (
	"context"
	"testing"
	"time"

	"github.com/relayx-tech/agent/internal/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func info() *grpc.UnaryServerInfo { return &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"} }

func TestAuthMissingMetadata(t *testing.T) {
	_, err := middleware.Auth(middleware.AuthConfig{})(context.Background(), nil, info(), func(ctx context.Context, req any) (any, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("缺少认证信息应拒绝")
	}
}

func TestAuthValidAndReplay(t *testing.T) {
	now := time.Unix(1000, 0)
	ic := middleware.Auth(middleware.AuthConfig{
		ClockSkew: time.Minute,
		NonceTTL:  time.Minute,
		Now:       func() time.Time { return now },
	})
	md := metadata.Pairs(
		middleware.HeaderTimestamp, now.Format(time.RFC3339Nano),
		middleware.HeaderNonce, "nonce-1",
		middleware.HeaderSignature, "sig",
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	calls := 0
	handler := func(ctx context.Context, req any) (any, error) { calls++; return nil, nil }

	if _, err := ic(ctx, nil, info(), handler); err != nil {
		t.Fatalf("首次应通过: %v", err)
	}
	if _, err := ic(ctx, nil, info(), handler); err == nil {
		t.Fatal("重复 nonce 应拒绝")
	}
	if calls != 1 {
		t.Fatalf("handler 调用 %d 次, 期望 1", calls)
	}
}
