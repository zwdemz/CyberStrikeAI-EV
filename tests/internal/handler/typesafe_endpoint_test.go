package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"cyberstrike-ai/internal/handler"
	"cyberstrike-ai/internal/typesafe"
	"github.com/gin-gonic/gin"
)

func TestTypeSafeConnectionEndpointValidation(t *testing.T) {
	var calls atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-test","answers":{"ok":{"noul":1}}}`))
	}))
	defer gateway.Close()
	for _, test := range []struct {
		name, body, allowed string
		status              int
		wantCall            bool
	}{
		{name: "invalid JSON", body: `{`, status: http.StatusBadRequest},
		{name: "missing key", body: `{}`, status: http.StatusBadRequest},
		{name: "unapproved address", body: `{"api_key":"test-key","base_url":"` + gateway.URL + `"}`, status: http.StatusBadRequest},
		{name: "approved gateway", body: `{"api_key":"test-key","base_url":"` + gateway.URL + `"}`, allowed: gateway.URL, status: http.StatusOK, wantCall: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(typesafe.AllowedBaseURLsEnv, test.allowed)
			before := calls.Load()
			router := gin.New()
			configHandler := &handler.ConfigHandler{}
			router.POST("/config/test-typesafe", configHandler.TestTypeSafe)
			request := httptest.NewRequest(http.MethodPost, "/config/test-typesafe", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			if (calls.Load() == before+1) != test.wantCall {
				t.Fatal("unexpected gateway network activity")
			}
			if test.wantCall {
				var result struct {
					Success bool   `json:"success"`
					Model   string `json:"model"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !result.Success || result.Model != "jev-test" {
					t.Fatalf("unexpected connection result: %s", response.Body)
				}
			}
		})
	}
}
