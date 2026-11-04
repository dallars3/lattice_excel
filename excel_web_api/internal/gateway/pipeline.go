// gateway/pipeline.go：request-id、会话恢复与 Base 鉴权统一管线。
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_web_api/deal"
	authredis "excel_web_api/redis"
	"excel_web_api/rpc"
)

const requestIDHeader = "X-Request-Id"

const (
	registerRateLimit = 10
	loginRateLimit    = 20
	writeRateLimit    = 120
	// trackingRateLimit 埋点批上报独立桶（按来源 IP）：单请求最多 100 条，
	// 60 req/min/IP 覆盖正常埋点频次并防放大。
	trackingRateLimit = 60
	rateLimitWindow   = time.Minute
)

type pipelineContextKey int

const (
	principalContextKey pipelineContextKey = iota
	requestIDContextKey
	authorizationContextKey
	baseIDContextKey
)

// AuthorizationContext is the Base permission decision attached by BaseAuth.
type AuthorizationContext struct {
	BaseID    string
	Action    sharev1.Action
	Principal *commonv1.Principal
	Decision  *sharev1.CheckAccessResponse
}

type requestRateLimiter interface {
	Allow(
		context.Context,
		string,
		string,
		int64,
		time.Duration,
	) (authredis.RateLimitResult, error)
}

// Pipeline owns the table-driven HTTP routes and shared authorization stages.
type Pipeline struct {
	mux          *http.ServeMux
	iam          rpc.IamRPC
	share        rpc.ShareRPC
	cache        *authredis.Cache
	logger       *slog.Logger
	rateLimiter  requestRateLimiter
	rateFailOpen atomic.Uint64
	routesMu     sync.RWMutex
	routeList    []string
}

// NewPipeline creates a gateway pipeline. A nil logger uses slog.Default().
func NewPipeline(
	iam rpc.IamRPC,
	share rpc.ShareRPC,
	cache *authredis.Cache,
	logger *slog.Logger,
) *Pipeline {
	if logger == nil {
		logger = slog.Default()
	}
	var limiter requestRateLimiter
	if cache != nil {
		if cacheLimiter := cache.RateLimiter(); cacheLimiter != nil {
			limiter = cacheLimiter
		}
	}
	return &Pipeline{
		mux:         http.NewServeMux(),
		iam:         iam,
		share:       share,
		cache:       cache,
		logger:      logger,
		rateLimiter: limiter,
	}
}

// Public registers a route without session restoration or Base authorization.
func (pipeline *Pipeline) Public(method, path string, handler http.Handler) {
	pipeline.registerRoute(routeSpec{
		method:  method,
		path:    path,
		access:  routePublic,
		handler: handler,
	})
}

// SessionRequired registers an authenticated route without a Base action.
func (pipeline *Pipeline) SessionRequired(method, path string, handler http.Handler) {
	pipeline.registerRoute(routeSpec{
		method:  method,
		path:    path,
		access:  routeSession,
		handler: handler,
	})
}

// BaseAuth registers an authenticated route guarded by share.CheckAccess.
func (pipeline *Pipeline) BaseAuth(method, path, action string, handler http.Handler) {
	parseAction(action)
	pipeline.registerRoute(routeSpec{
		method:  method,
		path:    path,
		access:  routeBaseAuth,
		action:  action,
		handler: handler,
	})
}

// Handler returns the pipeline's HTTP entry point.
func (pipeline *Pipeline) Handler() http.Handler {
	return pipeline.decorate(pipeline.mux)
}

// Routes returns a sorted snapshot of registered HTTP method and path patterns.
func (pipeline *Pipeline) Routes() []string {
	pipeline.routesMu.RLock()
	defer pipeline.routesMu.RUnlock()
	routes := append([]string(nil), pipeline.routeList...)
	sort.Strings(routes)
	return routes
}

// ServeHTTP applies request correlation before route-specific authorization.
func (pipeline *Pipeline) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	pipeline.Handler().ServeHTTP(response, request)
}

func (pipeline *Pipeline) wrap(spec routeSpec) http.Handler {
	pipeline.routesMu.Lock()
	pipeline.routeList = append(pipeline.routeList, spec.method+" "+spec.path)
	pipeline.routesMu.Unlock()

	routeHandler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !pipeline.allowRequestRate(response, request, spec) {
			return
		}
		if spec.access == routePublic {
			spec.handler.ServeHTTP(response, request)
			return
		}
		token := sessionToken(request)
		if token == "" {
			writeCodeError(response, deal.CodeUnauthenticated)
			return
		}
		ctx := context.WithValue(request.Context(), requestIDContextKey, requestID(request.Context()))
		request = request.WithContext(ctx)
		if spec.access == routeSession {
			principalContext, _, err := pipeline.resolvePrincipal(request, token)
			if err != nil {
				writeBusinessError(response, err)
				return
			}
			request = request.WithContext(principalContext)
		}
		if spec.access == routeBaseAuth {
			principalContext, principal, err := pipeline.resolvePrincipal(request, token)
			if err != nil {
				writeBusinessError(response, err)
				return
			}
			baseID := request.PathValue("base_id")
			if baseID == "" {
				baseID, _ = request.Context().Value(baseIDContextKey).(string)
			}
			if baseID == "" {
				writeCodeError(response, deal.CodeInvalidArgument)
				return
			}
			if pipeline.share == nil {
				writeCodeError(response, deal.CodeServiceUnavailable)
				return
			}
			action := parseAction(spec.action)
			decision, err := pipeline.checkAccess(principalContext, principal, baseID, action)
			if err != nil {
				writeBusinessError(response, err)
				return
			}
			if decision == nil {
				writeCodeError(response, deal.CodeServiceUnavailable)
				return
			}
			if action == sharev1.Action_ACTION_EXPORT && !decision.GetInternal() {
				writeCodeError(response, deal.CodeExternalExportDenied)
				return
			}
			if !decision.GetAllow() {
				writeCodeError(response, deal.CodePermissionDenied)
				return
			}
			principalContext = context.WithValue(
				principalContext,
				authorizationContextKey,
				AuthorizationContext{
					BaseID:    baseID,
					Action:    action,
					Principal: principal,
					Decision:  decision,
				},
			)
			request = request.WithContext(principalContext)
		}
		spec.handler.ServeHTTP(response, request)
	})
	return pipeline.decorate(routeHandler)
}

func (pipeline *Pipeline) allowRequestRate(
	response http.ResponseWriter,
	request *http.Request,
	spec routeSpec,
) bool {
	scope, limit, ok := privacyRateRule(spec)
	if !ok || pipeline.rateLimiter == nil {
		return true
	}
	result, err := pipeline.rateLimiter.Allow(
		request.Context(),
		scope,
		clientIP(request),
		limit,
		rateLimitWindow,
	)
	if err != nil {
		// fail-open 与鉴权缓存同策略（ticket 05）：限速器故障不放大为全面不可用，
		// 但必须显式留痕——ERROR 日志携带累计放行计数，供运维检索。
		total := pipeline.rateFailOpen.Add(1)
		pipeline.logger.Error("rate limiter unavailable; request continues (fail-open)",
			"error", err, "fail_open_total", total)
		return true
	}
	if result.Allowed {
		return true
	}
	if result.RetryAfter > 0 {
		seconds := int64((result.RetryAfter + time.Second - 1) / time.Second)
		response.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	}
	writeCodeError(response, deal.CodeRateLimited)
	return false
}

func privacyRateRule(spec routeSpec) (string, int64, bool) {
	route := spec.method + " " + spec.path
	switch route {
	case http.MethodPost + " /api/v1/auth/register":
		return "register", registerRateLimit, true
	case http.MethodPost + " /api/v1/auth/login":
		return "login", loginRateLimit, true
	case http.MethodPost + " /api/v1/bases/{base_id}/records",
		http.MethodPut + " /api/v1/records/{record_id}/cells/{field_id}",
		http.MethodPost + " /api/v1/records/batch",
		http.MethodDelete + " /api/v1/records",
		http.MethodPost + " /api/v1/records/{record_id}/undo",
		http.MethodPost + " /api/v1/records/{record_id}/history/{node_id}/rollback":
		return "record_write", writeRateLimit, true
	case http.MethodPost + " /api/v1/records/{record_id}/comments",
		http.MethodPost + " /api/v1/comments/{id}/replies":
		return "comment_write", writeRateLimit, true
	case http.MethodPost + " /api/v1/tracking/batch":
		return "tracking", trackingRateLimit, true
	default:
		return "", 0, false
	}
}

func clientIP(request *http.Request) string {
	remote := strings.TrimSpace(request.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return host
	}
	return remote
}

func (pipeline *Pipeline) decorate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestIDValue := request.Header.Get(requestIDHeader)
		if requestIDValue == "" {
			requestIDValue = newRequestID()
			request.Header.Set(requestIDHeader, requestIDValue)
		}
		response.Header().Set(requestIDHeader, requestIDValue)
		response.Header().Set("Cache-Control", "no-store")
		ctx := request.Context()
		if requestID(ctx) == "" {
			ctx = context.WithValue(ctx, requestIDContextKey, requestIDValue)
			ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", requestIDValue)
		}
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

// PrincipalFromContext returns the principal restored by the pipeline.
func PrincipalFromContext(ctx context.Context) (*commonv1.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey).(*commonv1.Principal)
	return principal, ok && principal != nil && principal.GetUserId() != ""
}

// AuthorizationFromContext returns the Base decision when a BaseAuth route ran.
func AuthorizationFromContext(ctx context.Context) (AuthorizationContext, bool) {
	authorization, ok := ctx.Value(authorizationContextKey).(AuthorizationContext)
	return authorization, ok
}

// WithBaseID injects a Base identifier for wrapping handlers whose route
// pattern does not expose the {base_id} path variable.
func WithBaseID(ctx context.Context, baseID string) context.Context {
	return context.WithValue(ctx, baseIDContextKey, baseID)
}

func (pipeline *Pipeline) resolvePrincipal(
	request *http.Request,
	token string,
) (context.Context, *commonv1.Principal, error) {
	if pipeline.iam == nil {
		return nil, nil, status.Error(codes.Unavailable, "iam client is not configured")
	}
	load := func(ctx context.Context) (*commonv1.Principal, error) {
		response, err := pipeline.iam.ExchangeSession(
			ctx,
			&iamv1.ExchangeSessionRequest{Token: token},
		)
		if err != nil {
			return nil, err
		}
		if response == nil || response.GetPrincipal() == nil ||
			response.GetPrincipal().GetUserId() == "" {
			return nil, status.Error(codes.Unauthenticated, "未登录")
		}
		return response.GetPrincipal(), nil
	}
	var principal *commonv1.Principal
	var err error
	if pipeline.cache != nil {
		principal, err = pipeline.cache.Session(request.Context(), token, load)
	} else {
		principal, err = load(request.Context())
	}
	if err != nil {
		return nil, nil, err
	}
	payload, err := proto.Marshal(principal)
	if err != nil {
		return nil, nil, status.Error(codes.Internal, "principal marshal failed")
	}
	ctx := context.WithValue(request.Context(), principalContextKey, principal)
	ctx = metadata.AppendToOutgoingContext(
		ctx,
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
	)
	return ctx, principal, nil
}

func (pipeline *Pipeline) checkAccess(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
	action sharev1.Action,
) (*sharev1.CheckAccessResponse, error) {
	load := func(loadCtx context.Context) (*sharev1.CheckAccessResponse, error) {
		return pipeline.share.CheckAccess(
			loadCtx,
			&sharev1.CheckAccessRequest{
				Principal:   principal,
				BaseId:      baseID,
				Action:      action,
				LinkContext: principal.GetLinkContext(),
			},
		)
	}
	if pipeline.cache == nil {
		return load(ctx)
	}
	return pipeline.cache.Access(ctx, principal.GetUserId(), baseID, action, load)
}

func parseAction(value string) sharev1.Action {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if !strings.HasPrefix(normalized, "ACTION_") {
		normalized = "ACTION_" + normalized
	}
	action, ok := sharev1.Action_value[normalized]
	if !ok || sharev1.Action(action) == sharev1.Action_ACTION_UNSPECIFIED {
		panic("unknown share action: " + value)
	}
	return sharev1.Action(action)
}

func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey).(string)
	return value
}

func newRequestID() string {
	var payload [16]byte
	if _, err := rand.Read(payload[:]); err != nil {
		return "req_unknown"
	}
	return "req_" + hex.EncodeToString(payload[:])
}
