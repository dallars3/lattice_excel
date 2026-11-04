// gateway/workspace_routes.go：首页与 Base 资产路由（H-21 ~ H-28）。
package gateway

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	workspacev1 "excel/api/workspace/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type workspaceRoutes struct {
	pipeline *Pipeline
	client   rpc.WorkspaceRPC
	iam      rpc.IamEnterpriseRPC
}

type homePayload struct {
	RecentList     []*workspacev1.Base `json:"recent_list"`
	CreatedList    []*workspacev1.Base `json:"created_list"`
	SharedList     []*workspacev1.Base `json:"shared_list"`
	StarList       []*workspacev1.Base `json:"star_list"`
	InvitationList []*iamv1.Invitation `json:"invitation_list"`
}

// RegisterWorkspaceRoutes binds the home and Base asset endpoints.
func RegisterWorkspaceRoutes(pipeline *Pipeline, client rpc.WorkspaceRPC, iam rpc.IamEnterpriseRPC) {
	if pipeline == nil {
		panic("workspace routes pipeline is required")
	}
	routes := &workspaceRoutes{pipeline: pipeline, client: client, iam: iam}
	pipeline.SessionRequired(http.MethodGet, "/api/v1/home", http.HandlerFunc(routes.getHome))
	pipeline.SessionRequired(http.MethodGet, "/api/v1/bases", http.HandlerFunc(routes.searchBases))
	pipeline.SessionRequired(http.MethodPost, "/api/v1/bases", http.HandlerFunc(routes.createBase))
	pipeline.BaseAuth(
		http.MethodPatch,
		"/api/v1/bases/{base_id}",
		sharev1.Action_ACTION_MANAGE_SHARE.String(),
		http.HandlerFunc(routes.renameBase),
	)
	pipeline.BaseAuth(
		http.MethodPost,
		"/api/v1/bases/{base_id}/duplicate",
		sharev1.Action_ACTION_COPY_BASE.String(),
		http.HandlerFunc(routes.duplicateBase),
	)
	pipeline.BaseAuth(
		http.MethodDelete,
		"/api/v1/bases/{base_id}",
		sharev1.Action_ACTION_MANAGE_SHARE.String(),
		http.HandlerFunc(routes.deleteBase),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/bases/{base_id}/star",
		http.HandlerFunc(routes.toggleStar),
	)
	pipeline.BaseAuth(
		http.MethodPost,
		"/api/v1/bases/{base_id}/transfer",
		sharev1.Action_ACTION_MANAGE_SHARE.String(),
		http.HandlerFunc(routes.transferBase),
	)
}

func (routes *workspaceRoutes) getHome(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	ctx := request.Context()
	type homeResult struct {
		response *workspacev1.GetHomeResponse
		err      error
	}
	homeCh := make(chan homeResult, 1)
	go func() {
		result, err := routes.client.GetHome(ctx, &emptypb.Empty{})
		homeCh <- homeResult{response: result, err: err}
	}()
	invitations := routes.loadInvitations(ctx)
	result := <-homeCh
	if result.err != nil {
		writeBusinessError(response, result.err)
		return
	}
	payload := homePayload{
		RecentList:     workspaceBasesOrEmpty(nil),
		CreatedList:    workspaceBasesOrEmpty(nil),
		SharedList:     workspaceBasesOrEmpty(nil),
		StarList:       workspaceBasesOrEmpty(nil),
		InvitationList: invitations,
	}
	if home := result.response.GetHome(); home != nil {
		payload.RecentList = workspaceBasesOrEmpty(home.GetRecentList())
		payload.CreatedList = workspaceBasesOrEmpty(home.GetCreatedList())
		payload.SharedList = workspaceBasesOrEmpty(home.GetSharedList())
		payload.StarList = workspaceBasesOrEmpty(home.GetStarList())
	}
	writeSuccess(response, payload)
}

func (routes *workspaceRoutes) loadInvitations(ctx context.Context) []*iamv1.Invitation {
	empty := make([]*iamv1.Invitation, 0)
	if routes.iam == nil {
		return empty
	}
	result, err := routes.iam.ListMyInvitations(ctx, &emptypb.Empty{})
	if err != nil || result == nil || result.GetInvitationList() == nil {
		return empty
	}
	return result.GetInvitationList()
}

func (routes *workspaceRoutes) searchBases(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	query := strings.TrimSpace(queryString(request, "q", ""))
	if query == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.SearchBases(
		request.Context(),
		&workspacev1.SearchBasesRequest{Q: query},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *workspaceRoutes) createBase(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.CreateBase(
		request.Context(),
		&workspacev1.CreateBaseRequest{Name: body.Name},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *workspaceRoutes) renameBase(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	baseID := strings.TrimSpace(request.PathValue("base_id"))
	if baseID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.Name) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.RenameBase(
		request.Context(),
		&workspacev1.RenameBaseRequest{BaseId: baseID, Name: strings.TrimSpace(body.Name)},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *workspaceRoutes) duplicateBase(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	baseID := strings.TrimSpace(request.PathValue("base_id"))
	if baseID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.DuplicateBase(
		request.Context(),
		&workspacev1.DuplicateBaseRequest{BaseId: baseID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *workspaceRoutes) deleteBase(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	baseID := strings.TrimSpace(request.PathValue("base_id"))
	if baseID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeOptionalJSONBody(response, request, &body); err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.DeleteBase(
		request.Context(),
		&workspacev1.DeleteBaseRequest{BaseId: baseID, Confirm: body.Confirm},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *workspaceRoutes) toggleStar(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	baseID := strings.TrimSpace(request.PathValue("base_id"))
	if baseID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		Starred bool `json:"starred"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.ToggleStar(
		request.Context(),
		&workspacev1.ToggleStarRequest{BaseId: baseID, Starred: body.Starred},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *workspaceRoutes) transferBase(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	baseID := strings.TrimSpace(request.PathValue("base_id"))
	if baseID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		NewOwnerID string `json:"new_owner_id"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.NewOwnerID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.TransferBase(
		request.Context(),
		&workspacev1.TransferBaseRequest{
			BaseId:     baseID,
			NewOwnerId: strings.TrimSpace(body.NewOwnerID),
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func workspaceBasesOrEmpty(list []*workspacev1.Base) []*workspacev1.Base {
	if list == nil {
		return []*workspacev1.Base{}
	}
	return list
}
