// Package svc 是 agent 的 ServiceContext 组合根。
package svc

import (
	"time"

	"github.com/relayx-tech/agent/internal/config"
	"github.com/relayx-tech/agent/internal/nodeclient"
)

// ServiceContext 持有请求间共享的只读依赖。
type ServiceContext struct {
	AgentVersion    string
	CoreVersion     string
	ContractVersion string
	XrayVersion     string
	NodeID          string
	NodeType        string
	StartedAt       time.Time
	NodeClient      nodeclient.RuntimeClient
}

// New 构造 ServiceContext。
func New(cfg config.AgentConfig, client nodeclient.RuntimeClient) *ServiceContext {
	return &ServiceContext{
		AgentVersion:    cfg.AgentVersion,
		CoreVersion:     cfg.CoreVersion,
		ContractVersion: cfg.ContractVersion,
		XrayVersion:     "26.11.9",
		NodeID:          cfg.NodeID,
		NodeType:        cfg.NodeType,
		StartedAt:       time.Now(),
		NodeClient:      client,
	}
}
