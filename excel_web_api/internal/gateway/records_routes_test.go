package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/metadata"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	sharev1 "excel/api/share/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type recordsGatewayStub struct {
	queryCalls      int
	getCalls        int
	historyCalls    int
	lastQuery       *recordsv1.QueryViewRequest
	lastGet         *recordsv1.GetRecordRequest
	lastHistory     *recordsv1.GetHistoryRequest
	rowScopes       []string
	queryResponse   *recordsv1.QueryViewResponse
	getResponse     *recordsv1.GetRecordResponse
	historyResponse *recordsv1.GetHistoryResponse
	queryErr        error
}

func (stub *recordsGatewayStub) QueryView(
	ctx context.Context,
	request *recordsv1.QueryViewRequest,
) (*recordsv1.QueryViewResponse, error) {
	stub.queryCalls++
	stub.lastQuery = request
	stub.captureRowScope(ctx)
	return stub.queryResponse, stub.queryErr
}

func (stub *recordsGatewayStub) GetRecord(
	ctx context.Context,
	request *recordsv1.GetRecordRequest,
) (*recordsv1.GetRecordResponse, error) {
	stub.getCalls++
	stub.lastGet = request
	stub.captureRowScope(ctx)
	return stub.getResponse, nil
}

func (stub *recordsGatewayStub) GetHistory(
	ctx context.Context,
	request *recordsv1.GetHistoryRequest,
) (*recordsv1.GetHistoryResponse, error) {
	stub.historyCalls++
	stub.lastHistory = request
	stub.captureRowScope(ctx)
	return stub.historyResponse, nil
}

func (stub *recordsGatewayStub) captureRowScope(ctx context.Context) {
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	values := outgoing.Get("x-row-scope")
	if len(values) > 0 {
		stub.rowScopes = append(stub.rowScopes, values[0])
	}
}

var _ rpc.RecordsRPC = (*recordsGatewayStub)(nil)

func TestRecordsRoutesUseAuthPipelineEnvelopeAndOneBusinessRPC(t *testing.T) {
	iam := &pipelineIamRPC{
		exchangeResponse: allowedSession("user_1", "enterprise_1"),
	}
	share := &pipelineShareRPC{
		checkResponse: &sharev1.CheckAccessResponse{
			Allow:    true,
			Role:     sharev1.Role_ROLE_FILLER,
			Internal: true,
			RowScope: sharev1.RowScope_ROW_SCOPE_OWN,
		},
	}
	record := &recordsv1.Record{
		Id:      "record_1",
		TableId: "table_1",
		CellsMap: map[string]*commonv1.Value{
			"field_1": {
				Kind: &commonv1.Value_StringValue{StringValue: "visible"},
			},
		},
		CreatedBy: "user_1",
		Version:   3,
	}
	recordsClient := &recordsGatewayStub{
		queryResponse: &recordsv1.QueryViewResponse{
			RecordList: []*recordsv1.Record{record},
			HasMore:    true,
			NextCursor: "next-page",
		},
		getResponse: &recordsv1.GetRecordResponse{Record: record},
		historyResponse: &recordsv1.GetHistoryResponse{
			HistoryList: []*recordsv1.HistoryNode{{
				Id:       "history_1",
				RecordId: "record_1",
				FieldId:  "field_1",
			}},
		},
	}
	pipeline := NewPipeline(iam, share, nil, nil)
	RegisterRoutes(pipeline, recordsClient)
	server := httptest.NewServer(pipeline.Handler())
	defer server.Close()

	queryResponse := authenticatedRecordsRequest(
		t,
		server.URL+"/api/v1/bases/base_1/records?table_id=table_1&view_id=view_1&limit=2",
	)
	assertSuccessData(t, queryResponse, func(data map[string]any) {
		recordList, ok := data["record_list"].([]any)
		if !ok || len(recordList) != 1 || data["has_more"] != true || data["next_cursor"] != "next-page" {
			t.Fatalf("query data = %+v", data)
		}
	})
	if recordsClient.queryCalls != 1 ||
		recordsClient.lastQuery.GetBaseId() != "base_1" ||
		recordsClient.lastQuery.GetTableId() != "table_1" ||
		recordsClient.lastQuery.GetViewId() != "view_1" ||
		recordsClient.lastQuery.GetLimit() != 2 {
		t.Fatalf("QueryView calls=%d request=%v", recordsClient.queryCalls, recordsClient.lastQuery)
	}

	getResponse := authenticatedRecordsRequest(
		t,
		server.URL+"/api/v1/records/record_1",
	)
	assertSuccessData(t, getResponse, func(data map[string]any) {
		recordMap, ok := data["record"].(map[string]any)
		if !ok || recordMap["id"] != "record_1" {
			t.Fatalf("get data = %+v", data)
		}
	})
	if recordsClient.getCalls != 1 ||
		recordsClient.lastGet.GetRecordId() != "record_1" {
		t.Fatalf("GetRecord calls=%d request=%v", recordsClient.getCalls, recordsClient.lastGet)
	}

	historyResponse := authenticatedRecordsRequest(
		t,
		server.URL+"/api/v1/records/record_1/history?limit=1",
	)
	assertSuccessData(t, historyResponse, func(data map[string]any) {
		historyList, ok := data["history_list"].([]any)
		if !ok || len(historyList) != 1 {
			t.Fatalf("history data = %+v", data)
		}
	})
	if recordsClient.historyCalls != 1 ||
		recordsClient.lastHistory.GetRecordId() != "record_1" ||
		recordsClient.lastHistory.GetLimit() != 1 {
		t.Fatalf("GetHistory calls=%d request=%v", recordsClient.historyCalls, recordsClient.lastHistory)
	}
	if len(recordsClient.rowScopes) != 3 ||
		recordsClient.rowScopes[0] != "ROW_SCOPE_OWN" ||
		recordsClient.rowScopes[1] != "ROW_SCOPE_ALL" ||
		recordsClient.rowScopes[2] != "ROW_SCOPE_ALL" {
		t.Fatalf("x-row-scope values = %v", recordsClient.rowScopes)
	}
}

func TestRecordsQueryRejectsMalformedLimitWithoutRPC(t *testing.T) {
	iam := &pipelineIamRPC{
		exchangeResponse: allowedSession("user_1", "enterprise_1"),
	}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	recordsClient := &recordsGatewayStub{}
	pipeline := NewPipeline(iam, share, nil, nil)
	RegisterRoutes(pipeline, recordsClient)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/bases/base_1/records?table_id=table_1&limit=bad",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != http.StatusBadRequest || envelope.Code != deal.CodeInvalidArgument {
		t.Fatalf("response = HTTP %d code %d, want 400/1001", response.Code, envelope.Code)
	}
	if !strings.Contains(envelope.Message, "limit") {
		t.Fatalf("message = %q, want field-locating 1001 mentioning limit", envelope.Message)
	}
	if recordsClient.queryCalls != 0 {
		t.Fatalf("QueryView calls = %d, want 0", recordsClient.queryCalls)
	}
}

func TestRecordsQueryEmptyPageKeepsFullCursorContract(t *testing.T) {
	// H-48 空表（0 记录）也必须返回完整游标页：record_list=[]、has_more、next_cursor、
	// unsolved_comment_count_map（接口文档 §2.7 H-48 出参）；data 不得塌缩成 {}。
	iam := &pipelineIamRPC{
		exchangeResponse: allowedSession("user_1", "enterprise_1"),
	}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	recordsClient := &recordsGatewayStub{queryResponse: &recordsv1.QueryViewResponse{}}
	pipeline := NewPipeline(iam, share, nil, nil)
	RegisterRoutes(pipeline, recordsClient)
	server := httptest.NewServer(pipeline.Handler())
	defer server.Close()

	response := authenticatedRecordsRequest(
		t,
		server.URL+"/api/v1/bases/base_1/records?table_id=table_1",
	)
	assertSuccessData(t, response, func(data map[string]any) {
		recordList, ok := data["record_list"].([]any)
		if !ok || len(recordList) != 0 {
			t.Fatalf("empty page record_list = %v, want present []", data["record_list"])
		}
		hasMore, ok := data["has_more"].(bool)
		if !ok || hasMore {
			t.Fatalf("empty page has_more = %v, want present false", data["has_more"])
		}
		if _, ok := data["next_cursor"].(string); !ok {
			t.Fatalf("empty page next_cursor = %v, want present string", data["next_cursor"])
		}
		if _, ok := data["unsolved_comment_count_map"].(map[string]any); !ok {
			t.Fatalf(
				"empty page unsolved_comment_count_map = %v, want present {}",
				data["unsolved_comment_count_map"],
			)
		}
	})
}

func TestRecordsEntryShapeIdenticalWithAndWithoutViewId(t *testing.T) {
	// H-48 条目形状（票 01）：带 view_id 与不带的查询，条目字段集必须一致；
	// cells_map/version 恒在（空值以空 map/零值呈现，不得因 protojson 缺省省略而缺失）。
	iam := &pipelineIamRPC{
		exchangeResponse: allowedSession("user_1", "enterprise_1"),
	}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	recordsClient := &recordsGatewayStub{}
	pipeline := NewPipeline(iam, share, nil, nil)
	RegisterRoutes(pipeline, recordsClient)
	server := httptest.NewServer(pipeline.Handler())
	defer server.Close()

	entryFields := func(t *testing.T, target string) (map[string]struct{}, map[string]any) {
		t.Helper()
		recordsClient.queryResponse = &recordsv1.QueryViewResponse{
			RecordList: []*recordsv1.Record{{Id: "record_1", TableId: "table_1"}},
		}
		var fields map[string]struct{}
		var entry map[string]any
		response := authenticatedRecordsRequest(t, server.URL+target)
		assertSuccessData(t, response, func(data map[string]any) {
			recordList, ok := data["record_list"].([]any)
			if !ok || len(recordList) != 1 {
				t.Fatalf("record_list = %v, want 1 entry", data["record_list"])
			}
			entry, ok = recordList[0].(map[string]any)
			if !ok {
				t.Fatalf("entry = %v, want object", recordList[0])
			}
			fields = make(map[string]struct{}, len(entry))
			for name := range entry {
				fields[name] = struct{}{}
			}
		})
		return fields, entry
	}

	withoutView, withoutViewEntry := entryFields(
		t,
		"/api/v1/bases/base_1/records?table_id=table_1",
	)
	withView, withViewEntry := entryFields(
		t,
		"/api/v1/bases/base_1/records?table_id=table_1&view_id=view_1",
	)

	requiredFields := []string{"id", "table_id", "cells_map", "created_by", "created_at", "version"}
	for _, name := range requiredFields {
		if _, ok := withView[name]; !ok {
			t.Fatalf("view_id entry missing %q: fields=%v", name, withView)
		}
		if _, ok := withoutView[name]; !ok {
			t.Fatalf("entry missing %q: fields=%v", name, withoutView)
		}
	}
	if len(withView) != len(withoutView) {
		t.Fatalf(
			"entry field sets differ: with view_id %v, without view_id %v",
			withView,
			withoutView,
		)
	}
	if _, ok := withViewEntry["cells_map"].(map[string]any); !ok {
		t.Fatalf("view_id entry cells_map = %v, want present {}", withViewEntry["cells_map"])
	}
	if _, ok := withoutViewEntry["cells_map"].(map[string]any); !ok {
		t.Fatalf("entry cells_map = %v, want present {}", withoutViewEntry["cells_map"])
	}
}

func authenticatedRecordsRequest(t *testing.T, target string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("NewRequest(%s): %v", target, err)
	}
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	return response
}

func assertSuccessData(
	t *testing.T,
	response *http.Response,
	assert func(map[string]any),
) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var envelope struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 0 || envelope.Message != "ok" {
		t.Fatalf("envelope = %+v, want success", envelope)
	}
	assert(envelope.Data)
}
