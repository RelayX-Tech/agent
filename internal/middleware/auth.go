package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/relayx-tech/agent/internal/errs"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	HeaderSignature       = "x-relayx-signature"
	HeaderTimestamp       = "x-relayx-timestamp"
	HeaderNonce           = "x-relayx-nonce"
	HeaderContractVersion = "x-relayx-contract-version"
)

// SignatureVerifier 校验请求签名；实现由调用方注入（Ed25519）。
type SignatureVerifier interface {
	Verify(ctx context.Context, method, requestID string, timestamp time.Time, nonce string, body []byte, signature string) error
}

// AuthConfig 配置请求鉴权拦截器。
type AuthConfig struct {
	Verifier  SignatureVerifier
	ClockSkew time.Duration
	NonceTTL  time.Duration
	Now       func() time.Time
}

type nonceCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
	now     func() time.Time
}

func (c *nonceCache) seen(nonce string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for k, exp := range c.entries {
		if now.After(exp) {
			delete(c.entries, k)
		}
	}
	if exp, ok := c.entries[nonce]; ok && now.Before(exp) {
		return true
	}
	c.entries[nonce] = now.Add(c.ttl)
	return false
}

// Auth 校验签名 + 时间戳 + nonce 防重放。
func Auth(cfg AuthConfig) grpc.UnaryServerInterceptor {
	if cfg.ClockSkew <= 0 {
		cfg.ClockSkew = 5 * time.Minute
	}
	if cfg.NonceTTL <= 0 {
		cfg.NonceTTL = 10 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	cache := &nonceCache{entries: make(map[string]time.Time), ttl: cfg.NonceTTL, now: cfg.Now}

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, errs.New(errs.CodeValidationMissing, "", "缺少认证信息", false)
		}
		tsStr := firstValue(md, HeaderTimestamp)
		nonce := firstValue(md, HeaderNonce)
		signature := firstValue(md, HeaderSignature)
		if tsStr == "" || nonce == "" || signature == "" {
			return nil, errs.New(errs.CodeValidationMissing, "", "缺少认证信息", false)
		}
		ts, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			return nil, errs.New(errs.CodeProbeFailed, "", "时间戳格式非法", false)
		}
		if delta := cfg.Now().Sub(ts); delta > cfg.ClockSkew || delta < -cfg.ClockSkew {
			return nil, errs.New(errs.CodeVersionIncompatible, "", "时间戳超出允许窗口", false)
		}
		if cache.seen(nonce) {
			return nil, errs.New(errs.CodeOperationConflict, "", "nonce 重放被拒绝", false)
		}
		if cfg.Verifier != nil {
			if err := cfg.Verifier.Verify(ctx, info.FullMethod, firstValue(md, HeaderRequestID), ts, nonce, marshalBody(req), signature); err != nil {
				return nil, errs.New(errs.CodeVersionIncompatible, "", "请求签名无效", false)
			}
		}
		return handler(ctx, req)
	}
}

func firstValue(md metadata.MD, key string) string {
	if vals := md.Get(key); len(vals) > 0 {
		return vals[0]
	}
	return ""
}
