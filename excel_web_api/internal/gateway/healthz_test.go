package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzReportsProcessAlive(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	Healthz().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "ok" {
		t.Fatalf("body = %q, want %q", body, "ok")
	}
}
