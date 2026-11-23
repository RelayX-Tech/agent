// Package errs 把 Agent 稳定错误码映射为 gRPC status + AgentErrorDetail。
package errs

import (
	agentv1 "github.com/relayx-tech/contracts/gen/go/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// 稳定错误码（与 contracts/errors/v1/error-codes.yaml 对齐）。
const (
	CodeOK                  = "OK"
	CodeVersionIncompatible = "VERSION_INCOMPATIBLE"
	CodeOperationConflict   = "OPERATION_CONFLICT"
	CodeValidationMissing   = "VALIDATION_MISSING_FIELD"
	CodeProbeFailed         = "PROBE_FAILED"
	CodeProbeTimeout        = "PROBE_TIMEOUT"
	CodeInternal            = "INTERNAL"
)

var codeToGRPC = map[string]codes.Code{
	CodeOK:                  codes.OK,
	CodeVersionIncompatible: codes.FailedPrecondition,
	CodeOperationConflict:   codes.AlreadyExists,
	CodeValidationMissing:   codes.InvalidArgument,
	CodeProbeFailed:         codes.Unavailable,
	CodeProbeTimeout:        codes.DeadlineExceeded,
	CodeInternal:            codes.Internal,
}

// GRPCCode 返回错误码对应 status code；未登记兜底 INTERNAL。
func GRPCCode(code string) codes.Code {
	if c, ok := codeToGRPC[code]; ok {
		return c
	}
	return codes.Internal
}

// New 构造携带 AgentErrorDetail 的 gRPC 错误。
func New(code, field, message string, retryable bool) error {
	st := status.New(GRPCCode(code), message)
	withDetails, err := st.WithDetails(&agentv1.AgentErrorDetail{
		Code: code, Field: field, Message: message, Retryable: retryable,
	})
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}

// CodeOf 返回错误携带的稳定错误码；缺失兜底 INTERNAL。
func CodeOf(err error) string {
	if err == nil {
		return CodeInternal
	}
	st, ok := status.FromError(err)
	if !ok {
		return CodeInternal
	}
	for _, d := range st.Details() {
		if detail, ok := d.(*agentv1.AgentErrorDetail); ok && detail.GetCode() != "" {
			return detail.GetCode()
		}
	}
	return CodeInternal
}
