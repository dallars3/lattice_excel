package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type readyStub struct {
	err error
}

func (stub readyStub) Ready(context.Context) error {
	return stub.err
}

func TestReadyzReportsReadyWhenAllDependenciesAreAvailable(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()

	Readyz(readyStub{}, readyStub{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "ok" {
		t.Fatalf("body = %q, want %q", body, "ok")
	}
}

func TestReadyzReportsUnavailableWhenDependencyFails(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()

	Readyz(readyStub{}, readyStub{err: errors.New("redis unavailable")}).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if body := response.Body.String(); body != "not ready" {
		t.Fatalf("body = %q, want %q", body, "not ready")
	}
}
