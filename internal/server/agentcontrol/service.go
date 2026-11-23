// Package agentcontrol 实现 AgentControlService（Panel ↔ Agent）9 个方法。
// Apply / Rollback 经 operation_id 去重后转发本机 core-node；探测 / 状态 / 网卡在 Agent 本地执行。
package agentcontrol

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/relayx-tech/agent/internal/errs"
	"github.com/relayx-tech/agent/internal/nodeclient"
	"github.com/relayx-tech/agent/internal/probe"
	"github.com/relayx-tech/agent/internal/status"
	"github.com/relayx-tech/agent/internal/svc"
	agentv1 "github.com/relayx-tech/contracts/gen/go/agent/v1"
	corev1 "github.com/relayx-tech/contracts/gen/go/core/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service 实现 AgentControlService。
type Service struct {
	agentv1.UnimplementedAgentControlServiceServer
	svc   *svc.ServiceContext
	node  nodeclient.RuntimeClient
	dedup *operationDedup
	now   func() time.Time
}

// NewService 构造 AgentControlService；去重缓存默认 1024 条。
func NewService(s *svc.ServiceContext, node nodeclient.RuntimeClient) *Service {
	return &Service{svc: s, node: node, dedup: newOperationDedup(1024), now: time.Now}
}

// Hello 做合同 / 版本握手；不兼容返回 VERSION_INCOMPATIBLE。
func (s *Service) Hello(_ context.Context, req *agentv1.HelloRequest) (*agentv1.HelloResponse, error) {
	if !contractCompatible(req.GetContractVersion()) {
		return nil, errs.New(errs.CodeVersionIncompatible, "contract_version", "合同版本不兼容，请升级", false)
	}
	return &agentv1.HelloResponse{
		Compatible:   true,
		Version:      s.buildVersion(),
		NodeId:       s.svc.NodeID,
		Capabilities: []string{"apply", "rollback", "perception", "probe"},
	}, nil
}

// Apply 去重后转发 core-node；同 operation_id 同 payload 返回首次结果。
func (s *Service) Apply(ctx context.Context, req *agentv1.AgentApplyRequest) (*agentv1.AgentApplyResponse, error) {
	opID := req.GetMeta().GetOperationId()
	if opID != "" {
		if prev, ok := s.dedup.get(opID); ok {
			if prev.hash == req.GetConfigHash() {
				return &agentv1.AgentApplyResponse{
					Accepted: true, State: prev.state, Version: prev.version,
					AppliedHash: prev.hash, Deduplicated: true,
				}, nil
			}
			return nil, errs.New(errs.CodeOperationConflict, "meta.operation_id", "operation_id 复用了不同内容", false)
		}
	}

	resp, err := s.node.Apply(ctx, &corev1.ApplyRequest{
		Meta:          toCoreMeta(req.GetMeta()),
		NodeId:        req.GetNodeId(),
		Version:       req.GetVersion(),
		ConfigHash:    req.GetConfigHash(),
		ConfigJson:    req.GetConfigJson(),
		Materials:     toCoreMaterials(req.GetMaterials()),
		SchemaVersion: req.GetSchemaVersion(),
		XrayVersion:   req.GetXrayVersion(),
	})
	if err != nil {
		return nil, err
	}
	if opID != "" {
		s.dedup.put(opID, opEntry{hash: req.GetConfigHash(), state: resp.GetState(), version: resp.GetVersion()})
	}
	return &agentv1.AgentApplyResponse{
		Accepted: resp.GetAccepted(), State: resp.GetState(), Version: resp.GetVersion(),
		AppliedHash: resp.GetAppliedHash(), Deduplicated: resp.GetDeduplicated(),
	}, nil
}

// Rollback 去重后转发 core-node。
func (s *Service) Rollback(ctx context.Context, req *agentv1.AgentRollbackRequest) (*agentv1.AgentRollbackResponse, error) {
	opID := req.GetMeta().GetOperationId()
	if opID != "" {
		if prev, ok := s.dedup.get(opID); ok {
			if prev.hash == req.GetConfigHash() {
				return &agentv1.AgentRollbackResponse{
					Accepted: true, State: prev.state, Version: prev.version,
					AppliedHash: prev.hash, Deduplicated: true,
				}, nil
			}
			return nil, errs.New(errs.CodeOperationConflict, "meta.operation_id", "operation_id 复用了不同内容", false)
		}
	}

	resp, err := s.node.Rollback(ctx, &corev1.RollbackRequest{
		Meta:          toCoreMeta(req.GetMeta()),
		NodeId:        req.GetNodeId(),
		TargetVersion: req.GetTargetVersion(),
		NewVersion:    req.GetNewVersion(),
		ConfigHash:    req.GetConfigHash(),
		ConfigJson:    req.GetConfigJson(),
		Materials:     toCoreMaterials(req.GetMaterials()),
		Reason:        req.GetReason(),
	})
	if err != nil {
		return nil, err
	}
	if opID != "" {
		s.dedup.put(opID, opEntry{hash: req.GetConfigHash(), state: resp.GetState(), version: resp.GetVersion()})
	}
	return &agentv1.AgentRollbackResponse{
		Accepted: resp.GetAccepted(), State: resp.GetState(), Version: resp.GetVersion(),
		AppliedHash: resp.GetAppliedHash(), Deduplicated: resp.GetDeduplicated(),
	}, nil
}

// Version 返回 Agent / core / Xray 版本。
func (s *Service) Version(ctx context.Context, _ *agentv1.VersionRequest) (*agentv1.VersionResponse, error) {
	bv := s.buildVersion()
	if resp, err := s.node.Version(ctx, &corev1.VersionInfoRequest{}); err == nil && resp.GetVersion() != nil {
		bv.CoreVersion = resp.GetVersion().GetCoreVersion()
		bv.SchemaVersion = resp.GetVersion().GetSchemaVersion()
		bv.XrayVersion = resp.GetVersion().GetXrayVersion()
	}
	return &agentv1.VersionResponse{Version: bv}, nil
}

// Health 返回 SERVING 与 core-node 可达性。
func (s *Service) Health(ctx context.Context, _ *agentv1.AgentHealthRequest) (*agentv1.AgentHealthResponse, error) {
	resp := &agentv1.AgentHealthResponse{Status: "SERVING", CheckedAt: timestamppb.Now()}
	if _, err := s.node.Health(ctx, &corev1.HealthCheckRequest{}); err == nil {
		resp.CoreNodeReachable = true
	}
	return resp, nil
}

// Status 合并 Agent 资源与 core-node 内核状态。
func (s *Service) Status(ctx context.Context, _ *agentv1.StatusRequest) (*agentv1.StatusResponse, error) {
	resp := &agentv1.StatusResponse{
		NodeId:       s.svc.NodeID,
		NodeType:     s.svc.NodeType,
		AgentVersion: s.svc.AgentVersion,
		Core:         s.buildVersion(),
		Resources:    status.Resources(),
		LastSeen:     timestamppb.Now(),
	}
	if ks, err := s.node.KernelStatus(ctx, &corev1.KernelStatusRequest{NodeId: s.svc.NodeID}); err == nil {
		resp.Kernel = toAgentKernelState(ks.GetKernel())
		resp.LastApplyState = ks.GetKernel().GetState()
	}
	return resp, nil
}

// Perception 转发 core-node 感知并按 Agent 合同映射。
func (s *Service) Perception(ctx context.Context, req *agentv1.AgentPerceptionRequest) (*agentv1.AgentPerceptionResponse, error) {
	coreResp, err := s.node.Perception(ctx, &corev1.PerceptionRequest{
		NodeId:             s.svc.NodeID,
		PortGroupIds:       req.GetPortGroupIds(),
		IncludeTraffic:     req.GetIncludeTraffic(),
		IncludeConnections: req.GetIncludeConnections(),
	})
	if err != nil {
		return nil, err
	}
	resp := &agentv1.AgentPerceptionResponse{}
	for _, in := range coreResp.GetInbounds() {
		resp.Inbounds = append(resp.Inbounds, &agentv1.AgentInboundPerception{
			Tag: in.GetTag(), UplinkBytes: in.GetUplinkBytes(), DownlinkBytes: in.GetDownlinkBytes(), ActiveConnections: in.GetActiveConnections(),
		})
	}
	for _, out := range coreResp.GetOutbounds() {
		resp.Outbounds = append(resp.Outbounds, &agentv1.AgentOutboundPerception{
			Tag: out.GetTag(), UplinkBytes: out.GetUplinkBytes(), DownlinkBytes: out.GetDownlinkBytes(), ActiveConnections: out.GetActiveConnections(),
		})
	}
	if t := coreResp.GetTraffic(); t != nil {
		resp.Traffic = &agentv1.AgentTraffic{UplinkBytes: t.GetUplinkBytes(), DownlinkBytes: t.GetDownlinkBytes(), UplinkRate: t.GetUplinkRate(), DownlinkRate: t.GetDownlinkRate()}
	}
	if c := coreResp.GetConnections(); c != nil {
		resp.Connections = &agentv1.AgentConnections{Total: c.GetTotal(), Tcp: c.GetTcp(), Udp: c.GetUdp()}
	}
	if r := coreResp.GetResources(); r != nil {
		resp.Resources = &agentv1.AgentResources{CpuPercent: r.GetCpuPercent(), MemoryBytes: r.GetMemoryBytes(), ThreadCount: r.GetThreadCount()}
	}
	for _, e := range coreResp.GetEvents() {
		resp.Events = append(resp.Events, &agentv1.AgentEvent{Type: e.GetType(), At: e.GetAt(), Message: e.GetMessage()})
	}
	return resp, nil
}

// Interfaces 返回节点网卡清单。
func (s *Service) Interfaces(_ context.Context, _ *agentv1.InterfacesRequest) (*agentv1.InterfacesResponse, error) {
	return &agentv1.InterfacesResponse{Interfaces: status.Interfaces()}, nil
}

// Probe 在 Agent 本地执行 TCP / HTTP 探测（core 不执行通用探测）。
func (s *Service) Probe(ctx context.Context, req *agentv1.ProbeRequest) (*agentv1.ProbeResponse, error) {
	timeout := time.Duration(req.GetTimeoutMs()) * time.Millisecond
	switch req.GetType() {
	case agentv1.ProbeType_PROBE_TYPE_TCP:
		ok, latency, msg := probe.TCP(ctx, req.GetTarget(), timeout)
		return &agentv1.ProbeResponse{Success: ok, LatencyMs: latency, Error: msg}, nil
	case agentv1.ProbeType_PROBE_TYPE_HTTP:
		ok, latency, code, msg := probe.HTTP(ctx, req.GetTarget(), timeout)
		return &agentv1.ProbeResponse{Success: ok, LatencyMs: latency, StatusCode: code, Error: msg}, nil
	default:
		return nil, errs.New(errs.CodeValidationMissing, "type", "探测类型非法", false)
	}
}

func toAgentKernelState(ks *corev1.KernelState) *agentv1.AgentKernelState {
	if ks == nil {
		return nil
	}
	return &agentv1.AgentKernelState{
		State:       ks.GetState(),
		Pid:         ks.GetPid(),
		StartedAt:   ks.GetStartedAt(),
		Restarts:    ks.GetRestarts(),
		XrayVersion: ks.GetXrayVersion(),
		ConfigHash:  ks.GetConfigHash(),
		LastError:   ks.GetLastError(),
	}
}

func (s *Service) buildVersion() *agentv1.AgentBuildVersion {
	return &agentv1.AgentBuildVersion{
		CoreVersion:     s.svc.CoreVersion,
		SchemaVersion:   "1.0.0",
		ContractVersion: s.svc.ContractVersion,
		AgentVersion:    s.svc.AgentVersion,
		XrayVersion:     s.svc.XrayVersion,
	}
}

func contractCompatible(v string) bool {
	if v == "" {
		return true
	}
	parts := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 2)
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	return major == 1
}

func toCoreMeta(m *agentv1.AgentRequestMeta) *corev1.RequestMeta {
	if m == nil {
		return &corev1.RequestMeta{}
	}
	return &corev1.RequestMeta{
		RequestId:       m.GetRequestId(),
		OperationId:     m.GetOperationId(),
		ContractVersion: m.GetContractVersion(),
		Timestamp:       m.GetTimestamp(),
		Nonce:           m.GetNonce(),
		Actor:           m.GetActor(),
	}
}

func toCoreMaterials(in []*agentv1.AgentMaterial) []*corev1.Material {
	out := make([]*corev1.Material, 0, len(in))
	for _, m := range in {
		out = append(out, &corev1.Material{
			MaterialId:        m.GetMaterialId(),
			Kind:              corev1.MaterialKind(m.GetKind()),
			Content:           m.GetContent(),
			FingerprintSha256: m.GetFingerprintSha256(),
		})
	}
	return out
}

type opEntry struct {
	hash    string
	state   string
	version uint64
}

// operationDedup 是容量受限的 operation_id 去重缓存（FIFO 淘汰）。
type operationDedup struct {
	mu    sync.Mutex
	cap   int
	m     map[string]opEntry
	order []string
}

func newOperationDedup(capacity int) *operationDedup {
	if capacity <= 0 {
		capacity = 1024
	}
	return &operationDedup{cap: capacity, m: make(map[string]opEntry)}
}

func (d *operationDedup) get(id string) (opEntry, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.m[id]
	return e, ok
}

func (d *operationDedup) put(id string, e opEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.m[id]; !exists {
		d.order = append(d.order, id)
		if len(d.order) > d.cap {
			oldest := d.order[0]
			d.order = d.order[1:]
			delete(d.m, oldest)
		}
	}
	d.m[id] = e
}
