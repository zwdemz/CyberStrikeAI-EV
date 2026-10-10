package testproxy_test

import (
	"bytes"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/handler"
	"cyberstrike-ai/internal/security"
	"cyberstrike-ai/internal/testproxy"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestManagementAPIRequiresAuthenticationAndValidatesImports(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "proxy.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.InitTestProxyStorage(); err != nil {
		t.Fatal(err)
	}
	service, _ := testproxy.New(db, "")
	testproxy.Install(service)
	defer testproxy.Install(nil)
	auth := security.NewAuthManager(1)
	if _, err = auth.AttachRBACStore(db); err != nil {
		t.Fatal(err)
	}
	hash, _ := security.HashPassword("test-only-admin-password")
	if err = db.UpdateRBACAdminPassword(hash); err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.Authenticate("admin", "test-only-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	handler.RegisterTestProxyRoutes(r.Group("/api"), auth, db, zap.NewNop())
	call := func(method, path, body, credential string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/test-proxy-pools"+path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, tc := range []struct{ method, path string }{{"GET", ""}, {"DELETE", "/pool"}, {"POST", "/import"}, {"POST", "/probe"}, {"GET", "/preference"}, {"PUT", "/preference"}, {"GET", "/binding/project"}, {"PUT", "/binding/project"}} {
		if w := call(tc.method, tc.path, "{}", ""); w.Code != 401 {
			t.Fatalf("unauthorized %s = %d", tc.path, w.Code)
		}
	}
	if _, err = db.CreateRBACUser("viewer-fixture", "Viewer", hash, true, nil); err != nil {
		t.Fatal(err)
	}
	viewer, _, err := auth.Authenticate("viewer-fixture", "test-only-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	if w := call("GET", "", "", viewer); w.Code != 403 {
		t.Fatalf("non-admin list = %d", w.Code)
	}
	for _, body := range []string{`{}`, `{"name":"pool","text":"http://localhost:0"}`, `{"name":"pool","text":"http://localhost:80","per_node":99}`, `{"name":"pool","text":"http://user:secret@localhost:80"}`} {
		if w := call("POST", "/import", body, token); w.Code != 400 {
			t.Fatalf("invalid import %d", w.Code)
		}
	}
	w := call("POST", "/import", `{"name":"local fixture","text":"http://localhost:8181"}`, token)
	if w.Code != http.StatusOK {
		t.Fatalf("valid import failed %d", w.Code)
	}
	var pool testproxy.Pool
	if err = json.Unmarshal(w.Body.Bytes(), &pool); err != nil || pool.ID == "" {
		t.Fatal("missing pool")
	}
	if w = call("GET", "", "", token); w.Code != 200 {
		t.Fatal("list failed")
	}
	if w = call("PUT", "/binding/nonexistent", `{"pool_id":"`+pool.ID+`"}`, token); w.Code == 200 {
		t.Fatal("bound missing project")
	}
	if w = call("PUT", "/preference", `{"pool_id":"`+pool.ID+`","user_id":"someone-else"}`, token); w.Code != 200 {
		t.Fatalf("save preference %d", w.Code)
	}
	if w = call("GET", "/preference", "", token); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(pool.ID)) {
		t.Fatal("preference not restored")
	}
	if w = call("PUT", "/preference", `{"pool_id":"missing"}`, token); w.Code != 400 {
		t.Fatal("accepted missing pool")
	}
	if w = call("PUT", "/preference", `{"pool_id":""}`, viewer); w.Code != 403 {
		t.Fatal("unprivileged preference write")
	}
	if w = call("PUT", "/preference", `{`, token); w.Code != 400 {
		t.Fatal("accepted invalid JSON")
	}

}
