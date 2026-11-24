// Package config 定义 agent 的强类型配置。
package config

import "github.com/zeromicro/go-zero/zrpc"

// AgentConfig 是 agent（开源，独立发版）配置。
type AgentConfig struct {
	zrpc.RpcServerConf
	AgentVersion           string     `json:",default=1.0.0"`
	CoreVersion            string     `json:",default=1.0.0"`
	ContractVersion        string     `json:",default=1.0.0"`
	NodeID                 string     `json:",default="`
	NodeType               string     `json:",default=entry,options=[entry|tunnel]"`
	CoreNodeSocket         string     `json:",default=/run/relayx/core-node.sock"`
	CoreNodeTimeoutSeconds int        `json:",default=32"`
	DedupCapacity          int        `json:",default=1024"`
	RequestAuth            AuthConfig `json:",optional"`
	PanelPublicKey         string     `json:",optional"` // base64 Ed25519；为空时仅校验时间戳 / nonce（开发）
	TLS                    TLSConfig  `json:",optional"`

	// 节点注册（design §3.10）：地址为空时跳过注册（开发）。
	PanelRegistrationAddr string `json:",optional"`
	PanelCAFile           string `json:",optional"`
	RegistrationToken     string `json:",optional"`
	CertFile              string `json:",default=/var/lib/relayx/agent/client.crt"`
	KeyFile               string `json:",default=/var/lib/relayx/agent/client.key"`

	// STO-004 Agent 在线自升级。
	SelfUpdate SelfUpdateConfig `json:",optional"`
}

// SelfUpdateConfig 配置 Agent 自升级目录、服务与验签公钥。
type SelfUpdateConfig struct {
	Enabled     bool     `json:",default=true"`
	BaseDir     string   `json:",default=/opt/relayx-agent"`
	CurrentLink string   `json:",default=current"`
	ServiceName string   `json:",default=relayx-agent.service"`
	PublicKeys  []string `json:",optional"`
}

// TLSConfig 配置 mTLS（服务端要求客户端证书，TLS 1.3）。
type TLSConfig struct {
	CertFile string `json:",optional"`
	KeyFile  string `json:",optional"`
	CAFile   string `json:",optional"`
}

// AuthConfig 配置请求签名 / 防重放。
type AuthConfig struct {
	Enabled          bool `json:",default=true"`
	ClockSkewSeconds int  `json:",default=300"`
	NonceTTLSeconds  int  `json:",default=600"`
}
