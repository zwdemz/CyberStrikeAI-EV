package handler_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/handler"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// localAPITransport confines every API request to the fixture, including clients
// that resolve a built-in provider URL when no configuration object is supplied.
type localAPITransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (transport localAPITransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.URL.Scheme = transport.target.Scheme
	cloned.URL.Host = transport.target.Host
	cloned.Host = transport.target.Host
	return transport.base.RoundTrip(cloned)
}

func TestAPIUserAgentReachesProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, provider := range []string{"fofa", "zoomeye", "quake", "shodan"} {
		for _, userAgent := range []string{"", "Company-API-Client/1.0", "environment-only"} {
			t.Run(provider+"/"+userAgent, func(t *testing.T) {
				for _, suffix := range []string{"API_KEY", "BASE_URL", "BEARER_TOKEN", "AUTH_MODE", "FALLBACK_BASE_URLS"} {
					t.Setenv(strings.ToUpper(provider)+"_"+suffix, "")
				}
				received := make(chan string, 4)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					received <- r.Header.Get("User-Agent")
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"code":0,"size":0,"total":0,"results":[],"data":[],"matches":[]}`))
				}))
				defer server.Close()
				target, err := url.Parse(server.URL)
				if err != nil {
					t.Fatal(err)
				}
				originalTransport := http.DefaultTransport
				http.DefaultTransport = localAPITransport{base: originalTransport, target: target}
				defer func() { http.DefaultTransport = originalTransport }()
				space := config.SpaceSearchConfig{BaseURL: server.URL, APIKey: "test-key"}
				cfg := &config.Config{APIClient: config.APIClientConfig{UserAgent: userAgent}, FOFA: config.FofaConfig{BaseURL: server.URL, APIKey: "test-key"}, ZoomEye: space, Quake: space, Shodan: space}
				want := cfg.APIClient.EffectiveUserAgent()
				if userAgent == "environment-only" {
					t.Setenv(strings.ToUpper(provider)+"_API_KEY", "test-key")
					t.Setenv(strings.ToUpper(provider)+"_BASE_URL", server.URL)
					cfg = nil
					want = "CyberStrikeAI"
				}
				client := handler.NewFofaHandler(cfg, zap.NewNop())
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodPost, "/api/fofa/search", strings.NewReader(`{"provider":"`+provider+`","query":"example.com","size":1}`))
				ctx.Request.Header.Set("Content-Type", "application/json")
				client.Search(ctx)
				if recorder.Code != http.StatusOK {
					t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
				}
				select {
				case got := <-received:
					if got != want {
						t.Fatalf("got %q, want %q", got, want)
					}
				default:
					t.Fatal("provider received no request")
				}
			})
		}
	}
}
