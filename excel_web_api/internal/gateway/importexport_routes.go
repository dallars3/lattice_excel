// gateway/importexport_routes.go：H-82 ~ H-86 与 H-93 导入导出路由。
package gateway

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	importexportv1 "excel/api/importexport/v1"
	sharev1 "excel/api/share/v1"
	structurev1 "excel/api/structure/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type importExportRoutes struct {
	pipeline *Pipeline
	client   rpc.ImportExportRPC
}

// RegisterImportExportRoutes binds preview/import/status/report/export/download.
func RegisterImportExportRoutes(pipeline *Pipeline, client rpc.ImportExportRPC) {
	if pipeline == nil {
		panic("importexport routes pipeline is required")
	}
	routes := &importExportRoutes{pipeline: pipeline, client: client}
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/imports/preview",
		http.HandlerFunc(routes.preview),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/imports",
		http.HandlerFunc(routes.createImport),
	)
	pipeline.BaseAuth(
		http.MethodPost,
		"/api/v1/bases/{base_id}/imports",
		sharev1.Action_ACTION_IMPORT.String(),
		http.HandlerFunc(routes.createImport),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/imports/{task_id}",
		http.HandlerFunc(routes.getImportStatus),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/imports/{task_id}/report",
		http.HandlerFunc(routes.getImportReport),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/exports",
		http.HandlerFunc(routes.createExport),
	)
	pipeline.BaseAuth(
		http.MethodPost,
		"/api/v1/bases/{base_id}/exports",
		sharev1.Action_ACTION_EXPORT.String(),
		http.HandlerFunc(routes.createExport),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/exports/{task_id}/download",
		http.HandlerFunc(routes.exportStream),
	)
}

func (routes *importExportRoutes) preview(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	var body struct {
		FileID     string `json:"file_id"`
		SheetIndex int32  `json:"sheet_index"`
		BaseID     string `json:"base_id"`
		TableID    string `json:"table_id"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.FileID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	if strings.TrimSpace(body.BaseID) != "" &&
		!routes.authorizeImport(response, request, body.BaseID) {
		return
	}
	result, err := routes.client.PreviewImport(
		importWorkerContext(request, body.BaseID, body.TableID),
		&importexportv1.PreviewImportRequest{
			FileId:     strings.TrimSpace(body.FileID),
			SheetIndex: body.SheetIndex,
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *importExportRoutes) createImport(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	var body struct {
		FileID         string `json:"file_id"`
		SheetIndex     int32  `json:"sheet_index"`
		FirstRowHeader bool   `json:"first_row_header"`
		BaseID         string `json:"base_id"`
		TableID        string `json:"table_id"`
		ClientRequest  string `json:"client_request_id"`
		ColumnConfigs  []struct {
			Index  int32  `json:"index"`
			Name   string `json:"name"`
			Type   string `json:"type"`
			Ignore bool   `json:"ignore"`
		} `json:"column_config_list"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.FileID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	if authorization, ok := AuthorizationFromContext(request.Context()); ok {
		body.BaseID = authorization.BaseID
	}
	if strings.TrimSpace(body.BaseID) == "" {
		principal, ok := PrincipalFromContext(request.Context())
		if !ok || !principal.GetInternal() {
			writeCodeError(response, deal.CodePermissionDenied)
			return
		}
	} else if !routes.authorizeImport(response, request, body.BaseID) {
		return
	}
	columnConfigs := make([]*importexportv1.ColumnConfig, 0, len(body.ColumnConfigs))
	for _, config := range body.ColumnConfigs {
		fieldType, ok := parseFieldType(config.Type)
		if !ok || config.Index < 0 {
			writeCodeError(response, deal.CodeInvalidArgument)
			return
		}
		columnConfigs = append(columnConfigs, &importexportv1.ColumnConfig{
			Index:  config.Index,
			Name:   config.Name,
			Type:   fieldType,
			Ignore: config.Ignore,
		})
	}
	result, err := routes.client.CreateImport(
		importWorkerContext(request, body.BaseID, body.TableID),
		&importexportv1.CreateImportRequest{
			FileId:           strings.TrimSpace(body.FileID),
			SheetIndex:       body.SheetIndex,
			FirstRowHeader:   body.FirstRowHeader,
			ColumnConfigList: columnConfigs,
			ClientRequestId:  strings.TrimSpace(body.ClientRequest),
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *importExportRoutes) getImportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	taskID := strings.TrimSpace(request.PathValue("task_id"))
	if taskID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.GetImportStatus(
		importWorkerContext(request, "", ""),
		&importexportv1.GetImportStatusRequest{TaskId: taskID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *importExportRoutes) getImportReport(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	taskID := strings.TrimSpace(request.PathValue("task_id"))
	if taskID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	stream, err := routes.client.GetImportReport(
		importWorkerContext(request, "", ""),
		&importexportv1.GetImportReportRequest{TaskId: taskID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	proxyReportStream(response, stream)
}

func (routes *importExportRoutes) createExport(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	var body struct {
		BaseID          string `json:"base_id"`
		TableID         string `json:"table_id"`
		ViewID          string `json:"view_id"`
		Format          string `json:"format"`
		ClientRequestID string `json:"client_request_id"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.TableID) == "" ||
		strings.TrimSpace(body.ViewID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	if authorization, ok := AuthorizationFromContext(request.Context()); ok {
		body.BaseID = authorization.BaseID
	}
	if strings.TrimSpace(body.BaseID) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	if _, ok := AuthorizationFromContext(request.Context()); !ok &&
		!routes.authorizeExport(response, request, body.BaseID) {
		return
	}
	format, ok := parseExportFormat(body.Format)
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.CreateExport(
		importWorkerContext(request, body.BaseID, body.TableID),
		&importexportv1.CreateExportRequest{
			BaseId:          body.BaseID,
			TableId:         body.TableID,
			ViewId:          body.ViewID,
			Format:          format,
			ClientRequestId: strings.TrimSpace(body.ClientRequestID),
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *importExportRoutes) exportStream(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	taskID := strings.TrimSpace(request.PathValue("task_id"))
	if taskID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	stream, err := routes.client.ExportStream(
		importWorkerContext(request, "", ""),
		&importexportv1.ExportStreamRequest{TaskId: taskID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	proxyExportStream(response, stream)
}

func (routes *importExportRoutes) authorizeImport(
	response http.ResponseWriter,
	request *http.Request,
	baseID string,
) bool {
	return routes.authorizeAction(response, request, baseID, sharev1.Action_ACTION_IMPORT)
}

func (routes *importExportRoutes) authorizeExport(
	response http.ResponseWriter,
	request *http.Request,
	baseID string,
) bool {
	principal, ok := PrincipalFromContext(request.Context())
	if !ok {
		writeCodeError(response, deal.CodeUnauthenticated)
		return false
	}
	decision, err := routes.pipeline.checkAccess(
		request.Context(),
		principal,
		baseID,
		sharev1.Action_ACTION_EXPORT,
	)
	if err != nil {
		writeBusinessError(response, err)
		return false
	}
	if !decision.GetInternal() {
		writeCodeError(response, deal.CodeExternalExportDenied)
		return false
	}
	if !decision.GetAllow() {
		writeCodeError(response, deal.CodePermissionDenied)
		return false
	}
	return true
}

func (routes *importExportRoutes) authorizeAction(
	response http.ResponseWriter,
	request *http.Request,
	baseID string,
	action sharev1.Action,
) bool {
	if authorization, ok := AuthorizationFromContext(request.Context()); ok {
		return authorization.BaseID == baseID && authorization.Action == action
	}
	principal, ok := PrincipalFromContext(request.Context())
	if !ok {
		writeCodeError(response, deal.CodeUnauthenticated)
		return false
	}
	decision, err := routes.pipeline.checkAccess(
		request.Context(),
		principal,
		baseID,
		action,
	)
	if err != nil {
		writeBusinessError(response, err)
		return false
	}
	if !decision.GetAllow() {
		writeCodeError(response, deal.CodePermissionDenied)
		return false
	}
	return true
}

func importWorkerContext(
	request *http.Request,
	baseID string,
	tableID string,
) context.Context {
	ctx := workerRequestContext(request, baseID)
	if tableID != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-table-id", tableID)
	}
	return ctx
}

func parseFieldType(value string) (structurev1.FieldType, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" {
		normalized = "TEXT"
	}
	if !strings.HasPrefix(normalized, "FIELD_TYPE_") {
		normalized = "FIELD_TYPE_" + normalized
	}
	fieldType, ok := structurev1.FieldType_value[normalized]
	if !ok || structurev1.FieldType(fieldType) == structurev1.FieldType_FIELD_TYPE_UNSPECIFIED {
		return structurev1.FieldType_FIELD_TYPE_UNSPECIFIED, false
	}
	return structurev1.FieldType(fieldType), true
}

func parseExportFormat(value string) (importexportv1.ExportFormat, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if !strings.HasPrefix(normalized, "EXPORT_FORMAT_") {
		normalized = "EXPORT_FORMAT_" + normalized
	}
	format, ok := importexportv1.ExportFormat_value[normalized]
	if !ok || importexportv1.ExportFormat(format) == importexportv1.ExportFormat_EXPORT_FORMAT_UNSPECIFIED {
		return importexportv1.ExportFormat_EXPORT_FORMAT_UNSPECIFIED, false
	}
	return importexportv1.ExportFormat(format), true
}

func writeProtoResult(response http.ResponseWriter, message proto.Message) {
	payload, err := protoJSON(message)
	if err != nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	writeSuccess(response, payload)
}

type reportChunkStream interface {
	Header() (metadata.MD, error)
	Recv() (*importexportv1.ReportChunk, error)
}

type exportChunkStream interface {
	Header() (metadata.MD, error)
	Recv() (*importexportv1.ExportChunk, error)
}

func proxyReportStream(response http.ResponseWriter, stream reportChunkStream) {
	prepareDownloadHeaders(response, stream.Header)
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			return
		}
		_, _ = response.Write(chunk.GetChunk())
	}
}

func proxyExportStream(response http.ResponseWriter, stream exportChunkStream) {
	prepareDownloadHeaders(response, stream.Header)
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			return
		}
		_, _ = response.Write(chunk.GetChunk())
	}
}

func prepareDownloadHeaders(
	response http.ResponseWriter,
	loadHeader func() (metadata.MD, error),
) {
	header, _ := loadHeader()
	mimeType := firstHeader(header, "x-file-mime")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	response.Header().Set("Content-Type", mimeType)
	if filename := firstHeader(header, "x-file-name"); filename != "" {
		response.Header().Set("X-File-Name", filename)
		response.Header().Set(
			"Content-Disposition",
			mime.FormatMediaType("attachment", map[string]string{"filename": filename}),
		)
	}
	response.WriteHeader(http.StatusOK)
}
