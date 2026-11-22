// Package register 实现 Agent 注册引导：CSR 生成、Token 注册、证书落盘、到期续期（design §3.10）。
package register

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	agentv1 "github.com/relayx-tech/contracts/gen/go/agent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Client 是注册合同的最小客户端能力（RegistrationServiceClient 满足）。
type Client interface {
	Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error)
	Renew(ctx context.Context, req *agentv1.RenewRequest) (*agentv1.RegisterResponse, error)
}

// Options 配置注册引导。
type Options struct {
	Token       string
	NodeID      string
	NodeType    agentv1.AgentNodeType
	CertFile    string
	KeyFile     string
	RenewBefore time.Duration
	Now         func() time.Time
}

// Result 是引导结果。
type Result struct {
	Registered  bool
	Renewed     bool
	Fingerprint string
	NotAfter    time.Time
}

// Bootstrap 决定是否需要注册 / 续期：
// 无证书 → Register；证书临近到期（now+RenewBefore ≥ NotAfter）→ Renew；否则不动。
func Bootstrap(ctx context.Context, client Client, opts Options) (Result, error) {
	if opts.RenewBefore <= 0 {
		opts.RenewBefore = 7 * 24 * time.Hour
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	if cert, ok := loadCert(opts.CertFile); ok {
		if opts.Now().Add(opts.RenewBefore).Before(cert.NotAfter) {
			return Result{NotAfter: cert.NotAfter}, nil
		}
		return renew(ctx, client, opts)
	}
	return registerNew(ctx, client, opts)
}

func registerNew(ctx context.Context, client Client, opts Options) (Result, error) {
	csrPEM, keyPEM, err := generateKeyAndCSR(opts.NodeID)
	if err != nil {
		return Result{}, err
	}
	resp, err := client.Register(ctx, &agentv1.RegisterRequest{
		Meta:     newMeta(),
		Token:    opts.Token,
		NodeId:   opts.NodeID,
		NodeType: opts.NodeType,
		CsrPem:   csrPEM,
	})
	if err != nil {
		return Result{}, err
	}
	fp, err := persist(opts.CertFile, opts.KeyFile, resp, keyPEM)
	if err != nil {
		return Result{}, err
	}
	return Result{Registered: true, Fingerprint: fp, NotAfter: resp.GetNotAfter().AsTime()}, nil
}

func renew(ctx context.Context, client Client, opts Options) (Result, error) {
	csrPEM, keyPEM, err := generateKeyAndCSR(opts.NodeID)
	if err != nil {
		return Result{}, err
	}
	resp, err := client.Renew(ctx, &agentv1.RenewRequest{Meta: newMeta(), CsrPem: csrPEM})
	if err != nil {
		return Result{}, err
	}
	fp, err := persist(opts.CertFile, opts.KeyFile, resp, keyPEM)
	if err != nil {
		return Result{}, err
	}
	return Result{Renewed: true, Fingerprint: fp, NotAfter: resp.GetNotAfter().AsTime()}, nil
}

func generateKeyAndCSR(nodeID string) (csrPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkixName(nodeID)}, key)
	if err != nil {
		return nil, nil, err
	}
	csrPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return csrPEM, keyPEM, nil
}

func persist(certFile, keyFile string, resp *agentv1.RegisterResponse, keyPEM []byte) (string, error) {
	if len(resp.GetCertChainPem()) == 0 {
		return "", fmt.Errorf("注册响应缺少证书链")
	}
	if err := os.WriteFile(certFile, resp.GetCertChainPem(), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return "", err
	}
	if fp := resp.GetPinnedCertFingerprintSha256(); fp != "" {
		return fp, nil
	}
	// 兜底：从链首证书计算指纹
	block, _ := pem.Decode(resp.GetCertChainPem())
	if block == nil {
		return "", nil
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

func loadCert(certFile string) (*x509.Certificate, bool) {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return nil, false
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, false
	}
	return cert, true
}

func newMeta() *agentv1.AgentRequestMeta {
	return &agentv1.AgentRequestMeta{RequestId: randomID(), Timestamp: timestamppb.Now()}
}

func pkixName(nodeID string) pkix.Name { return pkix.Name{CommonName: nodeID} }

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "agent"
	}
	return hex.EncodeToString(b)
}
