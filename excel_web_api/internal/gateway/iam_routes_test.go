package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type iamEnterpriseGatewayStub struct {
	requestDeactivateResponse *iamv1.RequestDeactivateResponse
	requestDeactivateErr      error
	getEnterpriseResponse     *iamv1.GetEnterpriseResponse
	getEnterpriseErr          error
	inviteMemberResponse      *iamv1.InviteMemberResponse
	inviteMemberErr           error
	listInvitesResponse       *iamv1.ListInvitesResponse
	listInvitesErr            error
	listMyInvitationsResponse *iamv1.ListMyInvitationsResponse
	listMembersResponse       *iamv1.ListMembersResponse
	listMembersErr            error
	updateMemberRoleResponse  *iamv1.UpdateMemberRoleResponse
	updateMemberRoleErr       error
	dissolutionStatusResponse *iamv1.GetEnterpriseDissolutionStatusResponse
	searchUsersResponse       *iamv1.SearchUsersResponse

	requestDeactivateCalls int
	cancelDeactivateCalls  int
	getEnterpriseCalls     int
	inviteMemberCalls      int
	cancelInviteCalls      int
	listInvitesCalls       int
	listMyInvitationsCalls int
	acceptInviteCalls      int
	rejectInviteCalls      int
	listMembersCalls       int
	updateMemberRoleCalls  int
	disableMemberCalls     int
	enableMemberCalls      int
	transferOwnerCalls     int
	cancelDissolutionCalls int
	getDissolutionCalls    int
	searchUsersCalls       int

	lastInviteMember     *iamv1.InviteMemberRequest
	lastCancelInvite     *iamv1.CancelInviteRequest
	lastListInvites      *iamv1.ListInvitesRequest
	lastAcceptInvite     *iamv1.AcceptInviteRequest
	lastRejectInvite     *iamv1.RejectInviteRequest
	lastListMembers      *iamv1.ListMembersRequest
	lastUpdateMemberRole *iamv1.UpdateMemberRoleRequest
	lastDisableMember    *iamv1.DisableMemberRequest
	lastEnableMember     *iamv1.EnableMemberRequest
	lastTransferOwner    *iamv1.TransferOwnerRequest
	lastSearchUsers      *iamv1.SearchUsersRequest
}

func (stub *iamEnterpriseGatewayStub) RequestDeactivate(
	context.Context,
	*emptypb.Empty,
) (*iamv1.RequestDeactivateResponse, error) {
	stub.requestDeactivateCalls++
	return stub.requestDeactivateResponse, stub.requestDeactivateErr
}

func (stub *iamEnterpriseGatewayStub) CancelDeactivate(
	context.Context,
	*emptypb.Empty,
) (*emptypb.Empty, error) {
	stub.cancelDeactivateCalls++
	return &emptypb.Empty{}, nil
}

func (stub *iamEnterpriseGatewayStub) GetEnterprise(
	context.Context,
	*emptypb.Empty,
) (*iamv1.GetEnterpriseResponse, error) {
	stub.getEnterpriseCalls++
	return stub.getEnterpriseResponse, stub.getEnterpriseErr
}

func (stub *iamEnterpriseGatewayStub) InviteMember(
	_ context.Context,
	request *iamv1.InviteMemberRequest,
) (*iamv1.InviteMemberResponse, error) {
	stub.inviteMemberCalls++
	stub.lastInviteMember = request
	return stub.inviteMemberResponse, stub.inviteMemberErr
}

func (stub *iamEnterpriseGatewayStub) CancelInvite(
	_ context.Context,
	request *iamv1.CancelInviteRequest,
) (*emptypb.Empty, error) {
	stub.cancelInviteCalls++
	stub.lastCancelInvite = request
	return &emptypb.Empty{}, nil
}

func (stub *iamEnterpriseGatewayStub) ListInvites(
	_ context.Context,
	request *iamv1.ListInvitesRequest,
) (*iamv1.ListInvitesResponse, error) {
	stub.listInvitesCalls++
	stub.lastListInvites = request
	return stub.listInvitesResponse, stub.listInvitesErr
}

func (stub *iamEnterpriseGatewayStub) ListMyInvitations(
	context.Context,
	*emptypb.Empty,
) (*iamv1.ListMyInvitationsResponse, error) {
	stub.listMyInvitationsCalls++
	return stub.listMyInvitationsResponse, nil
}

func (stub *iamEnterpriseGatewayStub) AcceptInvite(
	_ context.Context,
	request *iamv1.AcceptInviteRequest,
) (*emptypb.Empty, error) {
	stub.acceptInviteCalls++
	stub.lastAcceptInvite = request
	return &emptypb.Empty{}, nil
}

func (stub *iamEnterpriseGatewayStub) RejectInvite(
	_ context.Context,
	request *iamv1.RejectInviteRequest,
) (*emptypb.Empty, error) {
	stub.rejectInviteCalls++
	stub.lastRejectInvite = request
	return &emptypb.Empty{}, nil
}

func (stub *iamEnterpriseGatewayStub) ListMembers(
	_ context.Context,
	request *iamv1.ListMembersRequest,
) (*iamv1.ListMembersResponse, error) {
	stub.listMembersCalls++
	stub.lastListMembers = request
	return stub.listMembersResponse, stub.listMembersErr
}

func (stub *iamEnterpriseGatewayStub) UpdateMemberRole(
	_ context.Context,
	request *iamv1.UpdateMemberRoleRequest,
) (*iamv1.UpdateMemberRoleResponse, error) {
	stub.updateMemberRoleCalls++
	stub.lastUpdateMemberRole = request
	return stub.updateMemberRoleResponse, stub.updateMemberRoleErr
}

func (stub *iamEnterpriseGatewayStub) DisableMember(
	_ context.Context,
	request *iamv1.DisableMemberRequest,
) (*emptypb.Empty, error) {
	stub.disableMemberCalls++
	stub.lastDisableMember = request
	return &emptypb.Empty{}, nil
}

func (stub *iamEnterpriseGatewayStub) EnableMember(
	_ context.Context,
	request *iamv1.EnableMemberRequest,
) (*emptypb.Empty, error) {
	stub.enableMemberCalls++
	stub.lastEnableMember = request
	return &emptypb.Empty{}, nil
}

func (stub *iamEnterpriseGatewayStub) TransferOwner(
	_ context.Context,
	request *iamv1.TransferOwnerRequest,
) (*emptypb.Empty, error) {
	stub.transferOwnerCalls++
	stub.lastTransferOwner = request
	return &emptypb.Empty{}, nil
}

func (stub *iamEnterpriseGatewayStub) CancelEnterpriseDissolution(
	context.Context,
	*emptypb.Empty,
) (*emptypb.Empty, error) {
	stub.cancelDissolutionCalls++
	return &emptypb.Empty{}, nil
}

func (stub *iamEnterpriseGatewayStub) GetEnterpriseDissolutionStatus(
	context.Context,
	*emptypb.Empty,
) (*iamv1.GetEnterpriseDissolutionStatusResponse, error) {
	stub.getDissolutionCalls++
	return stub.dissolutionStatusResponse, nil
}

func (stub *iamEnterpriseGatewayStub) SearchUsers(
	_ context.Context,
	request *iamv1.SearchUsersRequest,
) (*iamv1.SearchUsersResponse, error) {
	stub.searchUsersCalls++
	stub.lastSearchUsers = request
	return stub.searchUsersResponse, nil
}

var _ rpc.IamEnterpriseRPC = (*iamEnterpriseGatewayStub)(nil)

func withIamRoutes() (*Pipeline, *iamEnterpriseGatewayStub) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	stub := &iamEnterpriseGatewayStub{
		requestDeactivateResponse: &iamv1.RequestDeactivateResponse{DeadlineMs: 12345},
		getEnterpriseResponse: &iamv1.GetEnterpriseResponse{
			Enterprise: &iamv1.Enterprise{Id: "ent_1", Name: "启航科技"},
		},
		inviteMemberResponse: &iamv1.InviteMemberResponse{
			InvitationList: []*iamv1.Invitation{{Id: "inv_1", Username: "alice"}},
		},
		listInvitesResponse: &iamv1.ListInvitesResponse{
			InvitationList: []*iamv1.Invitation{{Id: "inv_1"}},
			HasMore:        true,
			NextCursor:     "next",
		},
		listMyInvitationsResponse: &iamv1.ListMyInvitationsResponse{
			InvitationList: []*iamv1.Invitation{{Id: "inv_1"}},
		},
		listMembersResponse: &iamv1.ListMembersResponse{
			MemberList: []*iamv1.Member{{UserId: "user_2", Role: iamv1.MemberRole_MEMBER_ROLE_MANAGER}},
			HasMore:    false,
		},
		updateMemberRoleResponse: &iamv1.UpdateMemberRoleResponse{
			Member: &iamv1.Member{UserId: "user_2", Role: iamv1.MemberRole_MEMBER_ROLE_MANAGER},
		},
		dissolutionStatusResponse: &iamv1.GetEnterpriseDissolutionStatusResponse{
			Status:     iamv1.DissolutionStatus_DISSOLUTION_STATUS_PENDING,
			DeadlineMs: 999,
		},
		searchUsersResponse: &iamv1.SearchUsersResponse{
			UserList: []*iamv1.User{{Id: "user_3", Username: "alice"}},
		},
	}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterIamRoutes(pipeline, stub)
	return pipeline, stub
}

func TestIamRoutesRejectMissingSession(t *testing.T) {
	pipeline, _ := withIamRoutes()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "request deactivate", method: http.MethodPost, path: "/api/v1/me/deactivate"},
		{name: "cancel deactivate", method: http.MethodPost, path: "/api/v1/me/deactivate/cancel"},
		{name: "get enterprise", method: http.MethodGet, path: "/api/v1/enterprise"},
		{name: "invite member", method: http.MethodPost, path: "/api/v1/enterprise/invites"},
		{name: "cancel invite", method: http.MethodDelete, path: "/api/v1/enterprise/invites/inv_1"},
		{name: "list invites", method: http.MethodGet, path: "/api/v1/enterprise/invites"},
		{name: "list my invitations", method: http.MethodGet, path: "/api/v1/invitations"},
		{name: "accept invite", method: http.MethodPost, path: "/api/v1/invitations/inv_1/accept"},
		{name: "reject invite", method: http.MethodPost, path: "/api/v1/invitations/inv_1/reject"},
		{name: "list members", method: http.MethodGet, path: "/api/v1/enterprise/members"},
		{name: "update member role", method: http.MethodPut, path: "/api/v1/enterprise/members/user_2/role"},
		{name: "disable member", method: http.MethodPost, path: "/api/v1/enterprise/members/user_2/disable"},
		{name: "enable member", method: http.MethodPost, path: "/api/v1/enterprise/members/user_2/enable"},
		{name: "transfer owner", method: http.MethodPost, path: "/api/v1/enterprise/owner/transfer"},
		{name: "cancel dissolution", method: http.MethodPost, path: "/api/v1/enterprise/dissolution/cancel"},
		{name: "get dissolution", method: http.MethodGet, path: "/api/v1/enterprise/dissolution"},
		{name: "search users", method: http.MethodGet, path: "/api/v1/users/search"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			pipeline.Handler().ServeHTTP(response, request)
			assertUnauthenticated(t, response)
		})
	}
}

func TestIamRoutesForwardSuccessfully(t *testing.T) {
	pipeline, stub := withIamRoutes()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		check  func(t *testing.T, response *httptest.ResponseRecorder)
	}{
		{
			name:   "request deactivate",
			method: http.MethodPost,
			path:   "/api/v1/me/deactivate",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {
					if data["deadline_ms"] != "12345" {
						t.Fatalf("deadline_ms = %v, want 12345", data["deadline_ms"])
					}
				})
				if stub.requestDeactivateCalls != 1 {
					t.Fatalf("RequestDeactivate calls = %d, want 1", stub.requestDeactivateCalls)
				}
			},
		},
		{
			name:   "cancel deactivate",
			method: http.MethodPost,
			path:   "/api/v1/me/deactivate/cancel",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {})
				if stub.cancelDeactivateCalls != 1 {
					t.Fatalf("CancelDeactivate calls = %d, want 1", stub.cancelDeactivateCalls)
				}
			},
		},
		{
			name:   "get enterprise",
			method: http.MethodGet,
			path:   "/api/v1/enterprise",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {
					enterprise, ok := data["enterprise"].(map[string]any)
					if !ok || enterprise["id"] != "ent_1" || enterprise["name"] != "启航科技" {
						t.Fatalf("enterprise = %v", data["enterprise"])
					}
				})
				if stub.getEnterpriseCalls != 1 {
					t.Fatalf("GetEnterprise calls = %d, want 1", stub.getEnterpriseCalls)
				}
			},
		},
		{
			name:   "invite member",
			method: http.MethodPost,
			path:   "/api/v1/enterprise/invites",
			body:   `{"username_list":["alice","bob"]}`,
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {
					list, ok := data["invitation_list"].([]any)
					if !ok || len(list) != 1 {
						t.Fatalf("invitation_list = %v", data["invitation_list"])
					}
				})
				if stub.lastInviteMember == nil ||
					len(stub.lastInviteMember.GetUsernameList()) != 2 ||
					stub.lastInviteMember.GetUsernameList()[1] != "bob" {
					t.Fatalf("InviteMember request = %v", stub.lastInviteMember)
				}
			},
		},
		{
			name:   "cancel invite",
			method: http.MethodDelete,
			path:   "/api/v1/enterprise/invites/inv_1",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {})
				if stub.lastCancelInvite == nil || stub.lastCancelInvite.GetInviteId() != "inv_1" {
					t.Fatalf("CancelInvite request = %v", stub.lastCancelInvite)
				}
			},
		},
		{
			name:   "list invites",
			method: http.MethodGet,
			path:   "/api/v1/enterprise/invites?cursor=page&limit=2",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {
					if data["has_more"] != true || data["next_cursor"] != "next" {
						t.Fatalf("list invites data = %v", data)
					}
				})
				if stub.lastListInvites == nil ||
					stub.lastListInvites.GetCursor() != "page" ||
					stub.lastListInvites.GetLimit() != 2 {
					t.Fatalf("ListInvites request = %v", stub.lastListInvites)
				}
			},
		},
		{
			name:   "list my invitations",
			method: http.MethodGet,
			path:   "/api/v1/invitations",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {
					list, ok := data["invitation_list"].([]any)
					if !ok || len(list) != 1 {
						t.Fatalf("invitation_list = %v", data["invitation_list"])
					}
				})
			},
		},
		{
			name:   "accept invite",
			method: http.MethodPost,
			path:   "/api/v1/invitations/inv_1/accept",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {})
				if stub.lastAcceptInvite == nil || stub.lastAcceptInvite.GetInviteId() != "inv_1" {
					t.Fatalf("AcceptInvite request = %v", stub.lastAcceptInvite)
				}
			},
		},
		{
			name:   "reject invite",
			method: http.MethodPost,
			path:   "/api/v1/invitations/inv_1/reject",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {})
				if stub.lastRejectInvite == nil || stub.lastRejectInvite.GetInviteId() != "inv_1" {
					t.Fatalf("RejectInvite request = %v", stub.lastRejectInvite)
				}
			},
		},
		{
			name:   "list members",
			method: http.MethodGet,
			path:   "/api/v1/enterprise/members?q=ali&cursor=page&limit=3",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {
					list, ok := data["member_list"].([]any)
					if !ok || len(list) != 1 {
						t.Fatalf("member_list = %v", data["member_list"])
					}
				})
				if stub.lastListMembers == nil ||
					stub.lastListMembers.GetQ() != "ali" ||
					stub.lastListMembers.GetCursor() != "page" ||
					stub.lastListMembers.GetLimit() != 3 {
					t.Fatalf("ListMembers request = %v", stub.lastListMembers)
				}
			},
		},
		{
			name:   "update member role",
			method: http.MethodPut,
			path:   "/api/v1/enterprise/members/user_2/role",
			body:   `{"role":"manager"}`,
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {
					member, ok := data["member"].(map[string]any)
					if !ok || member["user_id"] != "user_2" {
						t.Fatalf("member = %v", data["member"])
					}
				})
				if stub.lastUpdateMemberRole == nil ||
					stub.lastUpdateMemberRole.GetUserId() != "user_2" ||
					stub.lastUpdateMemberRole.GetRole() != iamv1.MemberRole_MEMBER_ROLE_MANAGER {
					t.Fatalf("UpdateMemberRole request = %v", stub.lastUpdateMemberRole)
				}
			},
		},
		{
			name:   "disable member",
			method: http.MethodPost,
			path:   "/api/v1/enterprise/members/user_2/disable",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {})
				if stub.lastDisableMember == nil || stub.lastDisableMember.GetUserId() != "user_2" {
					t.Fatalf("DisableMember request = %v", stub.lastDisableMember)
				}
			},
		},
		{
			name:   "enable member",
			method: http.MethodPost,
			path:   "/api/v1/enterprise/members/user_2/enable",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {})
				if stub.lastEnableMember == nil || stub.lastEnableMember.GetUserId() != "user_2" {
					t.Fatalf("EnableMember request = %v", stub.lastEnableMember)
				}
			},
		},
		{
			name:   "transfer owner",
			method: http.MethodPost,
			path:   "/api/v1/enterprise/owner/transfer",
			body:   `{"successor_id":"user_2","confirm":true}`,
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {})
				if stub.lastTransferOwner == nil ||
					stub.lastTransferOwner.GetSuccessorId() != "user_2" ||
					!stub.lastTransferOwner.GetConfirm() {
					t.Fatalf("TransferOwner request = %v", stub.lastTransferOwner)
				}
			},
		},
		{
			name:   "cancel dissolution",
			method: http.MethodPost,
			path:   "/api/v1/enterprise/dissolution/cancel",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {})
				if stub.cancelDissolutionCalls != 1 {
					t.Fatalf("CancelEnterpriseDissolution calls = %d, want 1", stub.cancelDissolutionCalls)
				}
			},
		},
		{
			name:   "get dissolution status",
			method: http.MethodGet,
			path:   "/api/v1/enterprise/dissolution",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {
					if data["deadline_ms"] != "999" {
						t.Fatalf("deadline_ms = %v, want 999", data["deadline_ms"])
					}
				})
				if stub.getDissolutionCalls != 1 {
					t.Fatalf("GetEnterpriseDissolutionStatus calls = %d, want 1", stub.getDissolutionCalls)
				}
			},
		},
		{
			name:   "search users",
			method: http.MethodGet,
			path:   "/api/v1/users/search?q=ali&limit=5",
			check: func(t *testing.T, response *httptest.ResponseRecorder) {
				assertNotifySuccess(t, response, func(data map[string]any) {
					list, ok := data["user_list"].([]any)
					if !ok || len(list) != 1 {
						t.Fatalf("user_list = %v", data["user_list"])
					}
				})
				if stub.lastSearchUsers == nil ||
					stub.lastSearchUsers.GetQ() != "ali" ||
					stub.lastSearchUsers.GetLimit() != 5 {
					t.Fatalf("SearchUsers request = %v", stub.lastSearchUsers)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := iamGatewayRequest(test.method, test.path, test.body)
			response := httptest.NewRecorder()
			pipeline.Handler().ServeHTTP(response, request)
			test.check(t, response)
		})
	}
}

func TestIamRoutesMapPermissionDenied(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	stub := &iamEnterpriseGatewayStub{listMembersErr: businessRPCError(deal.CodePermissionDenied)}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterIamRoutes(pipeline, stub)

	request := iamGatewayRequest(http.MethodGet, "/api/v1/enterprise/members", "")
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != deal.CodePermissionDenied {
		t.Fatalf("code = %d, want 3001", envelope.Code)
	}
}

func TestIamRoutesRejectInvalidArguments(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	stub := &iamEnterpriseGatewayStub{}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterIamRoutes(pipeline, stub)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "invalid member role",
			method: http.MethodPut,
			path:   "/api/v1/enterprise/members/user_2/role",
			body:   `{"role":"superuser"}`,
		},
		{
			name:   "transfer owner missing successor",
			method: http.MethodPost,
			path:   "/api/v1/enterprise/owner/transfer",
			body:   `{"confirm":true}`,
		},
		{
			name:   "search users missing q",
			method: http.MethodGet,
			path:   "/api/v1/users/search",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := iamGatewayRequest(test.method, test.path, test.body)
			response := httptest.NewRecorder()
			pipeline.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", response.Code, response.Body.String())
			}
			var envelope struct {
				Code int `json:"code"`
			}
			if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if envelope.Code != deal.CodeInvalidArgument {
				t.Fatalf("code = %d, want 1001", envelope.Code)
			}
		})
	}
	if stub.updateMemberRoleCalls != 0 || stub.transferOwnerCalls != 0 || stub.searchUsersCalls != 0 {
		t.Fatalf("invalid arguments must not reach RPC: update=%d transfer=%d search=%d",
			stub.updateMemberRoleCalls, stub.transferOwnerCalls, stub.searchUsersCalls)
	}
}

func iamGatewayRequest(method, path, body string) *http.Request {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func assertUnauthenticated(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != deal.CodeUnauthenticated {
		t.Fatalf("code = %d, want 2001", envelope.Code)
	}
}
