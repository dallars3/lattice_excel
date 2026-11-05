package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	sharev1 "excel/api/share/v1"
	authredis "excel_web_api/redis"
)

type rateLimitCall struct {
	scope    string
	identity string
	limit    int64
	window   time.Duration
}

type pipelineRateLimiterStub struct {
	mu         sync.Mutex
	calls      []rateLimitCall
	allowCount int
	alwaysDeny bool
}

func (stub *pipelineRateLimiterStub) Allow(
	_ context.Context,
	scope string,
	identity string,
	limit int64,
	window time.Duration,
) (authredis.RateLimitResult, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.calls = append(stub.calls, rateLimitCall{
		scope:    scope,
		identity: identity,
		limit:    limit,
		window:   window,
	})
	allowed := !stub.alwaysDeny && len(stub.calls) <= stub.allowCount
	return authredis.RateLimitResult{
		Allowed:    allowed,
		RetryAfter: time.Second,
	}, nil
}

func TestPipelineRoutesReturnsRegisteredMethodAndPattern(t *testing.T) {
	pipeline := NewPipeline(nil, nil, nil, nil)
	pipeline.Public(http.MethodGet, "/healthz", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	pipeline.SessionRequired(http.MethodPost, "/session-only", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	pipeline.BaseAuth(
		http.MethodGet,
		"/api/v1/bases/{base_id}/records",
		"view",
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	)

	routes := pipeline.Routes()
	want := []string{
		"GET /api/v1/bases/{base_id}/records",
		"GET /healthz",
		"POST /session-only",
	}
	if !slices.Equal(routes, want) {
		t.Fatalf("Routes() = %v, want %v", routes, want)
	}

	pipeline.Public(http.MethodGet, "/late", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if len(routes) != len(want) {
		t.Fatalf("Routes() returned shared storage: length = %d, want %d", len(routes), len(want))
	}
}

func TestPipelineRateLimitsSensitiveRoutesWith5003(t *testing.T) {
	tests := []struct {
		name              string
		method            string
		path              string
		pattern           string
		action            string
		sessionRequired   bool
		baseID            string
		wantScope         string
		wantLimit         int64
		wantExchangeCalls int
	}{
		{
			name:      "register",
			method:    http.MethodPost,
			path:      "/api/v1/auth/register",
			pattern:   "/api/v1/auth/register",
			wantScope: "register",
			wantLimit: 10,
		},
		{
			name:      "login",
			method:    http.MethodPost,
			path:      "/api/v1/auth/login",
			pattern:   "/api/v1/auth/login",
			wantScope: "login",
			wantLimit: 20,
		},
		{
			name:              "record write",
			method:            http.MethodPost,
			path:              "/api/v1/bases/base_1/records",
			pattern:           "/api/v1/bases/{base_id}/records",
			action:            sharev1.Action_ACTION_CREATE_RECORD.String(),
			sessionRequired:   true,
			wantScope:         "record_write",
			wantLimit:         120,
			wantExchangeCalls: 1,
		},
		{
			name:              "comment mention write",
			method:            http.MethodPost,
			path:              "/api/v1/records/record_1/comments",
			pattern:           "/api/v1/records/{record_id}/comments",
			action:            sharev1.Action_ACTION_COMMENT.String(),
			sessionRequired:   true,
			baseID:            "base_1",
			wantScope:         "comment_write",
			wantLimit:         120,
			wantExchangeCalls: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			iam := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "enterprise_1")}
			share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
			pipeline := NewPipeline(iam, share, nil, nil)
			limiter := &pipelineRateLimiterStub{allowCount: 1}
			pipeline.rateLimiter = limiter
			handlerCalls := 0
			handler := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				handlerCalls++
				writeSuccess(response, map[string]bool{"ok": true})
			})
			if test.action == "" {
				pipeline.Public(test.method, test.pattern, handler)
			} else if test.sessionRequired {
				pipeline.BaseAuth(test.method, test.pattern, test.action, handler)
			}

			for attempt := 1; attempt <= 2; attempt++ {
				response := performPipelineRequest(t, pipeline, test.method, test.path, test.baseID)
				if attempt == 1 {
					if response.Code != http.StatusOK {
						t.Fatalf("attempt %d status = %d, want 200", attempt, response.Code)
					}
					continue
				}
				if response.Code != http.StatusTooManyRequests {
					t.Fatalf("attempt %d status = %d, want 429", attempt, response.Code)
				}
				var envelope struct {
					Code int `json:"code"`
				}
				if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
					t.Fatalf("decode rate-limit response: %v", err)
				}
				if envelope.Code != 5003 {
					t.Fatalf("attempt %d code = %d, want 5003", attempt, envelope.Code)
				}
			}

			if len(limiter.calls) != 2 {
				t.Fatalf("rate limiter calls = %d, want 2", len(limiter.calls))
			}
			for _, call := range limiter.calls {
				if call.scope != test.wantScope ||
					call.identity != "192.0.2.1" ||
					call.limit != test.wantLimit ||
					call.window != time.Minute {
					t.Fatalf("rate limiter call = %+v", call)
				}
			}
			if handlerCalls != 1 || iam.exchangeCalls != test.wantExchangeCalls {
				t.Fatalf(
					"calls = handler:%d exchange:%d, want handler:1 exchange:%d",
					handlerCalls,
					iam.exchangeCalls,
					test.wantExchangeCalls,
				)
			}
		})
	}
}

func TestPipelineDoesNotRateLimitUnrelatedRoutes(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "enterprise_1")}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	pipeline := NewPipeline(iam, share, nil, nil)
	limiter := &pipelineRateLimiterStub{alwaysDeny: true}
	pipeline.rateLimiter = limiter
	pipeline.Public(http.MethodGet, "/healthz", http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeSuccess(response, map[string]bool{"ok": true})
	}))
	pipeline.BaseAuth(
		http.MethodGet,
		"/api/v1/bases/{base_id}/tables",
		sharev1.Action_ACTION_VIEW.String(),
		http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			writeSuccess(response, map[string]bool{"ok": true})
		}),
	)

	for _, test := range []struct {
		path    string
		session bool
	}{
		{path: "/healthz"},
		{path: "/api/v1/bases/base_1/tables", session: true},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.RemoteAddr = "192.0.2.1:1234"
		if test.session {
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session_1"})
		}
		response := httptest.NewRecorder()
		pipeline.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", test.path, response.Code)
		}
	}
	if len(limiter.calls) != 0 {
		t.Fatalf("unrelated routes called rate limiter %d times, want zero", len(limiter.calls))
	}
}

func TestContractHTTPRouteInventory(t *testing.T) {
	pipeline := NewPipeline(nil, nil, nil, nil)
	pipeline.RegisterAuthRoutes(&pipelineIamRPC{})
	RegisterRoutes(pipeline, &recordsWriteGatewayStub{})
	RegisterIamRoutes(pipeline, nil)
	RegisterAuditRoutes(pipeline, nil)
	RegisterWorkspaceRoutes(pipeline, nil, nil)
	RegisterStructureRoutes(pipeline, nil)
	RegisterCommentRoutes(pipeline, nil)
	RegisterShareRoutes(pipeline, nil)
	RegisterNotifyRoutes(pipeline, nil)
	RegisterFileRoutes(pipeline, nil)
	RegisterImportExportRoutes(pipeline, nil)
	RegisterTrackingRoutes(pipeline, nil, nil)

	routes := pipeline.Routes()
	// H-95 GET /s/{link_token} 是顶层 mux 直挂的分享重定向入口（非管线路由），
	// 契约清单补入以对齐接口文档 §2（见 cmd/main.go: mux.Handle("/s/{link_token}", ...)）。
	routes = append(routes, http.MethodGet+" /s/{link_token}")
	slices.Sort(routes)
	if len(routes) == 0 {
		t.Fatal("registered HTTP route inventory is empty")
	}
	for index, route := range routes {
		if index > 0 && route == routes[index-1] {
			t.Fatalf("duplicate HTTP route registration: %s", route)
		}
		t.Logf("CONTRACT_ROUTE\t%s", route)
	}
}

func performPipelineRequest(
	t *testing.T,
	pipeline *Pipeline,
	method string,
	path string,
	baseID string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	request.RemoteAddr = "192.0.2.1:1234"
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session_1"})
	if baseID != "" {
		request = request.WithContext(WithBaseID(request.Context(), baseID))
	}
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)
	return response
}
