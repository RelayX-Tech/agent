package middleware_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/relayx-tech/agent/internal/middleware"
	"google.golang.org/grpc/metadata"
)

func TestEd25519SignVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	v, err := middleware.NewEd25519Verifier(pub)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	body := []byte(`{"a":1}`)
	sig, err := middleware.Ed25519Sign(priv, "/svc/M", "rid", now, "nonce", body)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(context.Background(), "/svc/M", "rid", now, "nonce", body, sig); err != nil {
		t.Fatalf("正确签名应通过: %v", err)
	}
	if err := v.Verify(context.Background(), "/svc/M", "rid", now, "nonce", []byte(`{"a":2}`), sig); err == nil {
		t.Fatal("篡改 body 应失败")
	}
}

func TestAuthWithEd25519(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	v, err := middleware.NewEd25519Verifier(pub)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	ic := middleware.Auth(middleware.AuthConfig{
		Verifier:  v,
		ClockSkew: time.Minute,
		NonceTTL:  time.Minute,
		Now:       func() time.Time { return now },
	})
	handler := func(context.Context, any) (any, error) { return nil, nil }

	sig, err := middleware.Ed25519Sign(priv, "/test.Service/Method", "rid-1", now, "nonce-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		middleware.HeaderTimestamp, now.Format(time.RFC3339Nano),
		middleware.HeaderNonce, "nonce-1",
		middleware.HeaderSignature, sig,
		middleware.HeaderRequestID, "rid-1",
	))
	if _, err := ic(ctx, nil, info(), handler); err != nil {
		t.Fatalf("正确签名应通过: %v", err)
	}

	badCtx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		middleware.HeaderTimestamp, now.Format(time.RFC3339Nano),
		middleware.HeaderNonce, "nonce-2",
		middleware.HeaderSignature, "bad-signature",
		middleware.HeaderRequestID, "rid-1",
	))
	if _, err := ic(badCtx, nil, info(), handler); err == nil {
		t.Fatal("错误签名应拒绝")
	}
}
