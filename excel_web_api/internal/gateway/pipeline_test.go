package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_web_api/deal"
	authredis "excel_web_api/redis"
)

type pipelineIamRPC struct {
	exchangeResponse *iamv1.ExchangeSessionResponse
	exchangeErr      error
	exchangeCalls    int
	requestIDs       []string
}

func (stub *pipelineIamRPC) Register(
	context.Context,
	*iamv1.RegisterRequest,
) (*iamv1.RegisterResponse, error) {
	panic("Register must not be called")
}

func (stub *pipelineIamRPC) Login(
	context.Context,
	*iamv1.LoginRequest,
) (*iamv1.LoginResponse, error) {
	panic("Login must not be called")
}

func (stub *pipelineIamRPC) Logout(
	context.Context,
	*iamv1.LogoutRequest,
) (*emptypb.Empty, error) {
	panic("Logout must not be called")
}

func (stub *pipelineIamRPC) ExchangeSession(
	ctx context.Context,
	_ *iamv1.ExchangeSessionRequest,
) (*iamv1.ExchangeSessionResponse, error) {
	stub.exchangeCalls++
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	stub.requestIDs = append(stub.requestIDs, outgoing.Get("x-request-id")...)
	return stub.exchangeResponse, stub.exchangeErr
}

func (stub *pipelineIamRPC) ChangePassword(
	context.Context,
	*iamv1.ChangePasswordRequest,
) (*emptypb.Empty, error) {
	panic("ChangePassword must not be called")
}

func (stub *pipelineIamRPC) GetMe(
	context.Context,
	*emptypb.Empty,
) (*iamv1.GetMeResponse, error) {
	panic("GetMe must not be called")
}

func (stub *pipelineIamRPC) UpdateProfile(
	context.Context,
	*iamv1.UpdateProfileRequest,
) (*iamv1.UpdateProfileResponse, error) {
	panic("UpdateProfile must not be called")
}

type pipelineShareRPC struct {
	checkResponse *sharev1.CheckAccessResponse
	checkErr      error
	checkCalls    int
	lastCheck     *sharev1.CheckAccessRequest
}

type directBusinessRPC struct {
	calls int
}

func (stub *directBusinessRPC) Get(context.Context) (*emptypb.Empty, error) {
	stub.calls++
	return &emptypb.Empty{}, nil
}

func (stub *pipelineShareRPC) CheckAccess(
	_ context.Context,
	request *sharev1.CheckAccessRequest,
) (*sharev1.CheckAccessResponse, error) {
	stub.checkCalls++
	stub.lastCheck = request
	return stub.checkResponse, stub.checkErr
}

func (stub *pipelineShareRPC) ResolveRole(
	context.Context,
	*sharev1.ResolveRoleRequest,
) (*sharev1.ResolveRoleResponse, error) {
	panic("ResolveRole must not be called")
}

func TestProtectedRoutesRejectMissingSessionBeforeAuthorizationOrBusiness(t *testing.T) {
	iam := &pipelineIamRPC{}
	share := &pipelineShareRPC{}
	pipeline := NewPipeline(iam, share, nil, nil)
	businessCalls := 0
	pipeline.SessionRequired(http.MethodGet, "/session-only", http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) {
			businessCalls++
		},
	))
	pipeline.BaseAuth(
		http.MethodGet,
		"/api/v1/bases/{base_id}/view",
		sharev1.Action_ACTION_VIEW.String(),
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			businessCalls++
		}),
	)

	server := httptest.NewServer(pipeline.Handler())
	defer server.Close()

	for _, path := range []string{
		"/session-only",
		"/api/v1/bases/base_1/view",
	} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		var envelope struct {
			Code int `json:"code"`
		}
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			response.Body.Close()
			t.Fatalf("decode %s response: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized || envelope.Code != deal.CodeUnauthenticated {
			t.Fatalf(
				"GET %s = HTTP %d code %d, want HTTP 401 code 2001",
				path,
				response.StatusCode,
				envelope.Code,
			)
		}
	}

	if iam.exchangeCalls != 0 || share.checkCalls != 0 || businessCalls != 0 {
		t.Fatalf(
			"calls = exchange:%d check:%d business:%d, want all zero",
			iam.exchangeCalls,
			share.checkCalls,
			businessCalls,
		)
	}
}

func TestSessionRequiredRejectsInvalidSessionBeforeBusiness(t *testing.T) {
	iam := &pipelineIamRPC{
		exchangeErr: status.Error(codes.Unauthenticated, "expired"),
	}
	pipeline := NewPipeline(iam, nil, nil, nil)
	businessCalls := 0
	pipeline.SessionRequired(http.MethodGet, "/session-only", http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) {
			businessCalls++
		},
	))

	request := httptest.NewRequest(http.MethodGet, "/session-only", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "expired-token"})
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != http.StatusUnauthorized || envelope.Code != deal.CodeUnauthenticated {
		t.Fatalf("response = HTTP %d code %d, want HTTP 401 code 2001", response.Code, envelope.Code)
	}
	if iam.exchangeCalls != 1 || businessCalls != 0 {
		t.Fatalf("calls = exchange:%d business:%d, want exchange one and business zero", iam.exchangeCalls, businessCalls)
	}
}

func TestBaseAuthRejectsDeniedAndExternalExportBeforeBusiness(t *testing.T) {
	tests := []struct {
		name       string
		action     sharev1.Action
		decision   *sharev1.CheckAccessResponse
		wantCode   int
		wantStatus int
	}{
		{
			name:   "role lacks action",
			action: sharev1.Action_ACTION_EDIT_ALL,
			decision: &sharev1.CheckAccessResponse{
				Allow:    false,
				Role:     sharev1.Role_ROLE_VIEWER,
				Internal: true,
				RowScope: sharev1.RowScope_ROW_SCOPE_ALL,
			},
			wantCode:   deal.CodePermissionDenied,
			wantStatus: http.StatusForbidden,
		},
		{
			name:   "external collaborator cannot export",
			action: sharev1.Action_ACTION_EXPORT,
			decision: &sharev1.CheckAccessResponse{
				Allow:    true,
				Role:     sharev1.Role_ROLE_EDITOR,
				Internal: false,
				RowScope: sharev1.RowScope_ROW_SCOPE_ALL,
			},
			wantCode:   deal.CodeExternalExportDenied,
			wantStatus: http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			iam := &pipelineIamRPC{
				exchangeResponse: &iamv1.ExchangeSessionResponse{
					Principal: &commonv1.Principal{
						UserId:       "user_1",
						EnterpriseId: "ent_1",
						Internal:     true,
					},
				},
			}
			share := &pipelineShareRPC{checkResponse: test.decision}
			pipeline := NewPipeline(iam, share, nil, nil)
			businessCalls := 0
			pipeline.BaseAuth(
				http.MethodPost,
				"/api/v1/bases/{base_id}/guarded",
				test.action.String(),
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					businessCalls++
				}),
			)

			request := httptest.NewRequest(http.MethodPost, "/api/v1/bases/base_1/guarded", nil)
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
			response := httptest.NewRecorder()
			pipeline.Handler().ServeHTTP(response, request)

			var envelope struct {
				Code int `json:"code"`
			}
			if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Code != test.wantStatus || envelope.Code != test.wantCode {
				t.Fatalf(
					"response = HTTP %d code %d, want HTTP %d code %d",
					response.Code,
					envelope.Code,
					test.wantStatus,
					test.wantCode,
				)
			}
			if businessCalls != 0 {
				t.Fatalf("business calls = %d, want zero", businessCalls)
			}
			if share.checkCalls != 1 {
				t.Fatalf("CheckAccess calls = %d, want one", share.checkCalls)
			}
			if share.lastCheck.GetBaseId() != "base_1" ||
				share.lastCheck.GetAction() != test.action ||
				share.lastCheck.GetPrincipal().GetUserId() != "user_1" {
				t.Fatalf("CheckAccess request = %v", share.lastCheck)
			}
		})
	}
}

func TestBaseAuthDirectTemplateCallsBusinessRPCOnceAndWrapsEnvelope(t *testing.T) {
	iam := &pipelineIamRPC{
		exchangeResponse: allowedSession("user_1", "ent_1"),
	}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	pipeline := NewPipeline(iam, share, nil, nil)
	business := &directBusinessRPC{}
	pipeline.BaseAuth(
		http.MethodGet,
		"/api/v1/bases/{base_id}/direct",
		"view",
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if _, err := business.Get(request.Context()); err != nil {
				writeBusinessError(response, err)
				return
			}
			writeSuccess(response, map[string]string{"base_id": request.PathValue("base_id")})
		}),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/bases/base_1/direct", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	var envelope struct {
		Code    int               `json:"code"`
		Message string            `json:"message"`
		Data    map[string]string `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != http.StatusOK || envelope.Code != 0 || envelope.Message != "ok" {
		t.Fatalf("response = HTTP %d envelope %+v, want success", response.Code, envelope)
	}
	if envelope.Data["base_id"] != "base_1" {
		t.Fatalf("data = %+v, want base_id=base_1", envelope.Data)
	}
	if business.calls != 1 {
		t.Fatalf("business RPC calls = %d, want one", business.calls)
	}
	if len(iam.requestIDs) != 1 || iam.requestIDs[0] == "" {
		t.Fatalf("x-request-id metadata = %v, want one non-empty value", iam.requestIDs)
	}
	if response.Header().Get(requestIDHeader) == "" {
		t.Fatal("response is missing X-Request-Id")
	}
}

func TestBaseAuthFailOpenWhenRedisUnavailableAndWarns(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	redisClient := goredis.NewClient(&goredis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  20 * time.Millisecond,
		ReadTimeout:  20 * time.Millisecond,
		WriteTimeout: 20 * time.Millisecond,
	})
	t.Cleanup(func() { _ = redisClient.Close() })

	iam := &pipelineIamRPC{
		exchangeResponse: allowedSession("user_1", "ent_1"),
	}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	pipeline := NewPipeline(iam, share, authredis.NewCache(redisClient, logger), logger)
	businessCalls := 0
	pipeline.BaseAuth(
		http.MethodGet,
		"/api/v1/bases/{base_id}/fail-open",
		"view",
		http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			businessCalls++
			writeSuccess(response, map[string]bool{"ok": true})
		}),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/bases/base_1/fail-open", nil)
	request.Header.Set("Authorization", "Bearer session-token")
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || businessCalls != 1 {
		t.Fatalf("response = HTTP %d business calls = %d, want HTTP 200 and one call", response.Code, businessCalls)
	}
	if iam.exchangeCalls != 1 || share.checkCalls != 1 {
		t.Fatalf("fallback calls = exchange:%d check:%d, want one each", iam.exchangeCalls, share.checkCalls)
	}
	if !bytes.Contains(logs.Bytes(), []byte("level=WARN")) {
		t.Fatalf("logs = %q, want WARNING", logs.String())
	}
}

func TestBaseAuthRedisFailureStillEnforcesCheckAccessDenial(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	redisClient := goredis.NewClient(&goredis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  20 * time.Millisecond,
		ReadTimeout:  20 * time.Millisecond,
		WriteTimeout: 20 * time.Millisecond,
	})
	t.Cleanup(func() { _ = redisClient.Close() })

	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "ent_1")}
	share := &pipelineShareRPC{
		checkResponse: &sharev1.CheckAccessResponse{
			Allow:    false,
			Internal: true,
		},
	}
	pipeline := NewPipeline(iam, share, authredis.NewCache(redisClient, logger), logger)
	businessCalls := 0
	pipeline.BaseAuth(
		http.MethodGet,
		"/api/v1/bases/{base_id}/denied",
		"view",
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			businessCalls++
		}),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/bases/base_1/denied", nil)
	request.Header.Set("Authorization", "Bearer session-token")
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != http.StatusForbidden || envelope.Code != deal.CodePermissionDenied {
		t.Fatalf("response = HTTP %d code %d, want HTTP 403 code 3001", response.Code, envelope.Code)
	}
	if businessCalls != 0 || share.checkCalls != 1 {
		t.Fatalf("calls = business:%d check:%d, want business zero and check one", businessCalls, share.checkCalls)
	}
}

func allowedSession(userID, enterpriseID string) *iamv1.ExchangeSessionResponse {
	return &iamv1.ExchangeSessionResponse{
		Principal: &commonv1.Principal{
			UserId:       userID,
			EnterpriseId: enterpriseID,
			Internal:     true,
		},
	}
}

func allowedDecision(internal bool) *sharev1.CheckAccessResponse {
	return &sharev1.CheckAccessResponse{
		Allow:    true,
		Role:     sharev1.Role_ROLE_EDITOR,
		Internal: internal,
		RowScope: sharev1.RowScope_ROW_SCOPE_ALL,
	}
}
