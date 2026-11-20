package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/openai"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestAuditFailureClassification(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, context.Canceled, errors.New("private-token"), &openai.APIError{StatusCode: 402, Body: "private-token"}} {
		decision := auditCallFailure(err)
		if decision.Decision != "reject" || !strings.HasPrefix(decision.Comment, "[audit_error]") || strings.Contains(decision.Comment, "private-token") {
			t.Fatalf("unsafe failure: %+v", decision)
		}
	}
	decision, err := parseAuditAgentLLMContent(`{"decision":"reject","comment":"policy rule"}`)
	if err != nil || decision.Decision != "reject" || strings.HasPrefix(decision.Comment, "[audit_error]") {
		t.Fatal("policy rejection confused with failure")
	}
}

func TestAuditTemperatureRequestAndFailure(t *testing.T) {
	zero, custom := 0.0, 0.6
	for _, test := range []struct {
		name        string
		temperature *float64
		want        float64
		status      int
	}{
		{"default", nil, 0.1, 200}, {"zero", &zero, 0, 200},
		{"custom", &custom, 0.6, 200}, {"failure", &custom, 0.6, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["temperature"] != test.want {
					t.Errorf("temperature=%v", body["temperature"])
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				if test.status != 200 {
					_, _ = w.Write([]byte(`{"error":{"message":"invalid temperature: only 0.6 allowed; secret-test-key private-content"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"decision\":\"approve\",\"comment\":\"read only\"}"}}]}`))
			}))
			defer server.Close()
			h := &AgentHandler{logger: zap.NewNop(), config: &config.Config{Hitl: config.HitlConfig{
				AuditModel: config.OpenAIConfig{APIKey: "secret-test-key", Model: "audit", BaseURL: server.URL, Temperature: test.temperature},
			}}}
			decision := h.auditAgentReview(context.Background(), "approval", "query_assets", nil)
			if test.status == 200 && decision.Decision != "approve" {
				t.Fatalf("%+v", decision)
			}
			if test.status != 200 {
				if decision.Decision != "reject" || !strings.HasPrefix(decision.Comment, "[audit_error]") || !strings.Contains(decision.Comment, "HTTP 400") {
					t.Fatalf("%+v", decision)
				}
				if strings.Contains(decision.Comment, "secret-test-key") || strings.Contains(decision.Comment, "private-content") {
					t.Fatal("upstream data leaked")
				}
			}
		})
	}
}

func TestAuditTemperatureYAMLPersistence(t *testing.T) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte("hitl:\n  audit_model:\n    temperature: 0.6\n"), &document); err != nil {
		t.Fatal(err)
	}
	zero, precise := 0.0, 0.65
	cfg := config.HitlConfig{AuditModel: config.OpenAIConfig{Temperature: &zero}}
	for _, temperature := range []*float64{&zero, &precise, nil} {
		cfg.AuditModel.Temperature = temperature
		updateHitlConfig(&document, cfg)
		data, err := yaml.Marshal(&document)
		if err != nil {
			t.Fatal(err)
		}
		var decoded config.Config
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		got := decoded.Hitl.AuditModel.Temperature
		if temperature == nil && got != nil || temperature != nil && (got == nil || *got != *temperature) {
			t.Fatalf("round trip: %s", data)
		}
	}
}
