// Package middleware 提供 agent 侧一元拦截器（Recovery / RequestID / AccessLog）。
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/relayx-tech/agent/internal/errs"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const HeaderRequestID = "x-request-id"

type requestIDKey struct{}

// Chain 按声明顺序组合一元拦截器（interceptors[0] 最外层）。
func Chain(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		h := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			ic := interceptors[i]
			next := h
			h = func(ctx context.Context, req any) (any, error) {
				return ic(ctx, req, info, next)
			}
		}
		return h(ctx, req)
	}
}

// RequestID 生成 / 透传 request_id 并回写响应头。
func RequestID() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		rid := ""
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get(HeaderRequestID); len(vals) > 0 {
				rid = vals[0]
			}
		}
		if rid == "" {
			b := make([]byte, 16)
			_, _ = rand.Read(b)
			rid = hex.EncodeToString(b)
		}
		ctx = context.WithValue(ctx, requestIDKey{}, rid)
		resp, err := handler(ctx, req)
		if err == nil {
			_ = grpc.SetHeader(ctx, metadata.Pairs(HeaderRequestID, rid))
		}
		return resp, err
	}
}

// RequestIDFromContext 读取 request_id。
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// Recovery 捕获 panic 转为 INTERNAL。
func Recovery() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logx.WithContext(ctx).Errorw("panic recovered", logx.Field("method", info.FullMethod), logx.Field("panic", r))
				resp = nil
				err = errs.New(errs.CodeInternal, "", "服务内部错误", false)
			}
		}()
		return handler(ctx, req)
	}
}

// AccessLog 记录 method / code / 耗时 / requestId。
func AccessLog() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logx.WithContext(ctx).Infow("rpc access",
			logx.Field("method", info.FullMethod),
			logx.Field("code", status.Code(err).String()),
			logx.Field("durationMs", time.Since(start).Milliseconds()),
			logx.Field("requestId", RequestIDFromContext(ctx)),
		)
		return resp, err
	}
}
