package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"log"
	"os"
	"time"

	"github.com/relayx-tech/agent/internal/config"
	"github.com/relayx-tech/agent/internal/nodeclient"
	"github.com/relayx-tech/agent/internal/register"
	"github.com/relayx-tech/agent/internal/server/agentcontrol"
	"github.com/relayx-tech/agent/internal/svc"
	agentv1 "github.com/relayx-tech/contracts/gen/go/agent/v1"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	configFile = flag.String("f", "etc/agent.yaml", "the config file")
	version    = "dev"
)

func main() {
	flag.Parse()

	var c config.AgentConfig
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)

	client, err := nodeclient.DialWithTimeout(c.CoreNodeSocket, time.Duration(c.CoreNodeTimeoutSeconds)*time.Second)
	if err != nil {
		log.Fatalf("连接本机 core-node 失败: %v", err)
	}
	defer client.Close()

	if c.PanelRegistrationAddr != "" {
		if err := bootstrapRegistration(c); err != nil {
			log.Fatalf("节点注册失败: %v", err)
		}
	}

	serviceContext := svc.New(c, client)
	agentcontrol.New(c, serviceContext).Start()
}

// bootstrapRegistration 在启动前完成节点注册 / 续期（design §3.10）。
func bootstrapRegistration(c config.AgentConfig) error {
	var opts []grpc.DialOption
	if c.PanelCAFile != "" {
		caPEM, err := os.ReadFile(c.PanelCAFile)
		if err != nil {
			return err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return errors.New("Panel CA PEM 解析失败")
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS13,
		})))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(c.PanelRegistrationAddr, opts...)
	if err != nil {
		return err
	}
	defer conn.Close()

	res, err := register.Bootstrap(context.Background(), registrationClient{agentv1.NewRegistrationServiceClient(conn)}, register.Options{
		Token:    c.RegistrationToken,
		NodeID:   c.NodeID,
		NodeType: nodeType(c.NodeType),
		CertFile: c.CertFile,
		KeyFile:  c.KeyFile,
	})
	if err != nil {
		return err
	}
	if res.Registered || res.Renewed {
		logx.Infow("node registration",
			logx.Field("registered", res.Registered),
			logx.Field("renewed", res.Renewed),
			logx.Field("notAfter", res.NotAfter),
		)
	}
	return nil
}

// registrationClient 适配生成客户端（去掉 variadic CallOption）。
type registrationClient struct {
	c agentv1.RegistrationServiceClient
}

func (r registrationClient) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	return r.c.Register(ctx, req)
}

func (r registrationClient) Renew(ctx context.Context, req *agentv1.RenewRequest) (*agentv1.RegisterResponse, error) {
	return r.c.Renew(ctx, req)
}

func nodeType(s string) agentv1.AgentNodeType {
	if s == "tunnel" {
		return agentv1.AgentNodeType_AGENT_NODE_TYPE_TUNNEL
	}
	return agentv1.AgentNodeType_AGENT_NODE_TYPE_ENTRY
}
