package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/metadata"

	auditv1 "excel/api/audit/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type auditGatewayStub struct {
	queryResponse *auditv1.QueryResponse
	queryErr      error
	exportStream  auditv1.AuditService_ExportCSVClient
	exportErr     error

	lastQuery  *auditv1.QueryRequest
	lastExport *auditv1.ExportCSVRequest
	queryCalls int
	exportCall int
}

func (stub *auditGatewayStub) Query(
	_ context.Context,
	request *auditv1.QueryRequest,
) (*auditv1.QueryResponse, error) {
	stub.queryCalls++
	stub.lastQuery = request
	return stub.queryResponse, stub.queryErr
}

func (stub *auditGatewayStub) ExportCSV(
	_ context.Context,
	request *auditv1.ExportCSVRequest,
) (auditv1.AuditService_ExportCSVClient, error) {
	stub.exportCall++
	stub.lastExport = request
	return stub.exportStream, stub.exportErr
}

var _ rpc.AuditRPC = (*auditGatewayStub)(nil)

type fakeAuditExportStream struct {
	header metadata.MD
	chunks [][]byte
	index  int
}

func (stream *fakeAuditExportStream) Header() (metadata.MD, error) { return stream.header, nil }
func (stream *fakeAuditExportStream) Trailer() metadata.MD         { return nil }
func (stream *fakeAuditExportStream) CloseSend() error             { return nil }
func (stream *fakeAuditExportStream) Context() context.Context     { return context.Background() }
func (stream *fakeAuditExportStream) SendMsg(any) error            { return nil }
func (stream *fakeAuditExportStream) RecvMsg(any) error            { return io.EOF }

func (stream *fakeAuditExportStream) Recv() (*auditv1.ExportCSVResponse, error) {
	if stream.index >= len(stream.chunks) {
		return nil, io.EOF
	}
	chunk := &auditv1.ExportCSVResponse{Chunk: stream.chunks[stream.index]}
	stream.index++
	return chunk, nil
}

func auditGatewayRequest(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	return request
}

func TestAuditRoutesRequireSession(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterAuditRoutes(pipeline, &auditGatewayStub{})

	for _, path := range []string{
		"/api/v1/enterprise/audits",
		"/api/v1/enterprise/audits/export.csv",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		pipeline.Handler().ServeHTTP(response, request)
		assertUnauthenticated(t, response)
	}
}

func TestAuditQueryForwardsAndReturnsEnvelope(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	stub := &auditGatewayStub{
		queryResponse: &auditv1.QueryResponse{
			AuditList:  []*auditv1.AuditEvent{{Id: "evt_1"}},
			HasMore:    true,
			NextCursor: "next",
		},
	}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterAuditRoutes(pipeline, stub)

	request := auditGatewayRequest(
		http.MethodGet,
		"/api/v1/enterprise/audits?operator=alice&from=1000&to=2000&cursor=page&limit=5",
	)
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	assertNotifySuccess(t, response, func(data map[string]any) {
		list, ok := data["audit_list"].([]any)
		if !ok || len(list) != 1 || data["has_more"] != true || data["next_cursor"] != "next" {
			t.Fatalf("audit data = %v", data)
		}
	})
	if stub.queryCalls != 1 ||
		stub.lastQuery.GetOperator() != "alice" ||
		stub.lastQuery.GetFromMs() != 1000 ||
		stub.lastQuery.GetToMs() != 2000 ||
		stub.lastQuery.GetCursor() != "page" ||
		stub.lastQuery.GetLimit() != 5 {
		t.Fatalf("Query request = %v", stub.lastQuery)
	}
}

func TestAuditExportStreamsCSVChunks(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	stub := &auditGatewayStub{
		exportStream: &fakeAuditExportStream{
			chunks: [][]byte{[]byte("a,b\n"), []byte("1,2\n")},
		},
	}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterAuditRoutes(pipeline, stub)

	request := auditGatewayRequest(
		http.MethodGet,
		"/api/v1/enterprise/audits/export.csv?operator=alice&from=1000&to=2000",
	)
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/csv" {
		t.Fatalf("Content-Type = %q, want text/csv", contentType)
	}
	if body := response.Body.String(); body != "a,b\n1,2\n" {
		t.Fatalf("body = %q, want concatenated chunks", body)
	}
	if stub.exportCall != 1 ||
		stub.lastExport.GetOperator() != "alice" ||
		stub.lastExport.GetFromMs() != 1000 ||
		stub.lastExport.GetToMs() != 2000 {
		t.Fatalf("ExportCSV request = %v", stub.lastExport)
	}
}

func TestAuditExportUsesAttachmentFilename(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	stub := &auditGatewayStub{
		exportStream: &fakeAuditExportStream{
			header: metadata.Pairs("x-file-name", "audits.csv"),
			chunks: [][]byte{[]byte("a,b\n")},
		},
	}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterAuditRoutes(pipeline, stub)

	request := auditGatewayRequest(http.MethodGet, "/api/v1/enterprise/audits/export.csv")
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if name := response.Header().Get("X-File-Name"); name != "audits.csv" {
		t.Fatalf("X-File-Name = %q, want audits.csv", name)
	}
}

func TestAuditQueryMapsPermissionDenied(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	stub := &auditGatewayStub{queryErr: businessRPCError(deal.CodePermissionDenied)}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterAuditRoutes(pipeline, stub)

	request := auditGatewayRequest(http.MethodGet, "/api/v1/enterprise/audits")
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

func TestAuditRejectsMalformedTimeRange(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	stub := &auditGatewayStub{}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterAuditRoutes(pipeline, stub)

	request := auditGatewayRequest(http.MethodGet, "/api/v1/enterprise/audits?from=abc")
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
	if envelope.Code != deal.CodeInvalidArgument || stub.queryCalls != 0 {
		t.Fatalf("code=%d queryCalls=%d, want 1001/0", envelope.Code, stub.queryCalls)
	}
}
