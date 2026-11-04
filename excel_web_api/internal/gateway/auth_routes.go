// gateway/auth_routes.go：认证路由装配与统一 HTTP 信封。
package gateway

import (
	"context"
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

	"excel_web_api/deal"
)

const (
	sessionCookieName = "sid"
	maxRequestBytes   = 1 << 20
)

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
