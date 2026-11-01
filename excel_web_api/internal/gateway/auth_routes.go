// gateway/auth_routes.go：认证路由装配与统一 HTTP 信封。
package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

const (
	sessionCookieName = "sid"
	maxRequestBytes   = 1 << 20
)

type authRoutes struct {
	client rpc.IamRPC
}

// RegisterAuthRoutes installs H-01 through H-06 without depending on cmd wiring.
func RegisterAuthRoutes(mux *http.ServeMux, client rpc.IamRPC) {
	routes := &authRoutes{client: client}
	mux.HandleFunc("POST /api/v1/auth/register", routes.register)
	mux.HandleFunc("POST /api/v1/auth/login", routes.login)
	mux.HandleFunc("POST /api/v1/auth/logout", routes.logout)
	mux.HandleFunc("GET /api/v1/auth/me", routes.getMe)
	mux.HandleFunc("PUT /api/v1/me/profile", routes.updateProfile)
	mux.HandleFunc("POST /api/v1/me/password", routes.changePassword)
}

type responseEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func decodeJSONBody(w http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(w, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeSuccess(w http.ResponseWriter, data any) {
	writeEnvelope(w, http.StatusOK, responseEnvelope{Code: 0, Message: "ok", Data: data})
}

func writeBusinessError(w http.ResponseWriter, err error) {
	code := grpcBusinessCode(err)
	spec, ok := deal.LookupError(code)
	if !ok {
		code = deal.CodeServiceUnavailable
		spec, _ = deal.LookupError(code)
	}
	writeEnvelope(w, spec.HTTPStatus, responseEnvelope{
		Code:    code,
		Message: spec.Message,
		Data:    nil,
	})
}

func writeCodeError(w http.ResponseWriter, code int) {
	spec, ok := deal.LookupError(code)
	if !ok {
		code = deal.CodeServiceUnavailable
		spec, _ = deal.LookupError(code)
	}
	writeEnvelope(w, spec.HTTPStatus, responseEnvelope{Code: code, Message: spec.Message, Data: nil})
}

func writeEnvelope(w http.ResponseWriter, httpStatus int, envelope responseEnvelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(envelope)
}

func protoJSON(message proto.Message) (json.RawMessage, error) {
	return protojson.MarshalOptions{UseProtoNames: true}.Marshal(message)
}

func grpcBusinessCode(err error) int {
	grpcStatus := status.Convert(err)
	for _, detail := range grpcStatus.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok {
			continue
		}
		if code, parseErr := strconv.Atoi(info.GetReason()); parseErr == nil {
			return code
		}
	}
	switch grpcStatus.Code() {
	case codes.InvalidArgument:
		return deal.CodeInvalidArgument
	case codes.AlreadyExists:
		return deal.CodeConflict
	case codes.Unauthenticated:
		return deal.CodeUnauthenticated
	case codes.PermissionDenied:
		return deal.CodePermissionDenied
	case codes.Aborted:
		return deal.CodeConcurrentConflict
	case codes.NotFound:
		return deal.CodeDeletedResource
	case codes.ResourceExhausted:
		return deal.CodeRateLimited
	default:
		return deal.CodeServiceUnavailable
	}
}

func outgoingClientContext(request *http.Request) context.Context {
	ip := request.RemoteAddr
	if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
		ip = host
	}
	return metadata.AppendToOutgoingContext(request.Context(), "x-client-ip", strings.TrimSpace(ip))
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int((30 * 24 * time.Hour) / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func sessionToken(request *http.Request) string {
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	if fields := strings.Fields(authorization); len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		return fields[1]
	}
	if cookie, err := request.Cookie(sessionCookieName); err == nil {
		return cookie.Value
	}
	return ""
}

func (routes *authRoutes) authenticatedContext(request *http.Request) (context.Context, error) {
	token := sessionToken(request)
	if token == "" {
		return nil, status.Error(codes.Unauthenticated, "未登录")
	}
	response, err := routes.client.ExchangeSession(
		request.Context(),
		&iamv1.ExchangeSessionRequest{Token: token},
	)
	if err != nil {
		return nil, err
	}
	if response == nil || response.GetPrincipal() == nil || response.GetPrincipal().GetUserId() == "" {
		return nil, status.Error(codes.Unauthenticated, "未登录")
	}
	payload, err := proto.Marshal(&commonv1.Principal{
		UserId:       response.GetPrincipal().GetUserId(),
		EnterpriseId: response.GetPrincipal().GetEnterpriseId(),
		Internal:     response.GetPrincipal().GetInternal(),
		LinkContext:  response.GetPrincipal().GetLinkContext(),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, "principal marshal failed")
	}
	return metadata.AppendToOutgoingContext(
		request.Context(),
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
	), nil
}
