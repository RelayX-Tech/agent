package middleware

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
)

// Ed25519Verifier 按 design §6.3 校验请求签名：
// SHA-256(method \n request_id \n timestamp \n nonce \n sha256(body))。
type Ed25519Verifier struct {
	publicKey ed25519.PublicKey
}

// NewEd25519Verifier 校验公钥长度并构造验签器。
func NewEd25519Verifier(publicKey ed25519.PublicKey) (*Ed25519Verifier, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, errors.New("ed25519 公钥长度非法")
	}
	return &Ed25519Verifier{publicKey: publicKey}, nil
}

// Verify 实现 SignatureVerifier。
func (v *Ed25519Verifier) Verify(_ context.Context, method, requestID string, timestamp time.Time, nonce string, body []byte, signature string) error {
	digest := signatureDigest(method, requestID, timestamp, nonce, body)
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("签名 base64 解码失败: %w", err)
	}
	if !ed25519.Verify(v.publicKey, digest, sig) {
		return errors.New("签名校验失败")
	}
	return nil
}

// Ed25519Sign 生成与 Verify 匹配的签名（Panel 客户端 / 测试使用）。
func Ed25519Sign(privateKey ed25519.PrivateKey, method, requestID string, timestamp time.Time, nonce string, body []byte) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", errors.New("ed25519 私钥长度非法")
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, signatureDigest(method, requestID, timestamp, nonce, body))), nil
}

func signatureDigest(method, requestID string, timestamp time.Time, nonce string, body []byte) []byte {
	bodyHash := sha256.Sum256(body)
	input := strings.Join([]string{
		method,
		requestID,
		timestamp.UTC().Format(time.RFC3339Nano),
		nonce,
		hex.EncodeToString(bodyHash[:]),
	}, "\n")
	digest := sha256.Sum256([]byte(input))
	return digest[:]
}

func marshalBody(req any) []byte {
	msg, ok := req.(proto.Message)
	if !ok {
		return nil
	}
	body, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		return nil
	}
	return body
}
