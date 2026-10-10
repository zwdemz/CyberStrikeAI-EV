package mcp

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func TestEndpointSchemeProxyBoundary(t *testing.T) {
	server := NewServer(nil)
	if err := server.ConfigureTrustedProxies([]string{"192.0.2.0/24", "2001:db8::1"}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, peer, header, want string
		tls                      bool
	}{
		{"trusted", "192.0.2.1:1234", "https", "https", false},
		{"untrusted", "198.51.100.1:1234", "https", "http", false},
		{"ipv6", "[2001:db8::1]:1234", "https", "https", false},
		{"chain", "192.0.2.1:1234", "https,http", "http", false},
		{"invalid", "192.0.2.1:1234", "javascript", "http", false},
		{"tls", "192.0.2.1:1234", "http", "https", true},
		{"malformed-peer", "192.0.2.1", "https", "http", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/mcp", nil)
			request.RemoteAddr = test.peer
			request.Header.Set("X-Forwarded-Proto", test.header)
			if test.tls {
				request.TLS = &tls.ConnectionState{}
			}
			if got := server.endpointScheme(request); got != test.want {
				t.Fatalf("got %s want %s", got, test.want)
			}
		})
	}
	if err := server.ConfigureTrustedProxies([]string{"0.0.0.0/0"}); err == nil {
		t.Fatal("accepted catch-all proxy")
	}
	request := httptest.NewRequest("GET", "/mcp", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Add("X-Forwarded-Proto", "https")
	request.Header.Add("X-Forwarded-Proto", "http")
	if server.endpointScheme(request) != "http" {
		t.Fatal("accepted duplicate headers")
	}
	if err := server.ConfigureTrustedProxies(nil); err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Forwarded-Proto", "https")
	if server.endpointScheme(request) != "http" {
		t.Fatal("trusted proxy after clearing configuration")
	}
}
