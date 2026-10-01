package typesafe_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"cyberstrike-ai/internal/typesafe"
)

type recordingTransport struct {
	url string
}

func (transport *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.url = request.URL.String()
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"answers":{}}`)), Header: make(http.Header)}, nil
}

func TestClientEndpointAllowlist(t *testing.T) {
	for _, test := range []struct {
		name, requested, allowed, wantURL string
	}{
		{name: "default", wantURL: typesafe.DefaultBaseURL},
		{name: "default trailing slash", requested: typesafe.DefaultBaseURL + "/", wantURL: typesafe.DefaultBaseURL},
		{name: "custom gateway path", requested: "https://gateway.example/jev/", allowed: " https://gateway.example/jev/ , https://backup.example ", wantURL: "https://gateway.example/jev"},
		{name: "explicit internal gateway", requested: "http://127.0.0.1:8080", allowed: "http://127.0.0.1:8080", wantURL: "http://127.0.0.1:8080"},
		{name: "localhost", requested: "http://127.0.0.1:8080"},
		{name: "private network", requested: "http://192.168.0.1"},
		{name: "metadata", requested: "http://169.254.169.254/latest/meta-data/"},
		{name: "ipv6 loopback", requested: "http://[::1]"},
		{name: "host suffix", requested: "https://api.typesafe.ai.attacker.example"},
		{name: "userinfo", requested: "https://api.typesafe.ai@127.0.0.1"},
		{name: "userinfo on trusted host", requested: "https://test-secret@api.typesafe.ai"},
		{name: "query", requested: typesafe.DefaultBaseURL + "?token=test-secret"},
		{name: "empty query", requested: typesafe.DefaultBaseURL + "?"},
		{name: "fragment", requested: typesafe.DefaultBaseURL + "#test-secret"},
		{name: "scheme downgrade", requested: "http://api.typesafe.ai"},
		{name: "different port", requested: "https://api.typesafe.ai:8443"},
		{name: "invalid port", requested: "https://api.typesafe.ai:65536"},
		{name: "port zero", requested: "https://api.typesafe.ai:0"},
		{name: "different path", requested: "https://gateway.example/other", allowed: "https://gateway.example/jev"},
		{name: "relative URL", requested: "//api.typesafe.ai"},
		{name: "non HTTP URL", requested: "file:///etc/passwd"},
		{name: "malformed URL", requested: "https://api.typesafe.ai/%"},
		{name: "invalid operator entry", allowed: "https://test-secret@gateway.example"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(typesafe.AllowedBaseURLsEnv, test.allowed)
			transport := &recordingTransport{}
			client, err := typesafe.NewClient(test.requested, "test-secret", "", &http.Client{Transport: transport})
			if test.wantURL == "" {
				if err == nil || client != nil || transport.url != "" {
					t.Fatal("unapproved endpoint was accepted or contacted")
				}
				if strings.Contains(err.Error(), "test-secret") {
					t.Fatal("error disclosed request credentials")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.SystemOne(context.Background(), "ping", map[string]typesafe.Question{"ok": typesafe.Noul("ping?", "yes", "no")})
			if err != nil {
				t.Fatal(err)
			}
			if transport.url != test.wantURL+"/v1/systemone" {
				t.Fatalf("request URL = %q", transport.url)
			}
		})
	}
}

func TestClientRejectsRedirectsWithoutChangingCaller(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, status)
			}))
			defer gateway.Close()
			t.Setenv(typesafe.AllowedBaseURLsEnv, gateway.URL)
			var callerRedirects atomic.Int32
			caller := gateway.Client()
			caller.CheckRedirect = func(*http.Request, []*http.Request) error { callerRedirects.Add(1); return nil }
			client, err := typesafe.NewClient(gateway.URL, "test-secret", "", caller)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.SystemOne(context.Background(), "private state", map[string]typesafe.Question{"ok": typesafe.Noul("ping?", "yes", "no")})
			var apiError *typesafe.APIError
			if !errors.As(err, &apiError) || apiError.StatusCode != status {
				t.Fatalf("expected redirect failure, got %v", err)
			}
			if redirected.Load() != 0 || callerRedirects.Load() != 0 {
				t.Fatal("redirect escaped the approved endpoint")
			}
			if err := caller.CheckRedirect(nil, nil); err != nil || callerRedirects.Load() != 1 {
				t.Fatal("caller HTTP client was modified")
			}
		})
	}
}
