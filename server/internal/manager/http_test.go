package manager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeni0101/vpn/server/internal/config"
	"github.com/jeni0101/vpn/server/internal/security"
	"github.com/jeni0101/vpn/server/internal/store"
)

func TestEmptyCollectionsAreJSONArrays(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/manager.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sealer, err := security.NewSealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(config.Manager{}, db, sealer, &fakeWG{})
	handler := NewHTTPServer(service)

	for _, path := range []string{
		"/v1/audit?limit=100",
		"/v1/usage?bucket=hour&from=2026-07-29T00:00:00Z&to=2026-07-29T01:00:00Z",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(context.Background())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		key := "events"
		if path[4] == 'u' {
			key = "points"
		}
		if string(payload[key]) != "[]" {
			t.Fatalf("%s returned %s=%s, want []", path, key, payload[key])
		}
	}
}

func TestUsageRangeValidation(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/manager.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sealer, _ := security.NewSealer(make([]byte, 32))
	handler := NewHTTPServer(NewService(config.Manager{}, db, sealer, &fakeWG{}))
	from := time.Now().UTC().Add(-7 * 365 * 24 * time.Hour).Format(time.RFC3339)
	to := time.Now().UTC().Format(time.RFC3339)
	request := httptest.NewRequest(http.MethodGet, "/v1/usage?bucket=day&from="+from+"&to="+to, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized range returned %d", response.Code)
	}
}
