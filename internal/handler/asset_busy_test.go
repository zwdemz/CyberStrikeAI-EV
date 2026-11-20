package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/database"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// TestAssetImportBusyResponse holds an external writer beyond every short test
// timeout and checks that the HTTP caller gets a retryable, sanitized response.
func TestAssetImportBusyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "asset-import.db")
	setup, err := database.NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if err := setup.Close(); err != nil {
		t.Fatal(err)
	}
	pool, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=1&_txlock=immediate&_busy_timeout=20")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	blocker, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_txlock=immediate&_busy_timeout=1000")
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	tx, err := blocker.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	router := gin.New()
	router.POST("/api/assets/import", NewAssetHandler(&database.DB{DB: pool}, zap.NewNop()).Import)
	request := httptest.NewRequest(http.MethodPost, "/api/assets/import", strings.NewReader(`{"assets":[{"ip":"192.0.2.77","port":443,"protocol":"https"}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "database is locked") {
		t.Fatalf("busy import returned status=%d body=%s", response.Code, response.Body.String())
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow("SELECT COUNT(*) FROM assets WHERE ip='192.0.2.77'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed import partially committed: count=%d err=%v", count, err)
	}
}
