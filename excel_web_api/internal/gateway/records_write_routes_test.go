package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	sharev1 "excel/api/share/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type recordsWriteGatewayStub struct {
	recordsGatewayStub
	createCalls   int
	updateCalls   int
	batchCalls    int
	deleteCalls   int
	undoCalls     int
	rollbackCalls int
	lastCreate    *recordsv1.CreateRecordRequest
	lastUpdate    *recordsv1.UpdateCellRequest
	lastBatch     *recordsv1.BatchUpdateRequest
	lastDelete    *recordsv1.DeleteRecordsRequest
	lastUndo      *recordsv1.UndoDeleteRequest
	lastRollback  *recordsv1.RollbackRequest
	updateErr     error
}

func (stub *recordsWriteGatewayStub) CreateRecord(
	ctx context.Context,
	request *recordsv1.CreateRecordRequest,
) (*recordsv1.CreateRecordResponse, error) {
	stub.createCalls++
	stub.lastCreate = request
	stub.captureRowScope(ctx)
	return &recordsv1.CreateRecordResponse{Record: &recordsv1.Record{
		Id:      "record_created",
		TableId: request.GetTableId(),
		Version: 1,
	}}, nil
}

func (stub *recordsWriteGatewayStub) UpdateCell(
	ctx context.Context,
	request *recordsv1.UpdateCellRequest,
) (*recordsv1.UpdateCellResponse, error) {
	stub.updateCalls++
	stub.lastUpdate = request
	stub.captureRowScope(ctx)
	if stub.updateErr != nil {
		return nil, stub.updateErr
	}
	return &recordsv1.UpdateCellResponse{
		Value:   request.GetValue(),
		Version: request.GetExpectVersion() + 1,
	}, nil
}

func (stub *recordsWriteGatewayStub) BatchUpdate(
	ctx context.Context,
	request *recordsv1.BatchUpdateRequest,
) (*recordsv1.BatchUpdateResponse, error) {
	stub.batchCalls++
	stub.lastBatch = request
	stub.captureRowScope(ctx)
	return &recordsv1.BatchUpdateResponse{
		ResultList: []*recordsv1.OpResult{{
			Index:    0,
			RecordId: "record_batch",
			Ok:       true,
			Version:  2,
		}},
	}, nil
}

func (stub *recordsWriteGatewayStub) DeleteRecords(
	ctx context.Context,
	request *recordsv1.DeleteRecordsRequest,
) (*recordsv1.DeleteRecordsResponse, error) {
	stub.deleteCalls++
	stub.lastDelete = request
	stub.captureRowScope(ctx)
	return &recordsv1.DeleteRecordsResponse{
		DeletedCount: int32(len(request.GetRecordIdList())),
	}, nil
}

func (stub *recordsWriteGatewayStub) UndoDelete(
	ctx context.Context,
	request *recordsv1.UndoDeleteRequest,
) (*recordsv1.UndoDeleteResponse, error) {
	stub.undoCalls++
	stub.lastUndo = request
	stub.captureRowScope(ctx)
	return &recordsv1.UndoDeleteResponse{Record: &recordsv1.Record{
		Id:      request.GetRecordId(),
		Version: 3,
	}}, nil
}

func (stub *recordsWriteGatewayStub) Rollback(
	ctx context.Context,
	request *recordsv1.RollbackRequest,
) (*recordsv1.RollbackResponse, error) {
	stub.rollbackCalls++
	stub.lastRollback = request
	stub.captureRowScope(ctx)
	return &recordsv1.RollbackResponse{Record: &recordsv1.Record{
		Id:      "record_rollback",
		Version: 4,
	}}, nil
}

var _ rpc.RecordsRPC = (*recordsWriteGatewayStub)(nil)

func TestRecordsWriteRoutesForwardBodyAndInjectRowScope(t *testing.T) {
	stub := &recordsWriteGatewayStub{}
	server := recordsWriteTestServer(t, stub)
	defer server.Close()

	create := recordsJSONRequest(
		t,
		http.MethodPost,
		server.URL+"/api/v1/bases/base_1/records",
		`{"table_id":"table_1","cells_map":{"field_1":7},"client_request_id":"req_create"}`,
	)
	assertHTTPCode(t, create, http.StatusOK, 0)
	if stub.createCalls != 1 ||
		stub.lastCreate.GetTableId() != "table_1" ||
		stub.lastCreate.GetCellsMap()["field_1"].GetIntValue() != 7 ||
		stub.lastCreate.GetClientRequestId() != "req_create" {
		t.Fatalf("CreateRecord calls=%d request=%v", stub.createCalls, stub.lastCreate)
	}

	update := recordsJSONRequest(
		t,
		http.MethodPut,
		server.URL+"/api/v1/records/record_1/cells/field_1?base_id=base_1",
		`{"value":8,"expect_version":2}`,
	)
	assertHTTPCode(t, update, http.StatusOK, 0)
	if stub.updateCalls != 1 ||
		stub.lastUpdate.GetRecordId() != "record_1" ||
		stub.lastUpdate.GetFieldId() != "field_1" ||
		stub.lastUpdate.GetValue().GetIntValue() != 8 ||
		stub.lastUpdate.GetExpectVersion() != 2 {
		t.Fatalf("UpdateCell calls=%d request=%v", stub.updateCalls, stub.lastUpdate)
	}

	batch := recordsJSONRequest(
		t,
		http.MethodPost,
		server.URL+"/api/v1/records/batch?base_id=base_1",
		`{"op_list":[{"record_id":"record_batch","field_id":"field_1","value":9,"expect_version":1}],"client_request_id":"req_batch"}`,
	)
	assertHTTPCode(t, batch, http.StatusOK, 0)
	if stub.batchCalls != 1 ||
		len(stub.lastBatch.GetOpList()) != 1 ||
		stub.lastBatch.GetOpList()[0].GetValue().GetIntValue() != 9 ||
		stub.lastBatch.GetClientRequestId() != "req_batch" {
		t.Fatalf("BatchUpdate calls=%d request=%v", stub.batchCalls, stub.lastBatch)
	}

	deleted := recordsJSONRequest(
		t,
		http.MethodDelete,
		server.URL+"/api/v1/records?base_id=base_1",
		`{"record_id_list":["record_1"],"client_request_id":"req_delete"}`,
	)
	assertHTTPCode(t, deleted, http.StatusOK, 0)
	if stub.deleteCalls != 1 ||
		len(stub.lastDelete.GetRecordIdList()) != 1 ||
		stub.lastDelete.GetClientRequestId() != "req_delete" {
		t.Fatalf("DeleteRecords calls=%d request=%v", stub.deleteCalls, stub.lastDelete)
	}

	undo := recordsJSONRequest(
		t,
		http.MethodPost,
		server.URL+"/api/v1/records/record_1/undo?base_id=base_1",
		``,
	)
	assertHTTPCode(t, undo, http.StatusOK, 0)
	if stub.undoCalls != 1 || stub.lastUndo.GetRecordId() != "record_1" {
		t.Fatalf("UndoDelete calls=%d request=%v", stub.undoCalls, stub.lastUndo)
	}

	rollback := recordsJSONRequest(
		t,
		http.MethodPost,
		server.URL+"/api/v1/records/record_rollback/history/history_1/rollback?base_id=base_1",
		``,
	)
	assertHTTPCode(t, rollback, http.StatusOK, 0)
	if stub.rollbackCalls != 1 || stub.lastRollback.GetNodeId() != "history_1" {
		t.Fatalf("Rollback calls=%d request=%v", stub.rollbackCalls, stub.lastRollback)
	}
	if len(stub.rowScopes) < 6 {
		t.Fatalf("x-row-scope calls = %v, want all write calls", stub.rowScopes)
	}
}

func TestRecordsWriteRoutesExposeConflictData(t *testing.T) {
	detailMap, err := structpb.NewStruct(map[string]any{
		"current_value":   99,
		"current_version": 8,
	})
	if err != nil {
		t.Fatalf("conflict detail: %v", err)
	}
	withDetails, err := status.New(codes.Aborted, "并发版本冲突").WithDetails(
		&commonv1.ErrorDetail{
			Code:      int32(deal.CodeConcurrentConflict),
			Message:   "并发版本冲突",
			DetailMap: detailMap,
		},
	)
	if err != nil {
		t.Fatalf("conflict status: %v", err)
	}
	stub := &recordsWriteGatewayStub{updateErr: withDetails.Err()}
	server := recordsWriteTestServer(t, stub)
	defer server.Close()

	response := recordsJSONRequest(
		t,
		http.MethodPut,
		server.URL+"/api/v1/records/record_1/cells/field_1?base_id=base_1",
		`{"value":100,"expect_version":7}`,
	)
	defer response.Body.Close()
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if response.StatusCode != http.StatusConflict ||
		envelope.Code != deal.CodeConcurrentConflict ||
		envelope.Data["current_value"] != float64(99) ||
		envelope.Data["current_version"] != float64(8) {
		t.Fatalf("conflict response = HTTP %d envelope %+v", response.StatusCode, envelope)
	}
}

func recordsWriteTestServer(
	t *testing.T,
	stub rpc.RecordsRPC,
) *httptest.Server {
	t.Helper()
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "enterprise_1")}
	share := &pipelineShareRPC{
		checkResponse: &sharev1.CheckAccessResponse{
			Allow:    true,
			Role:     sharev1.Role_ROLE_EDITOR,
			Internal: true,
			RowScope: sharev1.RowScope_ROW_SCOPE_ALL,
		},
	}
	pipeline := NewPipeline(iam, share, nil, nil)
	RegisterRoutes(pipeline, stub)
	return httptest.NewServer(pipeline.Handler())
}

func recordsJSONRequest(
	t *testing.T,
	method, target, body string,
) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, target, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("NewRequest(%s %s): %v", method, target, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	return response
}

func assertHTTPCode(
	t *testing.T,
	response *http.Response,
	wantStatus, wantCode int,
) {
	t.Helper()
	defer response.Body.Close()
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.StatusCode != wantStatus || envelope.Code != wantCode {
		t.Fatalf(
			"response = HTTP %d code %d, want HTTP %d code %d",
			response.StatusCode,
			envelope.Code,
			wantStatus,
			wantCode,
		)
	}
}
