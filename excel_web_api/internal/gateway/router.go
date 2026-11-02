// gateway/router.go：表驱动直通路由模板。
package gateway

import (
	"net/http"
)

type routeAccess int

const (
	routePublic routeAccess = iota
	routeSession
	routeSessionCarrier
	routeBaseAuth
)

type routeSpec struct {
	method  string
	path    string
	access  routeAccess
	action  string
	handler http.Handler
}

func (pipeline *Pipeline) registerRoute(spec routeSpec) {
	registerRoute(pipeline.mux, pipeline, spec)
}

func registerRoute(mux *http.ServeMux, pipeline *Pipeline, spec routeSpec) {
	if spec.handler == nil {
		panic("gateway route handler is required")
	}
	mux.Handle(spec.method+" "+spec.path, pipeline.wrap(spec))
}
