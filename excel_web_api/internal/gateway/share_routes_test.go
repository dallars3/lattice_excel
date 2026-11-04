package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	sharev1 "excel/api/share/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type shareManageStub struct {
	addCalls        int
	removeCalls     int
	updateRoleCalls int
	listCalls       int
	getLinkCalls    int
	updateLinkCalls int
	resetCalls      int
	closeCalls      int
	listOpenCalls   int
	forceCalls      int

	lastAdd        *sharev1.AddCollaboratorRequest
	lastRemove     *sharev1.RemoveCollaboratorRequest
	lastUpdateRole *sharev1.UpdateCollaboratorRoleRequest
	lastList       *sharev1.ListCollaboratorsRequest
	lastGetLink    *sharev1.GetLinkRequest
	lastUpdateLink *sharev1.UpdateLinkRequest
	lastReset      *sharev1.ResetLinkRequest
	lastClose      *sharev1.CloseLinkRequest
	lastListOpen   *sharev1.ListOpenLinksByEnterpriseRequest
	lastForce      *sharev1.ForceCloseByManagerRequest

	err error
}

func (stub *shareManageStub) AddCollaborator(
	_ context.Context,
	request *sharev1.AddCollaboratorRequest,
) (*sharev1.AddCollaboratorResponse, error) {
	stub.addCalls++
	stub.lastAdd = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &sharev1.AddCollaboratorResponse{
		Collaborator: &sharev1.Collaborator{
			UserId:   "user_1",
			Username: request.GetUsername(),
			Role:     request.GetRole(),
		},
	}, nil
}

func (stub *shareManageStub) RemoveCollaborator(
	_ context.Context,
	request *sharev1.RemoveCollaboratorRequest,
) (*emptypb.Empty, error) {
	stub.removeCalls++
	stub.lastRemove = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &emptypb.Empty{}, nil
}

func (stub *shareManageStub) UpdateCollaboratorRole(
	_ context.Context,
	request *sharev1.UpdateCollaboratorRoleRequest,
) (*sharev1.UpdateCollaboratorRoleResponse, error) {
	stub.updateRoleCalls++
	stub.lastUpdateRole = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &sharev1.UpdateCollaboratorRoleResponse{
		Collaborator: &sharev1.Collaborator{
			UserId: request.GetUserId(),
			Role:   request.GetRole(),
		},
	}, nil
}

func (stub *shareManageStub) ListCollaborators(
	_ context.Context,
	request *sharev1.ListCollaboratorsRequest,
) (*sharev1.ListCollaboratorsResponse, error) {
	stub.listCalls++
	stub.lastList = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &sharev1.ListCollaboratorsResponse{
		CollaboratorList: []*sharev1.Collaborator{{
			UserId:   "user_1",
			Username: "alice",
			Role:     sharev1.Role_ROLE_EDITOR,
		}},
	}, nil
}

func (stub *shareManageStub) GetLink(
	_ context.Context,
	request *sharev1.GetLinkRequest,
) (*sharev1.GetLinkResponse, error) {
	stub.getLinkCalls++
	stub.lastGetLink = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &sharev1.GetLinkResponse{
		LinkConfig: &sharev1.LinkConfig{
			Enabled: true,
			Scope:   sharev1.LinkScope_LINK_SCOPE_ENTERPRISE,
			Role:    sharev1.Role_ROLE_VIEWER,
		},
	}, nil
}

func (stub *shareManageStub) UpdateLink(
	_ context.Context,
	request *sharev1.UpdateLinkRequest,
) (*sharev1.UpdateLinkResponse, error) {
	stub.updateLinkCalls++
	stub.lastUpdateLink = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &sharev1.UpdateLinkResponse{
		LinkConfig: &sharev1.LinkConfig{
			Enabled: true,
			Scope:   request.GetScope(),
			Role:    request.GetRole(),
		},
	}, nil
}

func (stub *shareManageStub) ResetLink(
	_ context.Context,
	request *sharev1.ResetLinkRequest,
) (*sharev1.ResetLinkResponse, error) {
	stub.resetCalls++
	stub.lastReset = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &sharev1.ResetLinkResponse{
		LinkConfig: &sharev1.LinkConfig{Enabled: true},
	}, nil
}

func (stub *shareManageStub) CloseLink(
	_ context.Context,
	request *sharev1.CloseLinkRequest,
) (*emptypb.Empty, error) {
	stub.closeCalls++
	stub.lastClose = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &emptypb.Empty{}, nil
}

func (stub *shareManageStub) ListOpenLinksByEnterprise(
	_ context.Context,
	request *sharev1.ListOpenLinksByEnterpriseRequest,
) (*sharev1.ListOpenLinksByEnterpriseResponse, error) {
	stub.listOpenCalls++
	stub.lastListOpen = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &sharev1.ListOpenLinksByEnterpriseResponse{
		LinkList:   []*sharev1.OpenLink{{BaseId: "base_1", BaseName: "客户数据"}},
		HasMore:    true,
		NextCursor: "next",
	}, nil
}

func (stub *shareManageStub) ForceCloseByManager(
	_ context.Context,
	request *sharev1.ForceCloseByManagerRequest,
) (*emptypb.Empty, error) {
	stub.forceCalls++
	stub.lastForce = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &emptypb.Empty{}, nil
}

var _ rpc.ShareManageRPC = (*shareManageStub)(nil)

func TestShareRoutesRequireSession(t *testing.T) {
	pipeline := NewPipeline(&pipelineIamRPC{}, nil, nil, nil)
	RegisterShareRoutes(pipeline, &shareManageStub{})

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/bases/base_1/shares/members"},
		{http.MethodPost, "/api/v1/bases/base_1/shares/members"},
		{http.MethodPut, "/api/v1/shares/members/user_1/role"},
		{http.MethodDelete, "/api/v1/shares/members/user_1"},
		{http.MethodGet, "/api/v1/bases/base_1/link"},
		{http.MethodPut, "/api/v1/bases/base_1/link"},
		{http.MethodPost, "/api/v1/bases/base_1/link/reset"},
		{http.MethodPost, "/api/v1/bases/base_1/link/close"},
		{http.MethodGet, "/api/v1/enterprise/open-links"},
		{http.MethodPost, "/api/v1/enterprise/open-links/link_1/close"},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			pipeline.Handler().ServeHTTP(response, request)
			assertCodeEnvelope(t, response, http.StatusUnauthorized, deal.CodeUnauthenticated)
		})
	}
}

func TestShareRoutesForwardRequests(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "e1")}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	client := &shareManageStub{}
	pipeline := NewPipeline(iam, share, nil, nil)
	RegisterShareRoutes(pipeline, client)

	members := performGatewayRequest(
		t,
		pipeline,
		http.MethodGet,
		"/api/v1/bases/base_1/shares/members?q=ali",
		"",
	)
	assertGatewaySuccess(t, members, func(data map[string]any) {
		if _, ok := data["collaborator_list"].([]any); !ok {
			t.Fatalf("members data = %+v", data)
		}
	})
	if client.listCalls != 1 ||
		client.lastList.GetBaseId() != "base_1" ||
		client.lastList.GetQ() != "ali" {
		t.Fatalf("ListCollaborators calls=%d request=%v", client.listCalls, client.lastList)
	}

	add := performGatewayRequest(
		t,
		pipeline,
		http.MethodPost,
		"/api/v1/bases/base_1/shares/members",
		`{"username":"alice","role":"editor"}`,
	)
	assertGatewaySuccess(t, add, func(data map[string]any) {
		if _, ok := data["collaborator"].(map[string]any); !ok {
			t.Fatalf("add data = %+v", data)
		}
	})
	if client.addCalls != 1 ||
		client.lastAdd.GetBaseId() != "base_1" ||
		client.lastAdd.GetUsername() != "alice" ||
		client.lastAdd.GetRole() != sharev1.Role_ROLE_EDITOR {
		t.Fatalf("AddCollaborator calls=%d request=%v", client.addCalls, client.lastAdd)
	}

	updateRole := performGatewayRequest(
		t,
		pipeline,
		http.MethodPut,
		"/api/v1/shares/members/user_1/role",
		`{"base_id":"base_9","role":"manager"}`,
	)
	assertGatewaySuccess(t, updateRole, func(data map[string]any) {
		if _, ok := data["collaborator"].(map[string]any); !ok {
			t.Fatalf("update role data = %+v", data)
		}
	})
	if client.updateRoleCalls != 1 ||
		client.lastUpdateRole.GetBaseId() != "base_9" ||
		client.lastUpdateRole.GetUserId() != "user_1" ||
		client.lastUpdateRole.GetRole() != sharev1.Role_ROLE_MANAGER {
		t.Fatalf("UpdateCollaboratorRole calls=%d request=%v", client.updateRoleCalls, client.lastUpdateRole)
	}
	if share.lastCheck.GetBaseId() != "base_9" ||
		share.lastCheck.GetAction() != sharev1.Action_ACTION_MANAGE_SHARE {
		t.Fatalf("CheckAccess request = %v", share.lastCheck)
	}

	remove := performGatewayRequest(
		t,
		pipeline,
		http.MethodDelete,
		"/api/v1/shares/members/user_1",
		`{"base_id":"base_9"}`,
	)
	assertGatewaySuccess(t, remove, func(map[string]any) {})
	if client.removeCalls != 1 ||
		client.lastRemove.GetBaseId() != "base_9" ||
		client.lastRemove.GetUserId() != "user_1" {
		t.Fatalf("RemoveCollaborator calls=%d request=%v", client.removeCalls, client.lastRemove)
	}
	if share.lastCheck.GetBaseId() != "base_9" {
		t.Fatalf("CheckAccess base_id = %q, want base_9", share.lastCheck.GetBaseId())
	}

	getLink := performGatewayRequest(
		t,
		pipeline,
		http.MethodGet,
		"/api/v1/bases/base_1/link",
		"",
	)
	assertGatewaySuccess(t, getLink, func(data map[string]any) {
		if _, ok := data["link_config"].(map[string]any); !ok {
			t.Fatalf("get link data = %+v", data)
		}
	})
	if client.getLinkCalls != 1 || client.lastGetLink.GetBaseId() != "base_1" {
		t.Fatalf("GetLink calls=%d request=%v", client.getLinkCalls, client.lastGetLink)
	}

	updateLink := performGatewayRequest(
		t,
		pipeline,
		http.MethodPut,
		"/api/v1/bases/base_1/link",
		`{"scope":"anyone","role":"viewer"}`,
	)
	assertGatewaySuccess(t, updateLink, func(data map[string]any) {
		if _, ok := data["link_config"].(map[string]any); !ok {
			t.Fatalf("update link data = %+v", data)
		}
	})
	if client.updateLinkCalls != 1 ||
		client.lastUpdateLink.GetBaseId() != "base_1" ||
		client.lastUpdateLink.GetScope() != sharev1.LinkScope_LINK_SCOPE_ANYONE ||
		client.lastUpdateLink.GetRole() != sharev1.Role_ROLE_VIEWER {
		t.Fatalf("UpdateLink calls=%d request=%v", client.updateLinkCalls, client.lastUpdateLink)
	}

	reset := performGatewayRequest(
		t,
		pipeline,
		http.MethodPost,
		"/api/v1/bases/base_1/link/reset",
		"",
	)
	assertGatewaySuccess(t, reset, func(map[string]any) {})
	if client.resetCalls != 1 || client.lastReset.GetBaseId() != "base_1" {
		t.Fatalf("ResetLink calls=%d request=%v", client.resetCalls, client.lastReset)
	}

	close := performGatewayRequest(
		t,
		pipeline,
		http.MethodPost,
		"/api/v1/bases/base_1/link/close",
		"",
	)
	assertGatewaySuccess(t, close, func(map[string]any) {})
	if client.closeCalls != 1 || client.lastClose.GetBaseId() != "base_1" {
		t.Fatalf("CloseLink calls=%d request=%v", client.closeCalls, client.lastClose)
	}

	openLinks := performGatewayRequest(
		t,
		pipeline,
		http.MethodGet,
		"/api/v1/enterprise/open-links?cursor=c&limit=3",
		"",
	)
	assertGatewaySuccess(t, openLinks, func(data map[string]any) {
		if _, ok := data["link_list"].([]any); !ok {
			t.Fatalf("open links data = %+v", data)
		}
	})
	if client.listOpenCalls != 1 ||
		client.lastListOpen.GetCursor() != "c" ||
		client.lastListOpen.GetLimit() != 3 {
		t.Fatalf("ListOpenLinksByEnterprise calls=%d request=%v", client.listOpenCalls, client.lastListOpen)
	}

	force := performGatewayRequest(
		t,
		pipeline,
		http.MethodPost,
		"/api/v1/enterprise/open-links/link_1/close",
		"",
	)
	assertGatewaySuccess(t, force, func(map[string]any) {})
	if client.forceCalls != 1 || client.lastForce.GetLinkId() != "link_1" {
		t.Fatalf("ForceCloseByManager calls=%d request=%v", client.forceCalls, client.lastForce)
	}
}

func TestShareRouteMapsBusinessError(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "e1")}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	client := &shareManageStub{err: status.Error(codes.PermissionDenied, "manage share denied")}
	pipeline := NewPipeline(iam, share, nil, nil)
	RegisterShareRoutes(pipeline, client)

	response := performGatewayRequest(
		t,
		pipeline,
		http.MethodGet,
		"/api/v1/bases/base_1/shares/members",
		"",
	)
	assertCodeEnvelope(t, response, http.StatusForbidden, deal.CodePermissionDenied)
}

func TestShareRoleAndScopeParsing(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		wantRole sharev1.Role
		wantOK   bool
	}{
		{name: "viewer", role: "viewer", wantRole: sharev1.Role_ROLE_VIEWER, wantOK: true},
		{name: "filler", role: "filler", wantRole: sharev1.Role_ROLE_FILLER, wantOK: true},
		{name: "editor", role: "editor", wantRole: sharev1.Role_ROLE_EDITOR, wantOK: true},
		{name: "manager", role: "manager", wantRole: sharev1.Role_ROLE_MANAGER, wantOK: true},
		{name: "role prefix", role: "ROLE_EDITOR", wantRole: sharev1.Role_ROLE_EDITOR, wantOK: true},
		{name: "case insensitive", role: "Editor", wantRole: sharev1.Role_ROLE_EDITOR, wantOK: true},
		{name: "unknown", role: "admin", wantOK: false},
		{name: "empty", role: "", wantOK: false},
	}
	for _, test := range tests {
		t.Run("role "+test.name, func(t *testing.T) {
			role, ok := parseShareRole(test.role)
			if ok != test.wantOK || role != test.wantRole {
				t.Fatalf("parseShareRole(%q) = (%v, %v), want (%v, %v)", test.role, role, ok, test.wantRole, test.wantOK)
			}
		})
	}

	scopeTests := []struct {
		name      string
		scope     string
		wantScope sharev1.LinkScope
		wantOK    bool
	}{
		{name: "none", scope: "none", wantScope: sharev1.LinkScope_LINK_SCOPE_NONE, wantOK: true},
		{name: "enterprise", scope: "enterprise", wantScope: sharev1.LinkScope_LINK_SCOPE_ENTERPRISE, wantOK: true},
		{name: "anyone", scope: "anyone", wantScope: sharev1.LinkScope_LINK_SCOPE_ANYONE, wantOK: true},
		{name: "prefix", scope: "LINK_SCOPE_NONE", wantScope: sharev1.LinkScope_LINK_SCOPE_NONE, wantOK: true},
		{name: "unknown", scope: "public", wantOK: false},
		{name: "empty", scope: "", wantOK: false},
	}
	for _, test := range scopeTests {
		t.Run("scope "+test.name, func(t *testing.T) {
			scope, ok := parseLinkScope(test.scope)
			if ok != test.wantOK || scope != test.wantScope {
				t.Fatalf("parseLinkScope(%q) = (%v, %v), want (%v, %v)", test.scope, scope, ok, test.wantScope, test.wantOK)
			}
		})
	}
}
