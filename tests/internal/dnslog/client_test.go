package dnslog_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/dnslog"

	"github.com/gorilla/websocket"
)

const fixtureToken = "private-fixture-token"
const fixtureDomain = "short.ddns.example.test"

type fixture struct {
	domains     interface{}
	allocation  interface{}
	status      int
	raw         string
	messages    []interface{}
	closeStream bool
	binary      bool
	wsStatus    int
	calls       atomic.Int32
	wsCalls     atomic.Int32
}

func newFixture() *fixture {
	return &fixture{domains: []string{"ddns.example.test.", "ddns.xn--gg8h.example.test.", "ddns.example.test."}, allocation: map[string]string{
		"mainDomain": "ddns.example.test.", "subDomain": "short", "fullDomain": fixtureDomain + ".", "token": fixtureToken,
	}}
}

func (f *fixture) client(t *testing.T, options dnslog.Options) *dnslog.Client {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		if f.raw != "" {
			fmt.Fprint(w, f.raw)
			return
		}
		switch r.URL.Path {
		case "/get_domain":
			if r.Method != http.MethodGet {
				t.Errorf("domain method: %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(f.domains)
		case "/get_sub_domain":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("invalid allocation method/content type")
			}
			if err := r.ParseForm(); err != nil || r.Form.Get("mainDomain") != "ddns.example.test." {
				t.Error("invalid allocation form")
			}
			_ = json.NewEncoder(w).Encode(f.allocation)
		case "/ws":
			f.wsCalls.Add(1)
			allocated := f.allocation.(map[string]string)
			if r.URL.Query().Get("token") != fixtureToken || r.URL.Query().Get("subDomain") != allocated["subDomain"] || r.URL.Query().Get("mainDomain") != allocated["mainDomain"] {
				t.Error("invalid WebSocket session fields")
			}
			if f.wsStatus != 0 {
				w.WriteHeader(f.wsStatus)
				fmt.Fprint(w, fixtureToken)
				return
			}
			upgrader := websocket.Upgrader{}
			connection, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			if connection.WriteJSON(message("connection", map[string]string{"status": "connected"})) != nil {
				return
			}
			for _, message := range f.messages {
				var body []byte
				if raw, ok := message.(string); ok {
					body = []byte(raw)
				} else {
					body, _ = json.Marshal(message)
				}
				kind := websocket.TextMessage
				if f.binary {
					kind = websocket.BinaryMessage
				}
				if connection.WriteMessage(kind, body) != nil {
					return
				}
			}
			if f.closeStream {
				return
			}
			_, _, _ = connection.ReadMessage()
		default:
			t.Errorf("unexpected fixture path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	options.BaseURL = server.URL
	options.Transport = server.Client().Transport.(*http.Transport)
	options.Transport.ForceAttemptHTTP2 = true
	client, err := dnslog.New(options)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func allocate(t *testing.T, client *dnslog.Client, owner string) string {
	t.Helper()
	result, err := client.Execute(context.Background(), owner, map[string]interface{}{"operation": "get_domain"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), fixtureToken) || strings.Contains(string(encoded), `"token"`) {
		t.Fatal("provider token exposed")
	}
	value := result.(map[string]interface{})
	if value["domain"] != fixtureDomain {
		t.Fatalf("domain: %v", value["domain"])
	}
	return value["session_id"].(string)
}

func record(id interface{}) map[string]interface{} {
	return map[string]interface{}{"FullDomain": "probe." + fixtureDomain + ".", "ClientIp": "192.0.2.1", "CreatedAt": "2026-01-01T00:00:00Z", "ID": id}
}

func message(kind string, data interface{}) map[string]interface{} {
	return map[string]interface{}{"type": kind, "data": data}
}

func TestProtocolAndDeduplication(t *testing.T) {
	f := newFixture()
	first := record("one")
	first["UUID"] = "uuid-one"
	first["Location"] = fixtureToken
	second := record(2)
	third := record(nil)
	outside := record("outside")
	outside["FullDomain"] = "not-" + fixtureDomain
	f.messages = []interface{}{
		message("history", []interface{}{first, first, outside}),
		message("new_record", second), message("new_records", []interface{}{second, third, third}),
	}
	client := f.client(t, dnslog.Options{})
	domains, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "list_domains"})
	if err != nil || len(domains.(map[string]interface{})["domains"].([]string)) != 2 {
		t.Fatalf("domains: %v, %v", domains, err)
	}
	id := allocate(t, client, "owner")
	value, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_records", "session_id": id, "wait_time": 1})
	if err != nil {
		t.Fatal(err)
	}
	result := value.(*dnslog.RecordsResult)
	if result.RecordCount != 3 || result.Status != "success" || result.Truncated {
		t.Fatalf("records: %+v", result)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), fixtureToken) {
		t.Fatal("record echoed provider token")
	}
}

func TestEmptyWindowAndScope(t *testing.T) {
	f := newFixture()
	f.messages = []interface{}{message("history", []interface{}{})}
	client := f.client(t, dnslog.Options{})
	id := allocate(t, client, "user:conversation-one")
	args := map[string]interface{}{"operation": "get_records", "session_id": id, "wait_time": json.Number("1")}
	for _, owner := range []string{"", "user:conversation-two", "other:conversation-one"} {
		if _, err := client.Execute(context.Background(), owner, args); err == nil {
			t.Fatal("unscoped/cross-scope read allowed")
		}
	}
	if f.wsCalls.Load() != 0 {
		t.Fatal("rejected scope reached provider")
	}
	value, err := client.Execute(context.Background(), "user:conversation-one", args)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(*dnslog.RecordsResult)
	if result.Status != "no_records" || result.RecordCount != 0 || result.Records == nil {
		t.Fatalf("empty result: %+v", result)
	}
}

func TestProviderAuthenticationFieldsArePreserved(t *testing.T) {
	f := newFixture()
	allocated := f.allocation.(map[string]string)
	allocated["mainDomain"] = "ddns.example.test"
	allocated["subDomain"] = "SHORT"
	allocated["fullDomain"] = "SHORT.ddns.example.test."
	f.messages = []interface{}{message("history", []interface{}{})}
	client := f.client(t, dnslog.Options{})
	id := allocate(t, client, "owner")
	if _, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_records", "session_id": id, "wait_time": 1}); err != nil {
		t.Fatal(err)
	}
}

func TestValidationBeforeNetwork(t *testing.T) {
	f := newFixture()
	client := f.client(t, dnslog.Options{})
	for _, args := range []map[string]interface{}{
		nil, {"operation": "unknown"}, {"operation": 1}, {"operation": "list_domains", "token": fixtureToken},
		{"operation": "list_domains", "main_domain": "example.test"},
		{"operation": "get_domain", "main_domain": "https://example.test"},
		{"operation": "get_domain", "main_domain": "bad..test"},
		{"operation": "get_domain", "main_domain": "-bad.test"},
		{"operation": "get_domain", "main_domain": "bad-.test"},
		{"operation": "get_domain", "main_domain": strings.Repeat("a", 64) + ".test"},
		{"operation": "get_domain", "main_domain": strings.Repeat("a", 254)},
		{"operation": "get_domain", "wait_time": 1}, {"operation": "get_domain", "session_id": "unexpected"},
		{"operation": "get_records", "session_id": "not-a-session"},
		{"operation": "get_records", "session_id": "00000000-0000-0000-0000-000000000001", "main_domain": "example.test"},
	} {
		if _, err := client.Execute(context.Background(), "owner", args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	for _, seconds := range []interface{}{0, 31, -1, 1.1, "1", true, json.Number("bad"), math.NaN(), math.Inf(1)} {
		if _, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_records", "session_id": "00000000-0000-0000-0000-000000000001", "wait_time": seconds}); err == nil {
			t.Errorf("accepted wait_time %v", seconds)
		}
	}
	if f.calls.Load() != 0 {
		t.Fatal("invalid request reached network")
	}
}

func TestProviderValidation(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fixture)
	}{
		{"status", func(f *fixture) { f.status = 503 }},
		{"redirect", func(f *fixture) { f.status = 302 }},
		{"nonjson", func(f *fixture) { f.raw = fixtureToken }},
		{"oversize", func(f *fixture) { f.raw = strings.Repeat("x", (1<<20)+1) }},
		{"empty", func(f *fixture) { f.domains = []string{} }},
		{"too-many", func(f *fixture) { f.domains = make([]string, 129) }},
		{"invalid-domain", func(f *fixture) { f.domains = []string{"domain.test/"} }},
		{"different-main", func(f *fixture) { f.allocation.(map[string]string)["mainDomain"] = "elsewhere.test" }},
		{"different-full", func(f *fixture) { f.allocation.(map[string]string)["fullDomain"] = "wrong.ddns.example.test" }},
		{"missing-token", func(f *fixture) { f.allocation.(map[string]string)["token"] = "" }},
		{"token-control", func(f *fixture) { f.allocation.(map[string]string)["token"] = "private\nfixture" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture()
			test.configure(f)
			client := f.client(t, dnslog.Options{})
			_, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_domain"})
			if err == nil || strings.Contains(err.Error(), fixtureToken) {
				t.Fatalf("unsafe/missing error: %v", err)
			}
		})
	}
}

func TestSelectionCapacityAndExpiry(t *testing.T) {
	f := newFixture()
	client := f.client(t, dnslog.Options{MaxSessions: 1, SessionTTL: 50 * time.Millisecond})
	for _, domain := range []string{"absent.example.test", "DDNS.EXAMPLE.TEST."} {
		value, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_domain", "main_domain": domain})
		if strings.HasPrefix(domain, "absent") {
			if err == nil {
				t.Fatal("unlisted main domain accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		id := value.(map[string]interface{})["session_id"].(string)
		if _, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_domain"}); err == nil {
			t.Fatal("capacity bypass")
		}
		time.Sleep(60 * time.Millisecond)
		if _, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_records", "session_id": id}); err == nil {
			t.Fatal("expired session accepted")
		}
		allocate(t, client, "owner")
		time.Sleep(60 * time.Millisecond)
		allocate(t, client, "owner")
	}
}

func TestStreamFailuresNeverBecomeEmptySuccess(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fixture)
	}{
		{"closed", func(f *fixture) { f.closeStream = true }},
		{"handshake", func(f *fixture) { f.wsStatus = 401 }},
		{"binary", func(f *fixture) { f.binary = true; f.messages = []interface{}{"data"} }},
		{"malformed", func(f *fixture) { f.messages = []interface{}{"not json"} }},
		{"unknown-message", func(f *fixture) { f.messages = []interface{}{message("error", fixtureToken)} }},
		{"invalid-ack", func(f *fixture) { f.messages = []interface{}{message("connection", nil)} }},
		{"null-history", func(f *fixture) { f.messages = []interface{}{message("history", nil)} }},
		{"null-record", func(f *fixture) { f.messages = []interface{}{message("new_record", nil)} }},
		{"object-history", func(f *fixture) { f.messages = []interface{}{message("history", record(1))} }},
		{"array-record", func(f *fixture) { f.messages = []interface{}{message("new_record", []interface{}{})} }},
		{"oversize-message", func(f *fixture) { f.messages = []interface{}{strings.Repeat("x", (1<<20)+1)} }},
		{"bad-id", func(f *fixture) {
			f.messages = []interface{}{message("new_record", record(map[string]string{"bad": "id"}))}
		}},
		{"long-id", func(f *fixture) { f.messages = []interface{}{message("new_record", record(strings.Repeat("x", 1025)))} }},
		{"long-field", func(f *fixture) {
			r := record(1)
			r["Location"] = strings.Repeat("x", 1025)
			f.messages = []interface{}{message("new_record", r)}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture()
			test.configure(f)
			client := f.client(t, dnslog.Options{})
			id := allocate(t, client, "owner")
			_, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_records", "session_id": id, "wait_time": 1})
			if err == nil || strings.Contains(err.Error(), fixtureToken) {
				t.Fatalf("unsafe/missing stream error: %v", err)
			}
		})
	}
}

func TestLimitsCancellationAndConcurrentSessions(t *testing.T) {
	t.Run("numeric-id-precision", func(t *testing.T) {
		f := newFixture()
		f.messages = []interface{}{message("history", []interface{}{record(json.Number("9007199254740992")), record(json.Number("9007199254740993"))})}
		client := f.client(t, dnslog.Options{})
		id := allocate(t, client, "owner")
		value, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_records", "session_id": id, "wait_time": 1})
		if err != nil || value.(*dnslog.RecordsResult).RecordCount != 2 {
			t.Fatalf("rounded provider IDs: %v %v", value, err)
		}
	})
	t.Run("total-byte-limit", func(t *testing.T) {
		f := newFixture()
		body := `{"type":"history","data":[],"padding":"` + strings.Repeat("x", 900000) + `"}`
		for i := 0; i < 5; i++ {
			f.messages = append(f.messages, body)
		}
		client := f.client(t, dnslog.Options{})
		id := allocate(t, client, "owner")
		value, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_records", "session_id": id})
		if err != nil || value.(*dnslog.RecordsResult).Status != "partial" || !value.(*dnslog.RecordsResult).Truncated {
			t.Fatalf("byte cap: %v %v", value, err)
		}
	})
	t.Run("record-limit", func(t *testing.T) {
		f := newFixture()
		rows := make([]interface{}, 101)
		for i := range rows {
			rows[i] = record(i)
		}
		f.messages = []interface{}{message("history", rows)}
		client := f.client(t, dnslog.Options{})
		id := allocate(t, client, "owner")
		value, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_records", "session_id": id})
		if err != nil || !value.(*dnslog.RecordsResult).Truncated || value.(*dnslog.RecordsResult).RecordCount != 100 {
			t.Fatalf("cap: %v %v", value, err)
		}
	})
	t.Run("message-limit", func(t *testing.T) {
		f := newFixture()
		for i := 0; i < 256; i++ {
			f.messages = append(f.messages, message("history", []interface{}{}))
		}
		client := f.client(t, dnslog.Options{})
		id := allocate(t, client, "owner")
		value, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_records", "session_id": id})
		if err != nil || !value.(*dnslog.RecordsResult).Truncated {
			t.Fatalf("message cap: %v %v", value, err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		f := newFixture()
		client := f.client(t, dnslog.Options{})
		id := allocate(t, client, "owner")
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		started := time.Now()
		if _, err := client.Execute(ctx, "owner", map[string]interface{}{"operation": "get_records", "session_id": id, "wait_time": 30}); err == nil || time.Since(started) > time.Second {
			t.Fatalf("cancellation: %v", err)
		}
	})
	t.Run("expiry-during-observation", func(t *testing.T) {
		f := newFixture()
		client := f.client(t, dnslog.Options{SessionTTL: 100 * time.Millisecond})
		id := allocate(t, client, "owner")
		if _, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_records", "session_id": id}); err == nil {
			t.Fatal("expiry treated as success")
		}
	})
	t.Run("concurrent-capacity", func(t *testing.T) {
		f := newFixture()
		client := f.client(t, dnslog.Options{MaxSessions: 2})
		var successful atomic.Int32
		var group sync.WaitGroup
		for i := 0; i < 10; i++ {
			group.Add(1)
			go func() {
				defer group.Done()
				if _, err := client.Execute(context.Background(), "owner", map[string]interface{}{"operation": "get_domain"}); err == nil {
					successful.Add(1)
				}
			}()
		}
		group.Wait()
		if successful.Load() != 2 {
			t.Fatalf("concurrent sessions: %d", successful.Load())
		}
	})
}

func TestClientConfigurationAndTransportFailure(t *testing.T) {
	if _, err := dnslog.New(dnslog.Options{}); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://example.test", "https://u:p@example.test", "https://example.test/path", "https://example.test?x=1", "https://example.test#fragment", "://bad", "https://"} {
		if _, err := dnslog.New(dnslog.Options{BaseURL: endpoint}); err == nil {
			t.Errorf("accepted endpoint %s", endpoint)
		}
	}
	for _, options := range []dnslog.Options{{MaxSessions: -1}, {SessionTTL: -time.Second}, {Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}} {
		if _, err := dnslog.New(options); err == nil {
			t.Fatal("accepted invalid options")
		}
	}
	f := newFixture()
	client := f.client(t, dnslog.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Execute(ctx, "owner", map[string]interface{}{"operation": "list_domains"}); err == nil {
		t.Fatal("cancelled transport succeeded")
	}
}
