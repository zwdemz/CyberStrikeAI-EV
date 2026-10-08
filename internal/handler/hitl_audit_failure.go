package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/openai"
	"cyberstrike-ai/internal/typesafe"
)

// auditFailure preserves the deny decision while recording a distinct operational
// failure marker in the existing persisted comment and progress-event fields.
func auditFailure(category string, status int) hitlDecision {
	detail := category
	if status != 0 {
		detail = fmt.Sprintf("HTTP %d; %s", status, category)
	}
	return hitlDecision{Decision: "reject", Comment: "[audit_error] 审查服务异常，工具未执行（非模型安全裁决）: " + detail}
}

// auditCallFailure exports only allowlisted diagnostics. Gateway bodies may
// contain arbitrary request data, so raw messages and credentials never enter
// the audit log or browser, even when the upstream echoes them in an error.
func auditCallFailure(err error) hitlDecision {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		category := "upstream_request_failed"
		if apiErr.StatusCode == 400 && strings.Contains(strings.ToLower(apiErr.Body), "temperature") {
			category = "temperature_not_supported; configure hitl.audit_model.temperature"
		}
		return auditFailure(category, apiErr.StatusCode)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return auditFailure("timeout", 0)
	}
	if errors.Is(err, context.Canceled) {
		return auditFailure("cancelled", 0)
	}
	return auditFailure("connection_or_protocol_error", 0)
}

func typeSafeAuditFailure(category string, status int) hitlDecision {
	failure := auditFailure(category, status)
	failure.Comment = strings.Replace(failure.Comment, "[audit_error]", "[audit_error] TypeSafe", 1)
	return failure
}

func typeSafeAuditCallFailure(err error) hitlDecision {
	var apiErr *typesafe.APIError
	if errors.As(err, &apiErr) {
		return typeSafeAuditFailure("upstream_request_failed", apiErr.StatusCode)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return typeSafeAuditFailure("timeout", 0)
	}
	if errors.Is(err, context.Canceled) {
		return typeSafeAuditFailure("cancelled", 0)
	}
	return typeSafeAuditFailure("connection_or_protocol_error", 0)
}
