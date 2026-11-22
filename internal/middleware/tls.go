package middleware

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// ServerTLSConfig 构造 mTLS 服务端配置：要求并校验客户端证书，TLS 1.3。
func ServerTLSConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("加载服务端证书: %w", err)
	}
	pool, err := loadCertPool(caFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// ClientTLSConfig 构造 mTLS 客户端配置：加载客户端证书并校验服务端 CA，TLS 1.3。
func ClientTLSConfig(certFile, keyFile, caFile, serverName string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("加载客户端证书: %w", err)
	}
	pool, err := loadCertPool(caFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

func loadCertPool(caFile string) (*x509.CertPool, error) {
	data, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("读取 CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("CA PEM 解析失败: %s", caFile)
	}
	return pool, nil
}
