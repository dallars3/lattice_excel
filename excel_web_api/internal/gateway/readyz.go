package gateway

import (
	"context"
	"io"
	"net/http"
)

// ReadinessChecker verifies one external dependency needed to serve traffic.
type ReadinessChecker interface {
	Ready(context.Context) error
}

// Readyz reports whether all configured dependencies are ready.
func Readyz(checkers ...ReadinessChecker) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			http.Error(response, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, checker := range checkers {
			if err := checker.Ready(request.Context()); err != nil {
				response.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(response, "not ready")
				return
			}
		}
		response.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(response, "ok")
	})
}
