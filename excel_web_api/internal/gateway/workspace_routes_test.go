package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	workspacev1 "excel/api/workspace/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type workspaceStub struct {
	homeResponse      *workspacev1.GetHomeResponse
	homeErr           error
	homeCalls         int
	searchResponse    *workspacev1.SearchBasesResponse
	searchErr         error
	searchCalls       int
	createResponse    *workspacev1.CreateBaseResponse
	renameResponse    *workspacev1.RenameBaseResponse
	duplicateResponse *workspacev1.DuplicateBaseResponse
	toggleResponse    *workspacev1.ToggleStarResponse
	lastSearch        *workspacev1.SearchBasesRequest
	lastCreate        *workspacev1.CreateBaseRequest
	lastRename        *workspacev1.RenameBaseRequest
	lastDuplicate     *workspacev1.DuplicateBaseRequest
	lastDelete        *workspacev1.DeleteBaseRequest
	lastToggle        *workspacev1.ToggleStarRequest
	lastTransfer      *workspacev1.TransferBaseRequest
}

func (stub *workspaceStub) GetHome(
	_ context.Context,
	_ *emptypb.Empty,
) (*workspacev1.GetHomeResponse, error) {
	stub.homeCalls++
	return stub.homeResponse, stub.homeErr
}

func (stub *workspaceStub) SearchBases(
	_ context.Context,
	request *workspacev1.SearchBasesRequest,
) (*workspacev1.SearchBasesResponse, error) {
	stub.searchCalls++
	stub.lastSearch = request
	return stub.searchResponse, stub.searchErr
}

func (stub *workspaceStub) CreateBase(
	_ context.Context,
	request *workspacev1.CreateBaseRequest,
) (*workspacev1.CreateBaseResponse, error) {
	stub.lastCreate = request
	return stub.createResponse, nil
}

func (stub *workspaceStub) RenameBase(
	_ context.Context,
	request *workspacev1.RenameBaseRequest,
) (*workspacev1.RenameBaseResponse, error) {
	stub.lastRename = request
	return stub.renameResponse, nil
}

func (stub *workspaceStub) DuplicateBase(
	_ context.Context,
	request *workspacev1.DuplicateBaseRequest,
) (*workspacev1.DuplicateBaseResponse, error) {
	stub.lastDuplicate = request
	return stub.duplicateResponse, nil
}

func (stub *workspaceStub) DeleteBase(
	_ context.Context,
	request *workspacev1.DeleteBaseRequest,
) (*emptypb.Empty, error) {
	stub.lastDelete = request
	return &emptypb.Empty{}, nil
}

func (stub *workspaceStub) ToggleStar(
	_ context.Context,
	request *workspacev1.ToggleStarRequest,
) (*workspacev1.ToggleStarResponse, error) {
	stub.lastToggle = request
	return stub.toggleResponse, nil
}

func (stub *workspaceStub) TransferBase(
	_ context.Context,
	request *workspacev1.TransferBaseRequest,
) (*emptypb.Empty, error) {
	stub.lastTransfer = request
	return &emptypb.Empty{}, nil
}

var _ rpc.WorkspaceRPC = (*workspaceStub)(nil)

type iamStub struct {
	invitationResponse *iamv1.ListMyInvitationsResponse
	invitationErr      error
	invitationCalls    int
}

func (stub *iamStub) RequestDeactivate(context.Context, *emptypb.Empty) (*iamv1.RequestDeactivateResponse, error) {
	return nil, nil
}

func (stub *iamStub) CancelDeactivate(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return nil, nil
}

func (stub *iamStub) GetEnterprise(context.Context, *emptypb.Empty) (*iamv1.GetEnterpriseResponse, error) {
	return nil, nil
}

func (stub *iamStub) InviteMember(context.Context, *iamv1.InviteMemberRequest) (*iamv1.InviteMemberResponse, error) {
	return nil, nil
}

func (stub *iamStub) CancelInvite(context.Context, *iamv1.CancelInviteRequest) (*emptypb.Empty, error) {
	return nil, nil
}

func (stub *iamStub) ListInvites(context.Context, *iamv1.ListInvitesRequest) (*iamv1.ListInvitesResponse, error) {
	return nil, nil
}

func (stub *iamStub) ListMyInvitations(
	_ context.Context,
	_ *emptypb.Empty,
) (*iamv1.ListMyInvitationsResponse, error) {
	stub.invitationCalls++
	return stub.invitationResponse, stub.invitationErr
}

func (stub *iamStub) AcceptInvite(context.Context, *iamv1.AcceptInviteRequest) (*emptypb.Empty, error) {
	return nil, nil
}

func (stub *iamStub) RejectInvite(context.Context, *iamv1.RejectInviteRequest) (*emptypb.Empty, error) {
	return nil, nil
}

func (stub *iamStub) ListMembers(context.Context, *iamv1.ListMembersRequest) (*iamv1.ListMembersResponse, error) {
	return nil, nil
}

func (stub *iamStub) UpdateMemberRole(context.Context, *iamv1.UpdateMemberRoleRequest) (*iamv1.UpdateMemberRoleResponse, error) {
	return nil, nil
}

func (stub *iamStub) DisableMember(context.Context, *iamv1.DisableMemberRequest) (*emptypb.Empty, error) {
	return nil, nil
}

func (stub *iamStub) EnableMember(context.Context, *iamv1.EnableMemberRequest) (*emptypb.Empty, error) {
	return nil, nil
}

func (stub *iamStub) TransferOwner(context.Context, *iamv1.TransferOwnerRequest) (*emptypb.Empty, error) {
	return nil, nil
}

func (stub *iamStub) CancelEnterpriseDissolution(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return nil, nil
}

func (stub *iamStub) GetEnterpriseDissolutionStatus(context.Context, *emptypb.Empty) (*iamv1.GetEnterpriseDissolutionStatusResponse, error) {
	return nil, nil
}

func (stub *iamStub) SearchUsers(context.Context, *iamv1.SearchUsersRequest) (*iamv1.SearchUsersResponse, error) {
	return nil, nil
}

var _ rpc.IamEnterpriseRPC = (*iamStub)(nil)

type workspaceTestContext struct {
	ws       *workspaceStub
	invites  *iamStub
	share    *pipelineShareRPC
	pipeline *Pipeline
}

func newWorkspaceTestContext(t *testing.T) *workspaceTestContext {
	t.Helper()
	ws := &workspaceStub{
		homeResponse: &workspacev1.GetHomeResponse{Home: &workspacev1.Home{
			RecentList:  []*workspacev1.Base{{Id: "base_r", Name: "Recent"}},
			CreatedList: []*workspacev1.Base{{Id: "base_c", Name: "Created"}},
			SharedList:  []*workspacev1.Base{{Id: "base_s", Name: "Shared"}},
			StarList:    []*workspacev1.Base{{Id: "base_star", Name: "Star"}},
		}},
		searchResponse:    &workspacev1.SearchBasesResponse{BaseList: []*workspacev1.Base{{Id: "base_hit"}}},
		createResponse:    &workspacev1.CreateBaseResponse{Base: &workspacev1.Base{Id: "base_new", Name: "New"}},
		renameResponse:    &workspacev1.RenameBaseResponse{Base: &workspacev1.Base{Id: "base_1", Name: "Renamed"}},
		duplicateResponse: &workspacev1.DuplicateBaseResponse{Base: &workspacev1.Base{Id: "base_copy"}},
		toggleResponse:    &workspacev1.ToggleStarResponse{Base: &workspacev1.Base{Id: "base_1", Starred: true}},
	}
	invites := &iamStub{
		invitationResponse: &iamv1.ListMyInvitationsResponse{
			InvitationList: []*iamv1.Invitation{{Id: "i1"}},
		},
	}
	sessionIAM := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "enterprise_1")}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	pipeline := NewPipeline(sessionIAM, share, nil, nil)
	RegisterWorkspaceRoutes(pipeline, ws, invites)
	return &workspaceTestContext{ws: ws, invites: invites, share: share, pipeline: pipeline}
}

func workspaceRouteRequest(
	pipeline *Pipeline,
	method string,
	path string,
	body string,
	withCookie bool,
) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if withCookie {
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	}
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)
	return response
}

func decodeWorkspaceEnvelope(
	t *testing.T,
	response *httptest.ResponseRecorder,
) (int, int, map[string]any) {
	t.Helper()
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response.Code, envelope.Code, envelope.Data
}

func TestWorkspaceRoutesRejectMissingSession(t *testing.T) {
	context := newWorkspaceTestContext(t)
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/home"},
		{http.MethodGet, "/api/v1/bases?q=hello"},
		{http.MethodPost, "/api/v1/bases"},
		{http.MethodPatch, "/api/v1/bases/base_1"},
		{http.MethodPost, "/api/v1/bases/base_1/duplicate"},
		{http.MethodDelete, "/api/v1/bases/base_1"},
		{http.MethodPost, "/api/v1/bases/base_1/star"},
		{http.MethodPost, "/api/v1/bases/base_1/transfer"},
	}
	for _, route := range routes {
		response := workspaceRouteRequest(context.pipeline, route.method, route.path, "", false)
		statusCode, code, _ := decodeWorkspaceEnvelope(t, response)
		if statusCode != http.StatusUnauthorized || code != deal.CodeUnauthenticated {
			t.Fatalf(
				"%s %s = HTTP %d code %d, want 401/2001",
				route.method,
				route.path,
				statusCode,
				code,
			)
		}
	}
	if context.share.checkCalls != 0 {
		t.Fatalf("CheckAccess calls = %d, want zero before session resolve", context.share.checkCalls)
	}
}

func TestWorkspaceRoutesForwardSuccessfully(t *testing.T) {
	context := newWorkspaceTestContext(t)
	routes := []struct {
		method     string
		path       string
		body       string
		dataKey    string
		dataID     string
		assertStub func(t *testing.T, ws *workspaceStub)
	}{
		{
			method:  http.MethodGet,
			path:    "/api/v1/home",
			dataKey: "recent_list",
			dataID:  "base_r",
			assertStub: func(t *testing.T, ws *workspaceStub) {
				if ws.homeCalls != 1 {
					t.Fatalf("GetHome calls = %d, want 1", ws.homeCalls)
				}
			},
		},
		{
			method:  http.MethodGet,
			path:    "/api/v1/bases?q=hello",
			dataKey: "base_list",
			dataID:  "base_hit",
			assertStub: func(t *testing.T, ws *workspaceStub) {
				if ws.lastSearch.GetQ() != "hello" {
					t.Fatalf("SearchBases q = %q, want hello", ws.lastSearch.GetQ())
				}
			},
		},
		{
			method:  http.MethodPost,
			path:    "/api/v1/bases",
			body:    `{"name":"MyBase"}`,
			dataKey: "base",
			dataID:  "base_new",
			assertStub: func(t *testing.T, ws *workspaceStub) {
				if ws.lastCreate.GetName() != "MyBase" {
					t.Fatalf("CreateBase name = %q, want MyBase", ws.lastCreate.GetName())
				}
			},
		},
		{
			method:  http.MethodPatch,
			path:    "/api/v1/bases/base_1",
			body:    `{"name":"Renamed"}`,
			dataKey: "base",
			dataID:  "base_1",
			assertStub: func(t *testing.T, ws *workspaceStub) {
				if ws.lastRename.GetBaseId() != "base_1" || ws.lastRename.GetName() != "Renamed" {
					t.Fatalf("RenameBase request = %v", ws.lastRename)
				}
			},
		},
		{
			method:  http.MethodPost,
			path:    "/api/v1/bases/base_1/duplicate",
			dataKey: "base",
			dataID:  "base_copy",
			assertStub: func(t *testing.T, ws *workspaceStub) {
				if ws.lastDuplicate.GetBaseId() != "base_1" {
					t.Fatalf("DuplicateBase base_id = %q, want base_1", ws.lastDuplicate.GetBaseId())
				}
			},
		},
		{
			method:  http.MethodDelete,
			path:    "/api/v1/bases/base_1",
			body:    `{"confirm":true}`,
			dataKey: "",
			assertStub: func(t *testing.T, ws *workspaceStub) {
				if ws.lastDelete.GetBaseId() != "base_1" || !ws.lastDelete.GetConfirm() {
					t.Fatalf("DeleteBase request = %v", ws.lastDelete)
				}
			},
		},
		{
			method:  http.MethodPost,
			path:    "/api/v1/bases/base_1/star",
			body:    `{"starred":true}`,
			dataKey: "base",
			dataID:  "base_1",
			assertStub: func(t *testing.T, ws *workspaceStub) {
				if ws.lastToggle.GetBaseId() != "base_1" || !ws.lastToggle.GetStarred() {
					t.Fatalf("ToggleStar request = %v", ws.lastToggle)
				}
			},
		},
		{
			method:  http.MethodPost,
			path:    "/api/v1/bases/base_1/transfer",
			body:    `{"new_owner_id":"user_2"}`,
			dataKey: "",
			assertStub: func(t *testing.T, ws *workspaceStub) {
				if ws.lastTransfer.GetBaseId() != "base_1" || ws.lastTransfer.GetNewOwnerId() != "user_2" {
					t.Fatalf("TransferBase request = %v", ws.lastTransfer)
				}
			},
		},
	}

	for _, route := range routes {
		response := workspaceRouteRequest(context.pipeline, route.method, route.path, route.body, true)
		statusCode, code, data := decodeWorkspaceEnvelope(t, response)
		if statusCode != http.StatusOK || code != 0 {
			t.Fatalf(
				"%s %s = HTTP %d code %d, want success",
				route.method,
				route.path,
				statusCode,
				code,
			)
		}
		if route.dataKey != "" {
			item, ok := data[route.dataKey]
			if !ok {
				t.Fatalf("%s %s: data missing key %q: %+v", route.method, route.path, route.dataKey, data)
			}
			assertFirstID(t, item, route.dataID)
		}
		route.assertStub(t, context.ws)
	}
	if context.invites.invitationCalls != 1 {
		t.Fatalf("ListMyInvitations calls = %d, want 1", context.invites.invitationCalls)
	}
}

func TestWorkspaceHomeIncludesInvitations(t *testing.T) {
	context := newWorkspaceTestContext(t)
	response := workspaceRouteRequest(context.pipeline, http.MethodGet, "/api/v1/home", "", true)
	statusCode, code, data := decodeWorkspaceEnvelope(t, response)
	if statusCode != http.StatusOK || code != 0 {
		t.Fatalf("home = HTTP %d code %d, want success", statusCode, code)
	}
	invitationList, ok := data["invitation_list"].([]any)
	if !ok || len(invitationList) != 1 {
		t.Fatalf("invitation_list = %+v, want one invitation", data["invitation_list"])
	}
	first, ok := invitationList[0].(map[string]any)
	if !ok || first["id"] != "i1" {
		t.Fatalf("invitation_list[0] = %+v, want id i1", invitationList[0])
	}
}

func TestWorkspaceSearchRequiresQuery(t *testing.T) {
	context := newWorkspaceTestContext(t)
	response := workspaceRouteRequest(context.pipeline, http.MethodGet, "/api/v1/bases", "", true)
	statusCode, code, _ := decodeWorkspaceEnvelope(t, response)
	if statusCode != http.StatusBadRequest || code != deal.CodeInvalidArgument {
		t.Fatalf("search without q = HTTP %d code %d, want 400/1001", statusCode, code)
	}
	if context.ws.searchCalls != 0 {
		t.Fatalf("SearchBases calls = %d, want 0", context.ws.searchCalls)
	}
}

func TestWorkspaceRoutesMapGRPCError(t *testing.T) {
	context := newWorkspaceTestContext(t)
	context.ws.searchErr = status.Error(codes.NotFound, "base gone")
	response := workspaceRouteRequest(context.pipeline, http.MethodGet, "/api/v1/bases?q=gone", "", true)
	statusCode, code, _ := decodeWorkspaceEnvelope(t, response)
	if statusCode != http.StatusConflict || code != deal.CodeDeletedResource {
		t.Fatalf(
			"search gRPC error = HTTP %d code %d, want 409/4002",
			statusCode,
			code,
		)
	}
}

func assertFirstID(t *testing.T, item any, wantID string) {
	t.Helper()
	if list, ok := item.([]any); ok && len(list) > 0 {
		if entry, ok := list[0].(map[string]any); ok {
			if entry["id"] == wantID {
				return
			}
		}
		t.Fatalf("first entry = %+v, want id %q", item, wantID)
	}
	if obj, ok := item.(map[string]any); ok {
		if obj["id"] == wantID {
			return
		}
		t.Fatalf("data = %+v, want id %q", item, wantID)
	}
	t.Fatalf("data = %+v, want id %q", item, wantID)
}
