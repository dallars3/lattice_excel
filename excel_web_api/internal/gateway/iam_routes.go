// gateway/iam_routes.go：H-07 ~ H-20、H-89 ~ H-91 用户/企业级 IAM 路由。
package gateway

import (
	"net/http"
	"strings"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type iamRoutes struct {
	pipeline *Pipeline
	client   rpc.IamEnterpriseRPC
}

// RegisterIamRoutes binds user and enterprise IAM endpoints behind SessionRequired.
func RegisterIamRoutes(pipeline *Pipeline, client rpc.IamEnterpriseRPC) {
	if pipeline == nil {
		panic("iam routes pipeline is required")
	}
	routes := &iamRoutes{pipeline: pipeline, client: client}
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/me/deactivate",
		http.HandlerFunc(routes.requestDeactivate),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/me/deactivate/cancel",
		http.HandlerFunc(routes.cancelDeactivate),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/enterprise",
		http.HandlerFunc(routes.getEnterprise),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/enterprise/invites",
		http.HandlerFunc(routes.inviteMember),
	)
	pipeline.SessionRequired(
		http.MethodDelete,
		"/api/v1/enterprise/invites/{invite_id}",
		http.HandlerFunc(routes.cancelInvite),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/enterprise/invites",
		http.HandlerFunc(routes.listInvites),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/invitations",
		http.HandlerFunc(routes.listMyInvitations),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/invitations/{invite_id}/accept",
		http.HandlerFunc(routes.acceptInvite),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/invitations/{invite_id}/reject",
		http.HandlerFunc(routes.rejectInvite),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/enterprise/members",
		http.HandlerFunc(routes.listMembers),
	)
	pipeline.SessionRequired(
		http.MethodPut,
		"/api/v1/enterprise/members/{user_id}/role",
		http.HandlerFunc(routes.updateMemberRole),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/enterprise/members/{user_id}/disable",
		http.HandlerFunc(routes.disableMember),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/enterprise/members/{user_id}/enable",
		http.HandlerFunc(routes.enableMember),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/enterprise/owner/transfer",
		http.HandlerFunc(routes.transferOwner),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/enterprise/dissolution/cancel",
		http.HandlerFunc(routes.cancelEnterpriseDissolution),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/enterprise/dissolution",
		http.HandlerFunc(routes.getEnterpriseDissolutionStatus),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/users/search",
		http.HandlerFunc(routes.searchUsers),
	)
}

func (routes *iamRoutes) requestDeactivate(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	_ = decodeOptionalJSONBody(response, request, &struct{}{})
	result, err := routes.client.RequestDeactivate(
		workerRequestContext(request, ""),
		&emptypb.Empty{},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) cancelDeactivate(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	_ = decodeOptionalJSONBody(response, request, &struct{}{})
	result, err := routes.client.CancelDeactivate(
		workerRequestContext(request, ""),
		&emptypb.Empty{},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) getEnterprise(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	result, err := routes.client.GetEnterprise(
		workerRequestContext(request, ""),
		&emptypb.Empty{},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) inviteMember(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	var body struct {
		UsernameList []string `json:"username_list"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.InviteMember(
		workerRequestContext(request, ""),
		&iamv1.InviteMemberRequest{UsernameList: body.UsernameList},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) cancelInvite(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	inviteID := strings.TrimSpace(request.PathValue("invite_id"))
	if inviteID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	_ = decodeOptionalJSONBody(response, request, &struct{}{})
	result, err := routes.client.CancelInvite(
		workerRequestContext(request, ""),
		&iamv1.CancelInviteRequest{InviteId: inviteID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) listInvites(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	result, err := routes.client.ListInvites(
		workerRequestContext(request, ""),
		&iamv1.ListInvitesRequest{
			Cursor: queryString(request, "cursor", ""),
			Limit:  queryLimit(request, 20, 100),
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) listMyInvitations(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	result, err := routes.client.ListMyInvitations(
		workerRequestContext(request, ""),
		&emptypb.Empty{},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) acceptInvite(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	inviteID := strings.TrimSpace(request.PathValue("invite_id"))
	if inviteID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	_ = decodeOptionalJSONBody(response, request, &struct{}{})
	result, err := routes.client.AcceptInvite(
		workerRequestContext(request, ""),
		&iamv1.AcceptInviteRequest{InviteId: inviteID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) rejectInvite(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	inviteID := strings.TrimSpace(request.PathValue("invite_id"))
	if inviteID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	_ = decodeOptionalJSONBody(response, request, &struct{}{})
	result, err := routes.client.RejectInvite(
		workerRequestContext(request, ""),
		&iamv1.RejectInviteRequest{InviteId: inviteID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) listMembers(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	result, err := routes.client.ListMembers(
		workerRequestContext(request, ""),
		&iamv1.ListMembersRequest{
			Cursor: queryString(request, "cursor", ""),
			Limit:  queryLimit(request, 20, 100),
			Q:      queryString(request, "q", ""),
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) updateMemberRole(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	userID := strings.TrimSpace(request.PathValue("user_id"))
	if userID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	role, ok := parseMemberRole(body.Role)
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.UpdateMemberRole(
		workerRequestContext(request, ""),
		&iamv1.UpdateMemberRoleRequest{
			UserId: userID,
			Role:   role,
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) disableMember(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	userID := strings.TrimSpace(request.PathValue("user_id"))
	if userID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	_ = decodeOptionalJSONBody(response, request, &struct{}{})
	result, err := routes.client.DisableMember(
		workerRequestContext(request, ""),
		&iamv1.DisableMemberRequest{UserId: userID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) enableMember(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	userID := strings.TrimSpace(request.PathValue("user_id"))
	if userID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	_ = decodeOptionalJSONBody(response, request, &struct{}{})
	result, err := routes.client.EnableMember(
		workerRequestContext(request, ""),
		&iamv1.EnableMemberRequest{UserId: userID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) transferOwner(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	var body struct {
		SuccessorID string `json:"successor_id"`
		Confirm     bool   `json:"confirm"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.SuccessorID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.TransferOwner(
		workerRequestContext(request, ""),
		&iamv1.TransferOwnerRequest{
			SuccessorId: strings.TrimSpace(body.SuccessorID),
			Confirm:     body.Confirm,
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) cancelEnterpriseDissolution(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	_ = decodeOptionalJSONBody(response, request, &struct{}{})
	result, err := routes.client.CancelEnterpriseDissolution(
		workerRequestContext(request, ""),
		&emptypb.Empty{},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) getEnterpriseDissolutionStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	result, err := routes.client.GetEnterpriseDissolutionStatus(
		workerRequestContext(request, ""),
		&emptypb.Empty{},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *iamRoutes) searchUsers(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	q := queryString(request, "q", "")
	if q == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.SearchUsers(
		workerRequestContext(request, ""),
		&iamv1.SearchUsersRequest{
			Q:     q,
			Limit: queryLimit(request, 20, 100),
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

// parseMemberRole resolves a role string to a MemberRole. It accepts the bare
// "manager"/"member" names as well as their MEMBER_ROLE_ prefixed form; any
// other value (including owner) is rejected by this endpoint.
func parseMemberRole(value string) (iamv1.MemberRole, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" {
		return iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, false
	}
	if !strings.HasPrefix(normalized, "MEMBER_ROLE_") {
		normalized = "MEMBER_ROLE_" + normalized
	}
	switch normalized {
	case "MEMBER_ROLE_MANAGER":
		return iamv1.MemberRole_MEMBER_ROLE_MANAGER, true
	case "MEMBER_ROLE_MEMBER":
		return iamv1.MemberRole_MEMBER_ROLE_MEMBER, true
	default:
		return iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, false
	}
}
