package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type stubIamRPC struct {
	registerResponse      *iamv1.RegisterResponse
	registerErr           error
	loginResponse         *iamv1.LoginResponse
	loginErr              error
	logoutErr             error
	exchangeResponse      *iamv1.ExchangeSessionResponse
	exchangeErr           error
	changePasswordErr     error
	getMeResponse         *iamv1.GetMeResponse
	getMeErr              error
	updateProfileResponse *iamv1.UpdateProfileResponse
	updateProfileErr      error
	lastPrincipal         string
	lastRegister          *iamv1.RegisterRequest
	lastChangePassword    *iamv1.ChangePasswordRequest
	lastUpdateProfile     *iamv1.UpdateProfileRequest
	lastLogoutToken       string
}

func (stub *stubIamRPC) Register(_ context.Context, request *iamv1.RegisterRequest) (*iamv1.RegisterResponse, error) {
	stub.lastRegister = request
	return stub.registerResponse, stub.registerErr
}

func (stub *stubIamRPC) Login(context.Context, *iamv1.LoginRequest) (*iamv1.LoginResponse, error) {
	return stub.loginResponse, stub.loginErr
}

func (stub *stubIamRPC) Logout(_ context.Context, request *iamv1.LogoutRequest) (*emptypb.Empty, error) {
	stub.lastLogoutToken = request.GetToken()
	return &emptypb.Empty{}, stub.logoutErr
}

func (stub *stubIamRPC) ExchangeSession(
	context.Context,
	*iamv1.ExchangeSessionRequest,
) (*iamv1.ExchangeSessionResponse, error) {
	return stub.exchangeResponse, stub.exchangeErr
}

func (stub *stubIamRPC) ChangePassword(
	ctx context.Context,
	request *iamv1.ChangePasswordRequest,
) (*emptypb.Empty, error) {
	stub.capturePrincipal(ctx)
	stub.lastChangePassword = request
	return &emptypb.Empty{}, stub.changePasswordErr
}

func (stub *stubIamRPC) GetMe(ctx context.Context, _ *emptypb.Empty) (*iamv1.GetMeResponse, error) {
	stub.capturePrincipal(ctx)
	return stub.getMeResponse, stub.getMeErr
}

func (stub *stubIamRPC) UpdateProfile(
	ctx context.Context,
	request *iamv1.UpdateProfileRequest,
) (*iamv1.UpdateProfileResponse, error) {
	stub.capturePrincipal(ctx)
	stub.lastUpdateProfile = request
	return stub.updateProfileResponse, stub.updateProfileErr
}

func (stub *stubIamRPC) capturePrincipal(ctx context.Context) {
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	values := outgoing.Get("x-principal")
	if len(values) > 0 {
		stub.lastPrincipal = values[0]
	}
}

var _ rpc.IamRPC = (*stubIamRPC)(nil)

func TestLoginReturnsEnvelopeAndThirtyDaySIDCookie(t *testing.T) {
	token := "sess_test_token"
	stub := &stubIamRPC{
		loginResponse: &iamv1.LoginResponse{
			Token: token,
			User: &iamv1.User{
				Id:           "01J00000000000000000000001",
				Username:     "alice",
				DisplayName:  "Alice",
				EnterpriseId: "01J00000000000000000000002",
			},
		},
	}
	server := httptest.NewServer(authTestMux(stub))
	defer server.Close()

	response, err := http.Post(
		server.URL+"/api/v1/auth/login",
		"application/json",
		strings.NewReader(`{"username":"alice","password":"Pass1234"}`),
	)
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 0 || envelope.Message != "ok" {
		t.Fatalf("envelope = {code:%d message:%q}, want success", envelope.Code, envelope.Message)
	}
	var data struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Token != token {
		t.Fatalf("data.token = %q, want %q", data.Token, token)
	}

	cookies := response.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "sid" || cookies[0].Value != token {
		t.Fatalf("cookies = %v, want sid=%s", cookies, token)
	}
	if !cookies[0].HttpOnly || cookies[0].Path != "/" || cookies[0].MaxAge != int((30*24*time.Hour)/time.Second) {
		t.Fatalf("sid attributes = %+v, want HttpOnly Path=/ MaxAge=2592000", cookies[0])
	}
}

func TestAuthRoutesMapBusinessErrorsToStatusAndEnvelope(t *testing.T) {
	stub := &stubIamRPC{
		loginErr:    businessRPCError(deal.CodeLoginFailed),
		registerErr: businessRPCError(deal.CodeConflict),
	}
	server := httptest.NewServer(authTestMux(stub))
	defer server.Close()

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   int
	}{
		{
			name:       "login failure or lock",
			method:     http.MethodPost,
			path:       "/api/v1/auth/login",
			body:       `{"username":"locked","password":"Wrong1234"}`,
			wantStatus: http.StatusUnauthorized,
			wantCode:   deal.CodeLoginFailed,
		},
		{
			name:       "case-insensitive username conflict",
			method:     http.MethodPost,
			path:       "/api/v1/auth/register",
			body:       `{"username":"alice","password":"Pass1234"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   deal.CodeConflict,
		},
		{
			name:       "malformed request",
			method:     http.MethodPost,
			path:       "/api/v1/auth/register",
			body:       `{"username":`,
			wantStatus: http.StatusBadRequest,
			wantCode:   deal.CodeInvalidArgument,
		},
		{
			name:       "missing session",
			method:     http.MethodGet,
			path:       "/api/v1/auth/me",
			wantStatus: http.StatusUnauthorized,
			wantCode:   deal.CodeUnauthenticated,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, server.URL+test.path, strings.NewReader(test.body))
			if err != nil {
				t.Fatalf("NewRequest(): %v", err)
			}
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("HTTP request: %v", err)
			}
			defer response.Body.Close()

			var envelope struct {
				Code    int             `json:"code"`
				Message string          `json:"message"`
				Data    json.RawMessage `json:"data"`
			}
			if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.StatusCode, test.wantStatus, readBody(t, response.Body))
			}
			if envelope.Code != test.wantCode {
				t.Fatalf("code = %d, want %d", envelope.Code, test.wantCode)
			}
			spec, _ := deal.LookupError(test.wantCode)
			if envelope.Message != spec.Message || string(envelope.Data) != "null" {
				t.Fatalf("envelope = %+v, want message=%q data=null", envelope, spec.Message)
			}
		})
	}
}

func TestRegisterForwardsEachOwnershipPathAndReturnsEnvelope(t *testing.T) {
	stub := &stubIamRPC{
		registerResponse: &iamv1.RegisterResponse{
			Token: "sess_registered",
			User:  &iamv1.User{Id: "01J00000000000000000000001", Username: "alice"},
		},
	}
	server := httptest.NewServer(authTestMux(stub))
	defer server.Close()

	tests := []struct {
		name           string
		body           string
		wantEnterprise string
		wantLinkToken  string
	}{
		{
			name: "pending invitation or direct account path",
			body: `{"username":"alice","password":"Pass1234"}`,
		},
		{
			name:          "link token path",
			body:          `{"username":"alice","password":"Pass1234","link_token":"link_alice"}`,
			wantLinkToken: "link_alice",
		},
		{
			name:           "enterprise owner path",
			body:           `{"username":"alice","password":"Pass1234","enterprise_name":"启航科技"}`,
			wantEnterprise: "启航科技",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, err := http.Post(
				server.URL+"/api/v1/auth/register",
				"application/json",
				strings.NewReader(test.body),
			)
			if err != nil {
				t.Fatalf("POST register: %v", err)
			}
			assertSuccessEnvelope(t, response)
			if stub.lastRegister.GetEnterpriseName() != test.wantEnterprise ||
				stub.lastRegister.GetLinkToken() != test.wantLinkToken {
				t.Fatalf("Register request = %v", stub.lastRegister)
			}
		})
	}
}

func TestAuthenticatedRoutesForwardPrincipalAndSupportCookieOrBearer(t *testing.T) {
	principal := &commonv1.Principal{
		UserId:       "01J00000000000000000000001",
		EnterpriseId: "",
		Internal:     false,
		LinkContext: &commonv1.LinkContext{
			BaseId:    "01J00000000000000000000002",
			LinkToken: "link_alice",
			Role:      "editor",
		},
	}
	stub := &stubIamRPC{
		exchangeResponse: &iamv1.ExchangeSessionResponse{Principal: principal},
		getMeResponse: &iamv1.GetMeResponse{
			User: &iamv1.User{Id: principal.UserId, Username: "alice"},
		},
		updateProfileResponse: &iamv1.UpdateProfileResponse{
			User: &iamv1.User{Id: principal.UserId, Username: "alice", DisplayName: "Alice"},
		},
	}
	server := httptest.NewServer(authTestMux(stub))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/auth/me", nil)
	if err != nil {
		t.Fatalf("NewRequest(GET me): %v", err)
	}
	request.AddCookie(&http.Cookie{Name: "sid", Value: "cookie-session"})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET me: %v", err)
	}
	assertSuccessEnvelope(t, response)
	assertForwardedPrincipal(t, stub.lastPrincipal, principal)

	request, err = http.NewRequest(
		http.MethodPut,
		server.URL+"/api/v1/me/profile",
		strings.NewReader(`{"display_name":"Alice","avatar_uri":"/avatar.png"}`),
	)
	if err != nil {
		t.Fatalf("NewRequest(profile): %v", err)
	}
	request.Header.Set("Authorization", "Bearer bearer-session")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("PUT profile: %v", err)
	}
	assertSuccessEnvelope(t, response)
	if stub.lastUpdateProfile.GetDisplayName() != "Alice" ||
		stub.lastUpdateProfile.GetAvatarUri() != "/avatar.png" {
		t.Fatalf("UpdateProfile request = %v", stub.lastUpdateProfile)
	}
	assertForwardedPrincipal(t, stub.lastPrincipal, principal)

	request, err = http.NewRequest(
		http.MethodPost,
		server.URL+"/api/v1/me/password",
		strings.NewReader(`{"old_password":"Pass1234","new_password":"NewPass5678"}`),
	)
	if err != nil {
		t.Fatalf("NewRequest(password): %v", err)
	}
	request.AddCookie(&http.Cookie{Name: "sid", Value: "password-session"})
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST password: %v", err)
	}
	assertSuccessEnvelope(t, response)
	if stub.lastChangePassword.GetOldPassword() != "Pass1234" ||
		stub.lastChangePassword.GetNewPassword() != "NewPass5678" {
		t.Fatalf("ChangePassword request = %v", stub.lastChangePassword)
	}
	assertForwardedPrincipal(t, stub.lastPrincipal, principal)
}

func TestLogoutUsesSessionCarrierAndClearsSIDCookie(t *testing.T) {
	stub := &stubIamRPC{}
	server := httptest.NewServer(authTestMux(stub))
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/logout", nil)
	if err != nil {
		t.Fatalf("NewRequest(): %v", err)
	}
	request.AddCookie(&http.Cookie{Name: "sid", Value: "logout-token"})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	assertSuccessEnvelope(t, response)
	if stub.lastLogoutToken != "logout-token" {
		t.Fatalf("Logout token = %q, want logout-token", stub.lastLogoutToken)
	}
	cookies := response.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "sid" || cookies[0].MaxAge >= 0 {
		t.Fatalf("logout cookies = %+v, want expired sid", cookies)
	}
}

func authTestMux(stub rpc.IamRPC) *http.ServeMux {
	mux := http.NewServeMux()
	RegisterAuthRoutes(mux, stub)
	return mux
}

func assertSuccessEnvelope(t *testing.T, response *http.Response) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.StatusCode, readBody(t, response.Body))
	}
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 0 || envelope.Message != "ok" {
		t.Fatalf("envelope = %+v, want success", envelope)
	}
}

func assertForwardedPrincipal(t *testing.T, encoded string, want *commonv1.Principal) {
	t.Helper()
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode x-principal: %v", err)
	}
	var got commonv1.Principal
	if err := proto.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal x-principal: %v", err)
	}
	if !proto.Equal(&got, want) {
		t.Fatalf("x-principal = %v, want %v", &got, want)
	}
}

func businessRPCError(code int) error {
	spec, _ := deal.LookupError(code)
	withDetails, err := status.New(spec.GRPCCode, spec.Message).WithDetails(&errdetails.ErrorInfo{
		Reason: strconv.Itoa(code),
	})
	if err != nil {
		return status.Error(spec.GRPCCode, spec.Message)
	}
	return withDetails.Err()
}

func readBody(t *testing.T, body io.Reader) string {
	t.Helper()
	payload, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(payload)
}
