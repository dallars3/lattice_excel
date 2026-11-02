// gateway/auth.go：认证路由表与管线会话恢复接缝。
package gateway

import (
	"context"
	"log/slog"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"excel_web_api/rpc"
)

type authRoutes struct {
	client   rpc.IamRPC
	pipeline *Pipeline
}

type authRouteSpec struct {
	access  routeAccess
	method  string
	path    string
	handler http.HandlerFunc
}

// RegisterAuthRoutes keeps the ticket-04 entry point while allowing callers to
// pass a fully wired Pipeline (cache/share/logger) as the optional dependency.
func RegisterAuthRoutes(
	mux *http.ServeMux,
	client rpc.IamRPC,
	pipelines ...*Pipeline,
) {
	pipeline := firstPipeline(pipelines)
	if pipeline == nil {
		pipeline = NewPipeline(client, nil, nil, slog.Default())
	}
	pipeline.iam = client
	for _, spec := range authRouteSpecs(pipeline, client) {
		registerRoute(mux, pipeline, spec)
	}
}

// RegisterAuthRoutes adds the ticket-04 authentication routes to this
// pipeline's Handler, allowing later tickets to register all routes uniformly.
func (pipeline *Pipeline) RegisterAuthRoutes(client rpc.IamRPC) {
	pipeline.iam = client
	for _, spec := range authRouteSpecs(pipeline, client) {
		pipeline.registerRoute(spec)
	}
}

func authRouteSpecs(pipeline *Pipeline, client rpc.IamRPC) []routeSpec {
	routes := &authRoutes{client: client, pipeline: pipeline}
	routeTable := []authRouteSpec{
		{access: routePublic, method: http.MethodPost, path: "/api/v1/auth/register", handler: routes.register},
		{access: routePublic, method: http.MethodPost, path: "/api/v1/auth/login", handler: routes.login},
		{access: routeSessionCarrier, method: http.MethodPost, path: "/api/v1/auth/logout", handler: routes.logout},
		{access: routeSession, method: http.MethodGet, path: "/api/v1/auth/me", handler: routes.getMe},
		{access: routeSession, method: http.MethodPut, path: "/api/v1/me/profile", handler: routes.updateProfile},
		{access: routeSession, method: http.MethodPost, path: "/api/v1/me/password", handler: routes.changePassword},
	}
	specs := make([]routeSpec, 0, len(routeTable))
	for _, entry := range routeTable {
		specs = append(specs, routeSpec{
			method:  entry.method,
			path:    entry.path,
			access:  entry.access,
			handler: entry.handler,
		})
	}
	return specs
}

func firstPipeline(pipelines []*Pipeline) *Pipeline {
	if len(pipelines) == 0 {
		return nil
	}
	return pipelines[0]
}

func (routes *authRoutes) authenticatedContext(
	request *http.Request,
) (context.Context, error) {
	if _, ok := PrincipalFromContext(request.Context()); ok {
		return request.Context(), nil
	}
	token := sessionToken(request)
	if token == "" {
		return nil, status.Error(codes.Unauthenticated, "未登录")
	}
	if routes.pipeline == nil {
		return nil, status.Error(codes.Unavailable, "gateway pipeline is not configured")
	}
	ctx, _, err := routes.pipeline.resolvePrincipal(request, token)
	return ctx, err
}
