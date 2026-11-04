// gateway/records_routes.go：records 读域 HTTP 路由与行级范围元数据注入。
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	sharev1 "excel/api/share/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type recordsRoutes struct {
	client      rpc.RecordsRPC
	writeClient rpc.RecordsWriteRPC
	pipeline    *Pipeline
}

// RegisterRoutes binds the records read and supported write endpoints to a gateway pipeline.
func RegisterRoutes(pipeline *Pipeline, client rpc.RecordsRPC) {
	routes := &recordsRoutes{client: client, pipeline: pipeline}
	pipeline.BaseAuth(
		http.MethodGet,
		"/api/v1/bases/{base_id}/records",
		sharev1.Action_ACTION_VIEW.String(),
		http.HandlerFunc(routes.queryView),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/records/{record_id}",
		http.HandlerFunc(routes.getRecord),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/records/{record_id}/history",
		http.HandlerFunc(routes.getHistory),
	)
	if writeClient, ok := client.(rpc.RecordsWriteRPC); ok {
		routes.writeClient = writeClient
		routes.registerWriteRoutes()
	}
}

func (routes *recordsRoutes) registerWriteRoutes() {
	routes.pipeline.BaseAuth(
		http.MethodPost,
		"/api/v1/bases/{base_id}/records",
		sharev1.Action_ACTION_CREATE_RECORD.String(),
		http.HandlerFunc(routes.createRecord),
	)
	routes.pipeline.SessionRequired(
		http.MethodPut,
		"/api/v1/records/{record_id}/cells/{field_id}",
		http.HandlerFunc(routes.updateCell),
	)
	routes.pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/records/batch",
		http.HandlerFunc(routes.batchUpdate),
	)
	routes.pipeline.SessionRequired(
		http.MethodDelete,
		"/api/v1/records",
		http.HandlerFunc(routes.deleteRecords),
	)
	routes.pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/records/{record_id}/undo",
		http.HandlerFunc(routes.undoDelete),
	)
	routes.pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/records/{record_id}/history/{node_id}/rollback",
		http.HandlerFunc(routes.rollback),
	)
}

type createRecordPayload struct {
	TableID         string                     `json:"table_id"`
	CellsMap        map[string]json.RawMessage `json:"cells_map"`
	ClientRequestID string                     `json:"client_request_id"`
}

func (routes *recordsRoutes) createRecord(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload createRecordPayload
	if err := decodeJSONBody(response, request, &payload); err != nil ||
		strings.TrimSpace(payload.TableID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	cells, err := decodeCellsMap(payload.CellsMap)
	if err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	ctx, ok := recordsAuthorizedContext(request)
	if !ok {
		writeCodeError(response, deal.CodePermissionDenied)
		return
	}
	result, err := routes.writeClient.CreateRecord(ctx, &recordsv1.CreateRecordRequest{
		TableId:         payload.TableID,
		CellsMap:        cells,
		ClientRequestId: payload.ClientRequestID,
	})
	if err != nil {
		writeRecordsBusinessError(response, err)
		return
	}
	writeRecordsResult(response, result)
}

type updateCellPayload struct {
	BaseID        string          `json:"base_id"`
	Value         json.RawMessage `json:"value"`
	ExpectVersion int64           `json:"expect_version"`
}

func (routes *recordsRoutes) updateCell(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload updateCellPayload
	if err := decodeJSONBody(response, request, &payload); err != nil ||
		payload.Value == nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	value, err := decodeCellValue(payload.Value)
	if err != nil {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	baseID := recordsRequestBaseID(request, payload.BaseID)
	ctx, ok := routes.authorizedWriteContext(
		response,
		request,
		baseID,
		sharev1.Action_ACTION_EDIT_ALL,
	)
	if !ok {
		return
	}
	result, err := routes.writeClient.UpdateCell(ctx, &recordsv1.UpdateCellRequest{
		RecordId:      request.PathValue("record_id"),
		FieldId:       request.PathValue("field_id"),
		Value:         value,
		ExpectVersion: payload.ExpectVersion,
	})
	if err != nil {
		writeRecordsBusinessError(response, err)
		return
	}
	writeRecordsResult(response, result)
}

type batchUpdatePayload struct {
	BaseID          string                  `json:"base_id"`
	OpList          []batchOperationPayload `json:"op_list"`
	ClientRequestID string                  `json:"client_request_id"`
}

type batchOperationPayload struct {
	RecordID      string          `json:"record_id"`
	FieldID       string          `json:"field_id"`
	Value         json.RawMessage `json:"value"`
	ExpectVersion int64           `json:"expect_version"`
}

func (routes *recordsRoutes) batchUpdate(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload batchUpdatePayload
	if err := decodeJSONBody(response, request, &payload); err != nil ||
		len(payload.OpList) == 0 {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	operations := make([]*recordsv1.BatchOperation, 0, len(payload.OpList))
	for _, operation := range payload.OpList {
		if operation.Value == nil {
			writeCodeError(response, deal.CodeInvalidArgument)
			return
		}
		value, err := decodeCellValue(operation.Value)
		if err != nil {
			writeCodeError(response, deal.CodeInvalidArgument)
			return
		}
		operations = append(operations, &recordsv1.BatchOperation{
			RecordId:      operation.RecordID,
			FieldId:       operation.FieldID,
			Value:         value,
			ExpectVersion: operation.ExpectVersion,
		})
	}
	ctx, ok := routes.authorizedWriteContext(
		response,
		request,
		recordsRequestBaseID(request, payload.BaseID),
		sharev1.Action_ACTION_EDIT_ALL,
	)
	if !ok {
		return
	}
	result, err := routes.writeClient.BatchUpdate(ctx, &recordsv1.BatchUpdateRequest{
		OpList:          operations,
		ClientRequestId: payload.ClientRequestID,
	})
	if err != nil {
		writeRecordsBusinessError(response, err)
		return
	}
	writeRecordsResult(response, result)
}

type deleteRecordsPayload struct {
	BaseID          string   `json:"base_id"`
	RecordIDList    []string `json:"record_id_list"`
	ClientRequestID string   `json:"client_request_id"`
}

func (routes *recordsRoutes) deleteRecords(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload deleteRecordsPayload
	if err := decodeJSONBody(response, request, &payload); err != nil ||
		len(payload.RecordIDList) == 0 {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	ctx, ok := routes.authorizedWriteContext(
		response,
		request,
		recordsRequestBaseID(request, payload.BaseID),
		sharev1.Action_ACTION_DELETE_RECORD,
	)
	if !ok {
		return
	}
	result, err := routes.writeClient.DeleteRecords(ctx, &recordsv1.DeleteRecordsRequest{
		RecordIdList:    payload.RecordIDList,
		ClientRequestId: payload.ClientRequestID,
	})
	if err != nil {
		writeRecordsBusinessError(response, err)
		return
	}
	writeRecordsResult(response, result)
}

func (routes *recordsRoutes) undoDelete(
	response http.ResponseWriter,
	request *http.Request,
) {
	ctx, ok := routes.authorizedWriteContext(
		response,
		request,
		recordsRequestBaseID(request, ""),
		sharev1.Action_ACTION_EDIT_ALL,
	)
	if !ok {
		return
	}
	result, err := routes.writeClient.UndoDelete(ctx, &recordsv1.UndoDeleteRequest{
		RecordId: request.PathValue("record_id"),
	})
	if err != nil {
		writeRecordsBusinessError(response, err)
		return
	}
	writeRecordsResult(response, result)
}

func (routes *recordsRoutes) rollback(
	response http.ResponseWriter,
	request *http.Request,
) {
	ctx, ok := routes.authorizedWriteContext(
		response,
		request,
		recordsRequestBaseID(request, ""),
		sharev1.Action_ACTION_EDIT_ALL,
	)
	if !ok {
		return
	}
	result, err := routes.writeClient.Rollback(ctx, &recordsv1.RollbackRequest{
		NodeId: request.PathValue("node_id"),
	})
	if err != nil {
		writeRecordsBusinessError(response, err)
		return
	}
	writeRecordsResult(response, result)
}

func recordsRequestBaseID(request *http.Request, bodyBaseID string) string {
	for _, candidate := range []string{
		request.URL.Query().Get("base_id"),
		request.Header.Get("X-Base-Id"),
		bodyBaseID,
	} {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	if principal, ok := PrincipalFromContext(request.Context()); ok {
		return strings.TrimSpace(principal.GetLinkContext().GetBaseId())
	}
	return ""
}

func (routes *recordsRoutes) authorizedWriteContext(
	response http.ResponseWriter,
	request *http.Request,
	baseID string,
	action sharev1.Action,
) (context.Context, bool) {
	baseID = strings.TrimSpace(baseID)
	if baseID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return nil, false
	}
	principal, ok := PrincipalFromContext(request.Context())
	if !ok {
		writeCodeError(response, deal.CodeUnauthenticated)
		return nil, false
	}
	actions := []sharev1.Action{action}
	if action == sharev1.Action_ACTION_EDIT_ALL {
		actions = append(actions, sharev1.Action_ACTION_EDIT_OWN)
	}
	var decision *sharev1.CheckAccessResponse
	var checkedAction sharev1.Action
	for _, candidate := range actions {
		checked, err := routes.pipeline.checkAccess(
			request.Context(),
			principal,
			baseID,
			candidate,
		)
		if err != nil {
			writeBusinessError(response, err)
			return nil, false
		}
		if checked != nil && checked.GetAllow() {
			decision = checked
			checkedAction = candidate
			break
		}
	}
	if decision == nil {
		writeCodeError(response, deal.CodePermissionDenied)
		return nil, false
	}
	scope := decision.GetRowScope()
	if scope != sharev1.RowScope_ROW_SCOPE_ALL &&
		scope != sharev1.RowScope_ROW_SCOPE_OWN {
		writeCodeError(response, deal.CodePermissionDenied)
		return nil, false
	}
	authorization := AuthorizationContext{
		BaseID:    baseID,
		Action:    checkedAction,
		Principal: principal,
		Decision:  decision,
	}
	ctx := context.WithValue(
		request.Context(),
		authorizationContextKey,
		authorization,
	)
	ctx = metadata.AppendToOutgoingContext(
		ctx,
		"x-row-scope",
		scope.String(),
		"x-base-id",
		baseID,
	)
	return ctx, true
}

func decodeCellsMap(
	rawCells map[string]json.RawMessage,
) (map[string]*commonv1.Value, error) {
	cells := make(map[string]*commonv1.Value, len(rawCells))
	for fieldID, raw := range rawCells {
		if strings.TrimSpace(fieldID) == "" {
			return nil, errors.New("cell field_id is required")
		}
		value, err := decodeCellValue(raw)
		if err != nil {
			return nil, err
		}
		if value != nil {
			cells[fieldID] = value
		}
	}
	return cells, nil
}

func decodeCellValue(raw json.RawMessage) (*commonv1.Value, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return jsonAnyToCell(decoded)
}

func jsonAnyToCell(value any) (*commonv1.Value, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return &commonv1.Value{
			Kind: &commonv1.Value_StringValue{StringValue: typed},
		}, nil
	case bool:
		return &commonv1.Value{
			Kind: &commonv1.Value_BoolValue{BoolValue: typed},
		}, nil
	case json.Number:
		if integer, err := strconv.ParseInt(typed.String(), 10, 64); err == nil {
			return &commonv1.Value{
				Kind: &commonv1.Value_IntValue{IntValue: integer},
			}, nil
		}
		number, err := strconv.ParseFloat(typed.String(), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, errors.New("cell number is invalid")
		}
		return &commonv1.Value{
			Kind: &commonv1.Value_DoubleValue{DoubleValue: number},
		}, nil
	case []any:
		items := make([]*commonv1.Value, 0, len(typed))
		for _, item := range typed {
			converted, err := jsonAnyToCell(item)
			if err != nil {
				return nil, err
			}
			if converted == nil {
				converted = &commonv1.Value{}
			}
			items = append(items, converted)
		}
		return &commonv1.Value{
			Kind: &commonv1.Value_ListValue{
				ListValue: &commonv1.ValueList{ValueList: items},
			},
		}, nil
	case map[string]any:
		payload, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		return &commonv1.Value{
			Kind: &commonv1.Value_StringValue{StringValue: string(payload)},
		}, nil
	default:
		return nil, errors.New("unsupported cell value")
	}
}

func writeRecordsBusinessError(response http.ResponseWriter, err error) {
	grpcStatus := status.Convert(err)
	code := grpcBusinessCode(err)
	spec, ok := deal.LookupError(code)
	if !ok {
		writeBusinessError(response, err)
		return
	}
	for _, detail := range grpcStatus.Details() {
		if errorDetail, ok := detail.(*commonv1.ErrorDetail); ok &&
			errorDetail.GetDetailMap() != nil {
			writeEnvelope(response, spec.HTTPStatus, responseEnvelope{
				Code:    code,
				Message: spec.Message,
				Data:    errorDetail.GetDetailMap().AsMap(),
			})
			return
		}
	}
	writeBusinessError(response, err)
}

func (routes *recordsRoutes) queryView(
	response http.ResponseWriter,
	request *http.Request,
) {
	tableID := firstQueryValue(request, "table_id", "table")
	if strings.TrimSpace(tableID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	limit, ok := recordsQueryLimit(request)
	if !ok {
		writeFieldError(response, "limit")
		return
	}
	ctx, ok := recordsAuthorizedContext(request)
	if !ok {
		writeCodeError(response, deal.CodePermissionDenied)
		return
	}
	result, err := routes.client.QueryView(ctx, &recordsv1.QueryViewRequest{
		BaseId:  request.PathValue("base_id"),
		TableId: tableID,
		ViewId:  firstQueryValue(request, "view_id", "view"),
		Cursor:  request.URL.Query().Get("cursor"),
		Limit:   limit,
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeRecordsResult(response, result)
}

func (routes *recordsRoutes) getRecord(
	response http.ResponseWriter,
	request *http.Request,
) {
	recordID := request.PathValue("record_id")
	if strings.TrimSpace(recordID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	ctx, ok := recordsSessionContext(request)
	if !ok {
		writeCodeError(response, deal.CodePermissionDenied)
		return
	}
	result, err := routes.client.GetRecord(
		ctx,
		&recordsv1.GetRecordRequest{RecordId: recordID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeRecordsResult(response, result)
}

func (routes *recordsRoutes) getHistory(
	response http.ResponseWriter,
	request *http.Request,
) {
	recordID := request.PathValue("record_id")
	if strings.TrimSpace(recordID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	limit, ok := recordsQueryLimit(request)
	if !ok {
		writeFieldError(response, "limit")
		return
	}
	ctx, ok := recordsSessionContext(request)
	if !ok {
		writeCodeError(response, deal.CodePermissionDenied)
		return
	}
	result, err := routes.client.GetHistory(ctx, &recordsv1.GetHistoryRequest{
		RecordId: recordID,
		Cursor:   request.URL.Query().Get("cursor"),
		Limit:    limit,
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeRecordsResult(response, result)
}

func recordsAuthorizedContext(request *http.Request) (context.Context, bool) {
	authorization, ok := AuthorizationFromContext(request.Context())
	if !ok || authorization.Decision == nil {
		return nil, false
	}
	scope := authorization.Decision.GetRowScope()
	if scope != sharev1.RowScope_ROW_SCOPE_ALL &&
		scope != sharev1.RowScope_ROW_SCOPE_OWN {
		return nil, false
	}
	return metadata.AppendToOutgoingContext(
		request.Context(),
		"x-row-scope",
		scope.String(),
		"x-base-id",
		authorization.BaseID,
	), true
}

func recordsSessionContext(request *http.Request) (context.Context, bool) {
	principal, ok := PrincipalFromContext(request.Context())
	if !ok {
		return nil, false
	}
	scope := sharev1.RowScope_ROW_SCOPE_ALL
	role := strings.TrimPrefix(
		strings.ToLower(strings.TrimSpace(principal.GetLinkContext().GetRole())),
		"role_",
	)
	if role == "filler" {
		scope = sharev1.RowScope_ROW_SCOPE_OWN
	}
	return metadata.AppendToOutgoingContext(
		request.Context(),
		"x-row-scope",
		scope.String(),
	), true
}

func recordsQueryLimit(request *http.Request) (int32, bool) {
	raw := strings.TrimSpace(request.URL.Query().Get("limit"))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value <= 0 {
		return 0, false
	}
	if value > 100 {
		value = 100
	}
	return int32(value), true
}

func firstQueryValue(request *http.Request, keys ...string) string {
	for _, key := range keys {
		if value := request.URL.Query().Get(key); value != "" {
			return value
		}
	}
	return ""
}

func writeRecordsResult(response http.ResponseWriter, message proto.Message) {
	payload, err := protoJSON(message)
	if err != nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	writeSuccess(response, json.RawMessage(payload))
}
