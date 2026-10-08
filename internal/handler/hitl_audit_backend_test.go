package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/typesafe"
	"go.uber.org/zap"
)

func TestHitlAuditEngineInfoTypeSafe(t *testing.T) {
	h := &AgentHandler{config: &config.Config{
		OpenAI: config.OpenAIConfig{Model: "gpt-4o"},
		Hitl:   config.HitlConfig{AuditBackend: "typesafe"},
	}}
	backend, model := h.hitlAuditEngineInfo()
	if backend != config.HitlAuditBackendTypeSafe {
		t.Fatalf("backend=%q", backend)
	}
	if model != config.TypeSafeDefaultModel {
		t.Fatalf("model=%q, want %s", model, config.TypeSafeDefaultModel)
	}
}

func TestTypeSafeFailureIsDistinctFromPolicyRejection(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		auditError bool
	}{
		{name: "provider failure", status: 503, body: `{"error":"private-token"}`, auditError: true},
		{name: "incomplete answer", status: 200, body: `{}`, auditError: true},
		{name: "actual policy rejection", status: 200, body: `{"answers":{"destroys_availability":{"noul":0.9},"irreversible_data_loss":{"noul":0.1},"credential_or_acl_change":{"noul":0.1},"config_tamper":{"noul":0.1},"authorized_pentest_payload":{"noul":0.1},"decision":{"choice":"reject","confidence":0.95}}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer gateway.Close()
			t.Setenv(typesafe.AllowedBaseURLsEnv, gateway.URL)
			h := &AgentHandler{logger: zap.NewNop(), config: &config.Config{Hitl: config.HitlConfig{
				AuditBackend: "typesafe", AuditModel: config.OpenAIConfig{BaseURL: gateway.URL, APIKey: "test-key"},
			}}}
			decision := h.auditAgentReviewTypeSafe(context.Background(), "approval", "query_assets", nil)
			if decision.Decision != "reject" || strings.HasPrefix(decision.Comment, "[audit_error]") != test.auditError {
				t.Fatalf("wrong failure classification: %+v", decision)
			}
			if strings.Contains(decision.Comment, "private-token") {
				t.Fatal("provider response leaked into audit comment")
			}
		})
	}
}

func TestHitlAuditEngineInfoOpenAIInheritsMainModel(t *testing.T) {
	h := &AgentHandler{config: &config.Config{
		OpenAI: config.OpenAIConfig{Model: "gpt-4o-mini"},
		Hitl:   config.HitlConfig{AuditBackend: "openai"},
	}}
	backend, model := h.hitlAuditEngineInfo()
	if backend != config.HitlAuditBackendOpenAI {
		t.Fatalf("backend=%q", backend)
	}
	if model != "gpt-4o-mini" {
		t.Fatalf("model=%q", model)
	}
}

func TestHitlAuditBackendFromRecordPrefersPayload(t *testing.T) {
	backend, model := hitlAuditBackendFromRecord("audit_agent", "audit agent: 实际操作：探测", `{
		"hitlApproval": {"auditBackend": "typesafe", "auditModel": "jev-latest"}
	}`)
	if backend != config.HitlAuditBackendTypeSafe || model != "jev-latest" {
		t.Fatalf("backend=%q model=%q", backend, model)
	}
}

func TestHitlAuditBackendFromRecordInfersJevComment(t *testing.T) {
	backend, _ := hitlAuditBackendFromRecord("audit_agent",
		"audit agent: 未命中破坏性规则，默认放行；最高破坏分=破坏业务可用性 0.12；choice=approve(0.90)",
		`{}`)
	if backend != config.HitlAuditBackendTypeSafe {
		t.Fatalf("backend=%q", backend)
	}
}

func TestHitlAuditBackendFromRecordInfersOpenAIComment(t *testing.T) {
	backend, _ := hitlAuditBackendFromRecord("audit_agent",
		"audit agent: 实际操作：读取 /etc/passwd；命中规则：A3",
		`{}`)
	if backend != config.HitlAuditBackendOpenAI {
		t.Fatalf("backend=%q", backend)
	}
}

func TestHitlAuditBackendFromRecordIgnoresHuman(t *testing.T) {
	backend, model := hitlAuditBackendFromRecord("human", "人工通过", `{"hitlApproval":{"auditBackend":"typesafe"}}`)
	if backend != "" || model != "" {
		t.Fatalf("backend=%q model=%q", backend, model)
	}
}
