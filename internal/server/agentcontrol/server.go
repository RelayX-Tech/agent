// Package agentcontrol 组装 agent 的 zrpc 服务端。
package agentcontrol

import (
	"encoding/base64"
	"time"

	"github.com/relayx-tech/agent/internal/config"
	"github.com/relayx-tech/agent/internal/middleware"
	"github.com/relayx-tech/agent/internal/svc"
	agentv1 "github.com/relayx-tech/contracts/gen/go/agent/v1"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Server 是 agent 进程的组装根。
type Server struct {
	cfg     config.AgentConfig
	service *Service
}

// New 构造 agent Server。
func New(cfg config.AgentConfig, s *svc.ServiceContext) *Server {
	return &Server{cfg: cfg, service: NewService(s, s.NodeClient)}
}

// Start 启动 zrpc 服务并阻塞。
func (s *Server) Start() {
	server := zrpc.MustNewServer(s.cfg.RpcServerConf, func(grpcServer *grpc.Server) {
		agentv1.RegisterAgentControlServiceServer(grpcServer, s.service)
	})
	if s.cfg.TLS.CertFile != "" && s.cfg.TLS.KeyFile != "" && s.cfg.TLS.CAFile != "" {
		if tlsCfg, terr := middleware.ServerTLSConfig(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile, s.cfg.TLS.CAFile); terr == nil {
			server.AddOptions(grpc.Creds(credentials.NewTLS(tlsCfg)))
		} else {
			logx.Errorw("agent mTLS 配置失败", logx.Field("error", terr.Error()))
		}
	}

	interceptors := []grpc.UnaryServerInterceptor{
		middleware.Recovery(),
		middleware.RequestID(),
		middleware.AccessLog(),
	}
	if s.cfg.RequestAuth.Enabled {
		var verifier middleware.SignatureVerifier
		if s.cfg.PanelPublicKey != "" {
			if pub, derr := base64.StdEncoding.DecodeString(s.cfg.PanelPublicKey); derr == nil {
				if v, verr := middleware.NewEd25519Verifier(pub); verr == nil {
					verifier = v
				}
			}
		}
		interceptors = append(interceptors, middleware.Auth(middleware.AuthConfig{
			Verifier:  verifier,
			ClockSkew: time.Duration(s.cfg.RequestAuth.ClockSkewSeconds) * time.Second,
			NonceTTL:  time.Duration(s.cfg.RequestAuth.NonceTTLSeconds) * time.Second,
		}))
	}
	server.AddUnaryInterceptors(middleware.Chain(interceptors...))
	defer server.Stop()
	server.Start()
}
