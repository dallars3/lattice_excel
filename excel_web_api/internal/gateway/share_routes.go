// gateway/share_routes.go：H-63 ~ H-72 协作与链接分享路由。
package gateway

import (
	"net/http"
	"strings"

	sharev1 "excel/api/share/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type shareRoutes struct {
	pipeline *Pipeline
	client   rpc.ShareManageRPC
}

// RegisterShareRoutes binds collaborator and link-sharing endpoints.
func RegisterShareRoutes(pipeline *Pipeline, client rpc.ShareManageRPC) {
	if pipeline == nil {
		panic("share routes pipeline is required")
	}
	routes := &shareRoutes{pipeline: pipeline, client: client}
	pipeline.BaseAuth(
		http.MethodGet,
		"/api/v1/bases/{base_id}/shares/members",
		sharev1.Action_ACTION_VIEW.String(),
		http.HandlerFunc(routes.listCollaborators),
	)
	pipeline.BaseAuth(
		http.MethodPost,
		"/api/v1/bases/{base_id}/shares/members",
		sharev1.Action_ACTION_MANAGE_SHARE.String(),
		http.HandlerFunc(routes.addCollaborator),
	)
	pipeline.SessionRequired(
		http.MethodPut,
		"/api/v1/shares/members/{user_id}/role",
		http.HandlerFunc(routes.updateCollaboratorRole),
	)
	pipeline.SessionRequired(
		http.MethodDelete,
		"/api/v1/shares/members/{user_id}",
		http.HandlerFunc(routes.removeCollaborator),
	)
	pipeline.BaseAuth(
		http.MethodGet,
		"/api/v1/bases/{base_id}/link",
		sharev1.Action_ACTION_VIEW.String(),
		http.HandlerFunc(routes.getLink),
	)
	pipeline.BaseAuth(
		http.MethodPut,
		"/api/v1/bases/{base_id}/link",
		sharev1.Action_ACTION_MANAGE_SHARE.String(),
		http.HandlerFunc(routes.updateLink),
	)
	pipeline.BaseAuth(
		http.MethodPost,
		"/api/v1/bases/{base_id}/link/reset",
		sharev1.Action_ACTION_MANAGE_SHARE.String(),
		http.HandlerFunc(routes.resetLink),
	)
	pipeline.BaseAuth(
		http.MethodPost,
		"/api/v1/bases/{base_id}/link/close",
		sharev1.Action_ACTION_MANAGE_SHARE.String(),
		http.HandlerFunc(routes.closeLink),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/enterprise/open-links",
		http.HandlerFunc(routes.listOpenLinksByEnterprise),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/enterprise/open-links/{link_id}/close",
		http.HandlerFunc(routes.forceCloseByManager),
	)
}

func (routes *shareRoutes) listCollaborators(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	result, err := routes.client.ListCollaborators(request.Context(), &sharev1.ListCollaboratorsRequest{
		BaseId: request.PathValue("base_id"),
		Q:      queryString(request, "q", ""),
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *shareRoutes) addCollaborator(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	var body struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.Username) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	role, ok := parseShareRole(body.Role)
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.AddCollaborator(request.Context(), &sharev1.AddCollaboratorRequest{
		BaseId:   request.PathValue("base_id"),
		Username: strings.TrimSpace(body.Username),
		Role:     role,
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *shareRoutes) updateCollaboratorRole(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	var body struct {
		BaseID string `json:"base_id"`
		Role   string `json:"role"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.BaseID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	role, ok := parseShareRole(body.Role)
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	baseID := strings.TrimSpace(body.BaseID)
	if !routes.authorizeShareManage(response, request, baseID) {
		return
	}
	result, err := routes.client.UpdateCollaboratorRole(request.Context(), &sharev1.UpdateCollaboratorRoleRequest{
		BaseId: baseID,
		UserId: request.PathValue("user_id"),
		Role:   role,
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *shareRoutes) removeCollaborator(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	var body struct {
		BaseID string `json:"base_id"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.BaseID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	baseID := strings.TrimSpace(body.BaseID)
	if !routes.authorizeShareManage(response, request, baseID) {
		return
	}
	if _, err := routes.client.RemoveCollaborator(request.Context(), &sharev1.RemoveCollaboratorRequest{
		BaseId: baseID,
		UserId: request.PathValue("user_id"),
	}); err != nil {
		writeBusinessError(response, err)
		return
	}
	writeSuccess(response, map[string]any{})
}

func (routes *shareRoutes) getLink(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	result, err := routes.client.GetLink(request.Context(), &sharev1.GetLinkRequest{
		BaseId: request.PathValue("base_id"),
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *shareRoutes) updateLink(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	var body struct {
		Scope string `json:"scope"`
		Role  string `json:"role"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	scope, ok := parseLinkScope(body.Scope)
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	role, ok := parseShareRole(body.Role)
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.UpdateLink(request.Context(), &sharev1.UpdateLinkRequest{
		BaseId: request.PathValue("base_id"),
		Scope:  scope,
		Role:   role,
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *shareRoutes) resetLink(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	result, err := routes.client.ResetLink(request.Context(), &sharev1.ResetLinkRequest{
		BaseId: request.PathValue("base_id"),
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *shareRoutes) closeLink(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	if _, err := routes.client.CloseLink(request.Context(), &sharev1.CloseLinkRequest{
		BaseId: request.PathValue("base_id"),
	}); err != nil {
		writeBusinessError(response, err)
		return
	}
	writeSuccess(response, map[string]any{})
}

func (routes *shareRoutes) listOpenLinksByEnterprise(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	result, err := routes.client.ListOpenLinksByEnterprise(
		request.Context(),
		&sharev1.ListOpenLinksByEnterpriseRequest{
			Cursor: queryString(request, "cursor", ""),
			Limit:  queryLimit(request, 0, 100),
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *shareRoutes) forceCloseByManager(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	linkID := strings.TrimSpace(request.PathValue("link_id"))
	if linkID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	if _, err := routes.client.ForceCloseByManager(request.Context(), &sharev1.ForceCloseByManagerRequest{
		LinkId: linkID,
	}); err != nil {
		writeBusinessError(response, err)
		return
	}
	writeSuccess(response, map[string]any{})
}

// authorizeShareManage performs the handler-level Base authorization for routes
// without a {base_id} path variable (H-65/H-66).
func (routes *shareRoutes) authorizeShareManage(
	response http.ResponseWriter,
	request *http.Request,
	baseID string,
) bool {
	principal, ok := PrincipalFromContext(request.Context())
	if !ok {
		writeCodeError(response, deal.CodeUnauthenticated)
		return false
	}
	decision, err := routes.pipeline.checkAccess(
		request.Context(),
		principal,
		baseID,
		sharev1.Action_ACTION_MANAGE_SHARE,
	)
	if err != nil {
		writeBusinessError(response, err)
		return false
	}
	if decision == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return false
	}
	if !decision.GetAllow() {
		writeCodeError(response, deal.CodePermissionDenied)
		return false
	}
	return true
}

// parseShareRole maps a case-insensitive role token (viewer|filler|editor|manager
// or the ROLE_ prefixed form) onto sharev1.Role.
func parseShareRole(value string) (sharev1.Role, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" {
		return sharev1.Role_ROLE_UNSPECIFIED, false
	}
	if !strings.HasPrefix(normalized, "ROLE_") {
		normalized = "ROLE_" + normalized
	}
	role, ok := sharev1.Role_value[normalized]
	if !ok || sharev1.Role(role) == sharev1.Role_ROLE_UNSPECIFIED {
		return sharev1.Role_ROLE_UNSPECIFIED, false
	}
	return sharev1.Role(role), true
}

// parseLinkScope maps a case-insensitive link scope token (none|enterprise|anyone
// or the LINK_SCOPE_ prefixed form) onto sharev1.LinkScope.
func parseLinkScope(value string) (sharev1.LinkScope, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" {
		return sharev1.LinkScope_LINK_SCOPE_UNSPECIFIED, false
	}
	if !strings.HasPrefix(normalized, "LINK_SCOPE_") {
		normalized = "LINK_SCOPE_" + normalized
	}
	scope, ok := sharev1.LinkScope_value[normalized]
	if !ok || sharev1.LinkScope(scope) == sharev1.LinkScope_LINK_SCOPE_UNSPECIFIED {
		return sharev1.LinkScope_LINK_SCOPE_UNSPECIFIED, false
	}
	return sharev1.LinkScope(scope), true
}
