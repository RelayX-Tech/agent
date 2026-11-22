package register_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/relayx-tech/agent/internal/register"
	agentv1 "github.com/relayx-tech/contracts/gen/go/agent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeClient struct {
	registerCalls int
	renewCalls    int
	notAfter      time.Time
}

func (f *fakeClient) Register(context.Context, *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	f.registerCalls++
	return f.response(), nil
}

func (f *fakeClient) Renew(context.Context, *agentv1.RenewRequest) (*agentv1.RegisterResponse, error) {
	f.renewCalls++
	return f.response(), nil
}

func (f *fakeClient) response() *agentv1.RegisterResponse {
	return &agentv1.RegisterResponse{
		CertChainPem:                issueCert("node-1", f.notAfter),
		NotAfter:                    timestamppb.New(f.notAfter),
		PinnedCertFingerprintSha256: "fp",
	}
}

func issueCert(cn string, notAfter time.Time) []byte {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestBootstrapRegistersWhenNoCert(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "client.crt")
	keyFile := filepath.Join(dir, "client.key")
	fake := &fakeClient{notAfter: time.Now().Add(90 * 24 * time.Hour)}

	res, err := register.Bootstrap(context.Background(), fake, register.Options{
		Token: "token", NodeID: "node-1", NodeType: agentv1.AgentNodeType_AGENT_NODE_TYPE_ENTRY,
		CertFile: certFile, KeyFile: keyFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Registered || fake.registerCalls != 1 {
		t.Fatalf("res = %+v, registerCalls = %d", res, fake.registerCalls)
	}
	info, err := os.Stat(certFile)
	if err != nil {
		t.Fatal(err)
	}
	// Windows 不保留 POSIX 权限位；仅在非 Windows 断言 0600。
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("证书权限 = %v", info.Mode().Perm())
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatal("私钥未写入")
	}
}

func TestBootstrapNoopWhenValid(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "client.crt")
	keyFile := filepath.Join(dir, "client.key")
	_ = os.WriteFile(certFile, issueCert("node-1", time.Now().Add(90*24*time.Hour)), 0o600)
	_ = os.WriteFile(keyFile, []byte("key"), 0o600)

	fake := &fakeClient{notAfter: time.Now().Add(90 * 24 * time.Hour)}
	res, err := register.Bootstrap(context.Background(), fake, register.Options{NodeID: "node-1", CertFile: certFile, KeyFile: keyFile})
	if err != nil {
		t.Fatal(err)
	}
	if res.Registered || res.Renewed || fake.registerCalls != 0 || fake.renewCalls != 0 {
		t.Fatalf("res = %+v", res)
	}
}

func TestBootstrapRenewsWhenNearExpiry(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "client.crt")
	keyFile := filepath.Join(dir, "client.key")
	_ = os.WriteFile(certFile, issueCert("node-1", time.Now().Add(3*24*time.Hour)), 0o600)
	_ = os.WriteFile(keyFile, []byte("key"), 0o600)

	fake := &fakeClient{notAfter: time.Now().Add(90 * 24 * time.Hour)}
	res, err := register.Bootstrap(context.Background(), fake, register.Options{
		NodeID: "node-1", CertFile: certFile, KeyFile: keyFile, RenewBefore: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Renewed || fake.renewCalls != 1 {
		t.Fatalf("res = %+v, renewCalls = %d", res, fake.renewCalls)
	}
}
