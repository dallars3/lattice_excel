// gateway/records_routes.go：records 读域 HTTP 路由与行级范围元数据注入。
package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	recordsv1 "excel/api/records/v1"
	sharev1 "excel/api/share/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type recordsRoutes struct {
	client rpc.RecordsRPC
}

// RegisterRoutes binds the records read endpoints to a gateway pipeline.
func RegisterRoutes(pipeline *Pipeline, client rpc.RecordsRPC) {
	routes := &recordsRoutes{client: client}
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
		writeCodeError(response, deal.CodeInvalidArgument)
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
		writeCodeError(response, deal.CodeInvalidArgument)
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
