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

	commentv1 "excel/api/comment/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type commentStub struct {
	listCalls    int
	createCalls  int
	replyCalls   int
	resolveCalls int
	reopenCalls  int
	deleteCalls  int

	lastList    *commentv1.ListCommentsRequest
	lastCreate  *commentv1.CreateCommentRequest
	lastReply   *commentv1.ReplyCommentRequest
	lastResolve *commentv1.ResolveCommentRequest
	lastReopen  *commentv1.ReopenCommentRequest
	lastDelete  *commentv1.DeleteCommentRequest

	err error
}

func (stub *commentStub) ListComments(
	_ context.Context,
	request *commentv1.ListCommentsRequest,
) (*commentv1.ListCommentsResponse, error) {
	stub.listCalls++
	stub.lastList = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &commentv1.ListCommentsResponse{
		CommentList: []*commentv1.Comment{{
			Id:       "comment_1",
			RecordId: request.GetRecordId(),
			Content:  "hello",
		}},
		UnsolvedCount: 1,
		HasMore:       true,
		NextCursor:    "next",
	}, nil
}

func (stub *commentStub) CreateComment(
	_ context.Context,
	request *commentv1.CreateCommentRequest,
) (*commentv1.CreateCommentResponse, error) {
	stub.createCalls++
	stub.lastCreate = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &commentv1.CreateCommentResponse{
		Comment: &commentv1.Comment{
			Id:        "comment_1",
			RecordId:  request.GetRecordId(),
			Content:   request.GetContent(),
			Author:    "user_1",
			CreatedAt: 1,
		},
	}, nil
}

func (stub *commentStub) ReplyComment(
	_ context.Context,
	request *commentv1.ReplyCommentRequest,
) (*commentv1.ReplyCommentResponse, error) {
	stub.replyCalls++
	stub.lastReply = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &commentv1.ReplyCommentResponse{
		Comment: &commentv1.Comment{
			Id:       "comment_2",
			ParentId: request.GetCommentId(),
			Content:  request.GetContent(),
		},
	}, nil
}

func (stub *commentStub) ResolveComment(
	_ context.Context,
	request *commentv1.ResolveCommentRequest,
) (*commentv1.ResolveCommentResponse, error) {
	stub.resolveCalls++
	stub.lastResolve = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &commentv1.ResolveCommentResponse{
		Comment: &commentv1.Comment{Id: request.GetCommentId()},
	}, nil
}

func (stub *commentStub) ReopenComment(
	_ context.Context,
	request *commentv1.ReopenCommentRequest,
) (*commentv1.ReopenCommentResponse, error) {
	stub.reopenCalls++
	stub.lastReopen = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &commentv1.ReopenCommentResponse{
		Comment: &commentv1.Comment{Id: request.GetCommentId()},
	}, nil
}

func (stub *commentStub) DeleteComment(
	_ context.Context,
	request *commentv1.DeleteCommentRequest,
) (*emptypb.Empty, error) {
	stub.deleteCalls++
	stub.lastDelete = request
	if stub.err != nil {
		return nil, stub.err
	}
	return &emptypb.Empty{}, nil
}

var _ rpc.CommentRPC = (*commentStub)(nil)

func TestCommentRoutesRequireSession(t *testing.T) {
	iam := &pipelineIamRPC{}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterCommentRoutes(pipeline, &commentStub{})

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/records/record_1/comments"},
		{http.MethodPost, "/api/v1/records/record_1/comments"},
		{http.MethodPost, "/api/v1/comments/comment_1/replies"},
		{http.MethodPost, "/api/v1/comments/comment_1/resolve"},
		{http.MethodPost, "/api/v1/comments/comment_1/reopen"},
		{http.MethodDelete, "/api/v1/comments/comment_1"},
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

func TestCommentRoutesForwardRequests(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "e1")}
	client := &commentStub{}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterCommentRoutes(pipeline, client)

	list := performGatewayRequest(
		t,
		pipeline,
		http.MethodGet,
		"/api/v1/records/record_1/comments?cursor=cur&limit=5",
		"",
	)
	assertGatewaySuccess(t, list, func(data map[string]any) {
		if _, ok := data["comment_list"].([]any); !ok {
			t.Fatalf("list data = %+v", data)
		}
	})
	if client.listCalls != 1 ||
		client.lastList.GetRecordId() != "record_1" ||
		client.lastList.GetCursor() != "cur" ||
		client.lastList.GetLimit() != 5 {
		t.Fatalf("ListComments calls=%d request=%v", client.listCalls, client.lastList)
	}

	create := performGatewayRequest(
		t,
		pipeline,
		http.MethodPost,
		"/api/v1/records/record_1/comments",
		`{"content":"hello","mention_list":["u1","u2"]}`,
	)
	assertGatewaySuccess(t, create, func(data map[string]any) {
		if _, ok := data["comment"].(map[string]any); !ok {
			t.Fatalf("create data = %+v", data)
		}
	})
	if client.createCalls != 1 ||
		client.lastCreate.GetRecordId() != "record_1" ||
		client.lastCreate.GetContent() != "hello" ||
		len(client.lastCreate.GetMentionList()) != 2 {
		t.Fatalf("CreateComment calls=%d request=%v", client.createCalls, client.lastCreate)
	}

	reply := performGatewayRequest(
		t,
		pipeline,
		http.MethodPost,
		"/api/v1/comments/comment_1/replies",
		`{"content":"reply","mention_list":["u3"]}`,
	)
	assertGatewaySuccess(t, reply, func(data map[string]any) {
		if _, ok := data["comment"].(map[string]any); !ok {
			t.Fatalf("reply data = %+v", data)
		}
	})
	if client.replyCalls != 1 ||
		client.lastReply.GetCommentId() != "comment_1" ||
		client.lastReply.GetContent() != "reply" {
		t.Fatalf("ReplyComment calls=%d request=%v", client.replyCalls, client.lastReply)
	}

	resolve := performGatewayRequest(
		t,
		pipeline,
		http.MethodPost,
		"/api/v1/comments/comment_1/resolve",
		"",
	)
	assertGatewaySuccess(t, resolve, func(map[string]any) {})
	if client.resolveCalls != 1 || client.lastResolve.GetCommentId() != "comment_1" {
		t.Fatalf("ResolveComment calls=%d request=%v", client.resolveCalls, client.lastResolve)
	}

	reopen := performGatewayRequest(
		t,
		pipeline,
		http.MethodPost,
		"/api/v1/comments/comment_1/reopen",
		"",
	)
	assertGatewaySuccess(t, reopen, func(map[string]any) {})
	if client.reopenCalls != 1 || client.lastReopen.GetCommentId() != "comment_1" {
		t.Fatalf("ReopenComment calls=%d request=%v", client.reopenCalls, client.lastReopen)
	}

	del := performGatewayRequest(
		t,
		pipeline,
		http.MethodDelete,
		"/api/v1/comments/comment_1",
		"",
	)
	assertGatewaySuccess(t, del, func(map[string]any) {})
	if client.deleteCalls != 1 || client.lastDelete.GetCommentId() != "comment_1" {
		t.Fatalf("DeleteComment calls=%d request=%v", client.deleteCalls, client.lastDelete)
	}
}

func TestCommentRouteMapsBusinessError(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "e1")}
	client := &commentStub{err: status.Error(codes.PermissionDenied, "no comment access")}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterCommentRoutes(pipeline, client)

	response := performGatewayRequest(
		t,
		pipeline,
		http.MethodGet,
		"/api/v1/records/record_1/comments",
		"",
	)
	assertCodeEnvelope(t, response, http.StatusForbidden, deal.CodePermissionDenied)
}

func performGatewayRequest(
	t *testing.T,
	pipeline *Pipeline,
	method, path, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)
	return response
}

func assertGatewaySuccess(
	t *testing.T,
	response *httptest.ResponseRecorder,
	assert func(map[string]any),
) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 0 {
		t.Fatalf("envelope = %+v, want success", envelope)
	}
	assert(envelope.Data)
}

func assertCodeEnvelope(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantStatus int,
	wantCode int,
) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("status = %d body=%s, want %d", response.Code, response.Body.String(), wantStatus)
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != wantCode {
		t.Fatalf("code = %d, want %d", envelope.Code, wantCode)
	}
}
