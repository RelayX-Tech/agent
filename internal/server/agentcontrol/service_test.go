package agentcontrol_test

import (
	"context"
	"testing"

	"github.com/relayx-tech/agent/internal/config"
	"github.com/relayx-tech/agent/internal/errs"
	"github.com/relayx-tech/agent/internal/server/agentcontrol"
	"github.com/relayx-tech/agent/internal/svc"
	agentv1 "github.com/relayx-tech/contracts/gen/go/agent/v1"
	corev1 "github.com/relayx-tech/contracts/gen/go/core/v1"
)

type fakeClient struct {
	applyErr  error
	healthErr error
}

func (f *fakeClient) Apply(_ context.Context, req *corev1.ApplyRequest) (*corev1.ApplyResponse, error) {
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	return &corev1.ApplyResponse{Accepted: true, State: "APPLIED", Version: req.GetVersion(), AppliedHash: req.GetConfigHash()}, nil
}

func (f *fakeClient) Rollback(_ context.Context, req *corev1.RollbackRequest) (*corev1.RollbackResponse, error) {
	return &corev1.RollbackResponse{Accepted: true, State: "APPLIED", Version: req.GetNewVersion(), AppliedHash: req.GetConfigHash()}, nil
}

func (f *fakeClient) KernelStatus(context.Context, *corev1.KernelStatusRequest) (*corev1.KernelStatusResponse, error) {
	return &corev1.KernelStatusResponse{}, nil
}

func (f *fakeClient) Perception(context.Context, *corev1.PerceptionRequest) (*corev1.PerceptionResponse, error) {
	return &corev1.PerceptionResponse{}, nil
}

func (f *fakeClient) Version(context.Context, *corev1.VersionInfoRequest) (*corev1.VersionInfoResponse, error) {
	return &corev1.VersionInfoResponse{Version: &corev1.BuildVersion{CoreVersion: "1.0.0", XrayVersion: "26.11.9"}}, nil
}

func (f *fakeClient) Health(context.Context, *corev1.HealthCheckRequest) (*corev1.HealthCheckResponse, error) {
	if f.healthErr != nil {
		return nil, f.healthErr
	}
	return &corev1.HealthCheckResponse{Status: "SERVING"}, nil
}

func (f *fakeClient) Close() error { return nil }

func newService(f *fakeClient) *agentcontrol.Service {
	svcCtx := svc.New(config.AgentConfig{AgentVersion: "1.0.0", ContractVersion: "1.0.0", NodeID: "node-1"}, f)
	return agentcontrol.NewService(svcCtx, f)
}

func applyReq(opID, hash string) *agentv1.AgentApplyRequest {
	return &agentv1.AgentApplyRequest{
		Meta:        &agentv1.AgentRequestMeta{OperationId: opID},
		NodeId:      "node-1",
		Version:     1,
		ConfigHash:  hash,
		ConfigJson:  "{}",
		XrayVersion: "26.11.9",
	}
}

func TestHelloCompatible(t *testing.T) {
	resp, err := newService(&fakeClient{}).Hello(context.Background(), &agentv1.HelloRequest{ContractVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.GetCompatible() || resp.GetNodeId() != "node-1" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestHelloIncompatible(t *testing.T) {
	_, err := newService(&fakeClient{}).Hello(context.Background(), &agentv1.HelloRequest{ContractVersion: "2.0.0"})
	if errs.CodeOf(err) != errs.CodeVersionIncompatible {
		t.Fatalf("code = %q, 期望 VERSION_INCOMPATIBLE", errs.CodeOf(err))
	}
}

func TestApplyForwardAndDedup(t *testing.T) {
	s := newService(&fakeClient{})
	resp, err := s.Apply(context.Background(), applyReq("op-1", "h1"))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.GetAccepted() || resp.GetState() != "APPLIED" {
		t.Fatalf("resp = %+v", resp)
	}
	dup, err := s.Apply(context.Background(), applyReq("op-1", "h1"))
	if err != nil {
		t.Fatal(err)
	}
	if !dup.GetDeduplicated() {
		t.Fatal("重复 operation 应去重")
	}
}

func TestApplyOperationConflict(t *testing.T) {
	s := newService(&fakeClient{})
	if _, err := s.Apply(context.Background(), applyReq("op-1", "h1")); err != nil {
		t.Fatal(err)
	}
	_, err := s.Apply(context.Background(), applyReq("op-1", "h2"))
	if errs.CodeOf(err) != errs.CodeOperationConflict {
		t.Fatalf("code = %q, 期望 OPERATION_CONFLICT", errs.CodeOf(err))
	}
}

func TestRollback(t *testing.T) {
	resp, err := newService(&fakeClient{}).Rollback(context.Background(), &agentv1.AgentRollbackRequest{
		Meta:          &agentv1.AgentRequestMeta{OperationId: "op-rb"},
		NodeId:        "node-1",
		TargetVersion: 1,
		NewVersion:    2,
		ConfigHash:    "h",
		ConfigJson:    "{}",
		Reason:        "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetState() != "APPLIED" || resp.GetVersion() != 2 {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestHealth(t *testing.T) {
	resp, err := newService(&fakeClient{}).Health(context.Background(), &agentv1.AgentHealthRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != "SERVING" || !resp.GetCoreNodeReachable() {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestVersion(t *testing.T) {
	resp, err := newService(&fakeClient{}).Version(context.Background(), &agentv1.VersionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetVersion().GetAgentVersion() != "1.0.0" || resp.GetVersion().GetCoreVersion() != "1.0.0" {
		t.Fatalf("resp = %+v", resp.GetVersion())
	}
}
