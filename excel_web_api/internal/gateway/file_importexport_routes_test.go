package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	commonv1 "excel/api/common/v1"
	filev1 "excel/api/file/v1"
	iamv1 "excel/api/iam/v1"
	importexportv1 "excel/api/importexport/v1"
	sharev1 "excel/api/share/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

func TestExportRouteRejectsExternalCollaboratorBeforeBusinessRPC(t *testing.T) {
	iam := &pipelineIamRPC{
		exchangeResponse: &iamv1.ExchangeSessionResponse{
			Principal: &commonv1.Principal{
				UserId:       "external_1",
				EnterpriseId: "",
				Internal:     false,
				LinkContext: &commonv1.LinkContext{
					BaseId: "base_1",
					Role:   "editor",
				},
			},
		},
	}
	share := &pipelineShareRPC{
		checkResponse: &sharev1.CheckAccessResponse{
			Allow:    true,
			Role:     sharev1.Role_ROLE_EDITOR,
			Internal: false,
		},
	}
	client := &fakeImportExportRPC{}
	pipeline := NewPipeline(iam, share, nil, nil)
	RegisterImportExportRoutes(pipeline, client)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/exports",
		strings.NewReader(`{
			"base_id":"base_1",
			"table_id":"table_1",
			"view_id":"view_1",
			"format":"xlsx"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "external-session"})
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != http.StatusForbidden || envelope.Code != deal.CodeExternalExportDenied {
		t.Fatalf(
			"response = HTTP %d code %d, want HTTP 403 code 3002",
			response.Code,
			envelope.Code,
		)
	}
	if client.createExportCalls != 0 {
		t.Fatalf("CreateExport calls = %d, want 0", client.createExportCalls)
	}
	if share.lastCheck.GetAction() != sharev1.Action_ACTION_EXPORT {
		t.Fatalf("CheckAccess action = %s, want export", share.lastCheck.GetAction())
	}
}

func TestExportAndImportRoutesForwardEnterpriseMetadata(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "enterprise_1")}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	client := &fakeImportExportRPC{}
	pipeline := NewPipeline(iam, share, nil, nil)
	RegisterImportExportRoutes(pipeline, client)

	exportBody := `{
		"base_id":"base_1",
		"table_id":"table_1",
		"view_id":"view_1",
		"format":"csv",
		"client_request_id":"export-request-1"
	}`
	exportRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/exports",
		strings.NewReader(exportBody),
	)
	exportRequest.Header.Set("Content-Type", "application/json")
	exportRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	exportResponse := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK {
		t.Fatalf("export response = HTTP %d body=%s", exportResponse.Code, exportResponse.Body.String())
	}
	if client.createExportCalls != 1 ||
		client.lastExport.GetBaseId() != "base_1" ||
		client.lastExport.GetTableId() != "table_1" ||
		client.lastExport.GetViewId() != "view_1" ||
		client.lastExport.GetFormat() != importexportv1.ExportFormat_EXPORT_FORMAT_CSV {
		t.Fatalf("CreateExport request = %+v calls=%d", client.lastExport, client.createExportCalls)
	}
	if client.exportMetadata.Get("x-enterprise-id")[0] != "enterprise_1" ||
		client.exportMetadata.Get("x-base-id")[0] != "base_1" ||
		client.exportMetadata.Get("x-table-id")[0] != "table_1" {
		t.Fatalf("export metadata = %v", client.exportMetadata)
	}

	importBody := `{
		"file_id":"file_1",
		"first_row_header":true,
		"column_config_list":[{"index":0,"name":"姓名","type":"text"}]
	}`
	importRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/bases/base_1/imports",
		strings.NewReader(importBody),
	)
	importRequest.Header.Set("Content-Type", "application/json")
	importRequest.Header.Set("X-Table-Id", "table_1")
	importRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	importResponse := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(importResponse, importRequest)
	if importResponse.Code != http.StatusOK {
		t.Fatalf("import response = HTTP %d body=%s", importResponse.Code, importResponse.Body.String())
	}
	if client.createImportCalls != 1 || share.lastCheck.GetAction() != sharev1.Action_ACTION_IMPORT {
		t.Fatalf(
			"import calls=%d action=%s",
			client.createImportCalls,
			share.lastCheck.GetAction(),
		)
	}
	if client.importMetadata.Get("x-enterprise-id")[0] != "enterprise_1" ||
		client.importMetadata.Get("x-base-id")[0] != "base_1" ||
		client.importMetadata.Get("x-table-id")[0] != "table_1" {
		t.Fatalf("import metadata = %v", client.importMetadata)
	}
}

func TestFileUploadUsesCreateRecordAuthAndPassesWorkerMetadata(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "enterprise_1")}
	share := &pipelineShareRPC{checkResponse: allowedDecision(true)}
	initStream := &fakeInitUploadStream{response: &filev1.InitUploadResponse{
		FileMeta: &filev1.FileMeta{
			Id:          "file_1",
			Name:        "people.csv",
			DisplayName: "people.csv",
			Ext:         ".csv",
			Mime:        "text/csv",
			Size:        17,
			Uploader:    "user_1",
		},
	}}
	client := &fakeFileRPC{initStream: initStream}
	pipeline := NewPipeline(iam, share, nil, nil)
	RegisterFileRoutes(pipeline, client)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("base_id", "base_1"); err != nil {
		t.Fatalf("WriteField(base_id) error = %v", err)
	}
	if err := writer.WriteField("usage", "attachment"); err != nil {
		t.Fatalf("WriteField(usage) error = %v", err)
	}
	part, err := writer.CreateFormFile("file", "people.csv")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	content := []byte("name,age\nAlice,30\n")
	if _, err := part.Write(content); err != nil {
		t.Fatalf("part.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart.Close() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/files", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("upload response = HTTP %d body=%s", response.Code, response.Body.String())
	}
	if share.lastCheck.GetAction() != sharev1.Action_ACTION_CREATE_RECORD {
		t.Fatalf("CheckAccess action = %s, want create_record", share.lastCheck.GetAction())
	}
	if len(initStream.sent) < 2 ||
		initStream.sent[0].GetUsage() != filev1.FileUsage_FILE_USAGE_ATTACHMENT ||
		initStream.sent[0].GetName() != "people.csv" {
		t.Fatalf("InitUpload sends = %+v", initStream.sent)
	}
	if client.confirmRequest.GetFileId() != "file_1" ||
		client.confirmRequest.GetChecksum() == "" {
		t.Fatalf("ConfirmUpload request = %+v", client.confirmRequest)
	}
	for _, outgoing := range []metadata.MD{initStream.metadata, client.confirmMetadata} {
		if outgoing.Get("x-enterprise-id")[0] != "enterprise_1" ||
			outgoing.Get("x-base-id")[0] != "base_1" {
			t.Fatalf("worker metadata = %v", outgoing)
		}
	}
}

func TestFileDownloadPreservesServerFilenameAndBytes(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "enterprise_1")}
	stream := &fakeDownloadStream{
		header: metadata.Pairs(
			"x-file-name", "季度 报表.csv",
			"x-file-mime", "text/csv",
		),
		chunks: [][]byte{[]byte("a,b\n"), []byte("1,2\n")},
	}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterFileRoutes(pipeline, &fakeFileRPC{downloadStream: stream})

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/files/file_1/download",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK ||
		response.Body.String() != "a,b\n1,2\n" ||
		!strings.Contains(response.Header().Get("Content-Disposition"), "filename") {
		t.Fatalf(
			"download = HTTP %d body=%q disposition=%q",
			response.Code,
			response.Body.String(),
			response.Header().Get("Content-Disposition"),
		)
	}
}

type fakeFileRPC struct {
	initStream      *fakeInitUploadStream
	confirmRequest  *filev1.ConfirmUploadRequest
	confirmMetadata metadata.MD
	downloadStream  *fakeDownloadStream
}

func (fake *fakeFileRPC) InitUpload(
	ctx context.Context,
	_ ...grpc.CallOption,
) (filev1.FileService_InitUploadClient, error) {
	fake.initStream.ctx = ctx
	return fake.initStream, nil
}

func (fake *fakeFileRPC) ConfirmUpload(
	ctx context.Context,
	request *filev1.ConfirmUploadRequest,
	_ ...grpc.CallOption,
) (*filev1.ConfirmUploadResponse, error) {
	fake.confirmRequest = request
	fake.confirmMetadata, _ = metadata.FromOutgoingContext(ctx)
	return &filev1.ConfirmUploadResponse{FileMeta: fake.initStream.response.GetFileMeta()}, nil
}

func (fake *fakeFileRPC) DownloadStream(
	context.Context,
	*filev1.DownloadStreamRequest,
	...grpc.CallOption,
) (filev1.FileService_DownloadStreamClient, error) {
	return fake.downloadStream, nil
}

func (fake *fakeFileRPC) GetThumb(
	context.Context,
	*filev1.GetThumbRequest,
	...grpc.CallOption,
) (filev1.FileService_GetThumbClient, error) {
	panic("GetThumb must not be called")
}

func (fake *fakeFileRPC) RenameFile(
	context.Context,
	*filev1.RenameFileRequest,
	...grpc.CallOption,
) (*filev1.RenameFileResponse, error) {
	panic("RenameFile must not be called")
}

func (fake *fakeFileRPC) DeleteFile(
	context.Context,
	*filev1.DeleteFileRequest,
	...grpc.CallOption,
) (*emptypb.Empty, error) {
	panic("DeleteFile must not be called")
}

var _ rpc.FileRPC = (*fakeFileRPC)(nil)

type fakeInitUploadStream struct {
	ctx      context.Context
	response *filev1.InitUploadResponse
	sent     []*filev1.InitUploadRequest
	metadata metadata.MD
}

func (stream *fakeInitUploadStream) Send(request *filev1.InitUploadRequest) error {
	stream.sent = append(stream.sent, request)
	if stream.metadata == nil {
		stream.metadata, _ = metadata.FromOutgoingContext(stream.ctx)
	}
	return nil
}

func (stream *fakeInitUploadStream) CloseAndRecv() (*filev1.InitUploadResponse, error) {
	return stream.response, nil
}

func (stream *fakeInitUploadStream) Header() (metadata.MD, error) { return nil, nil }
func (stream *fakeInitUploadStream) Trailer() metadata.MD         { return nil }
func (stream *fakeInitUploadStream) CloseSend() error             { return nil }
func (stream *fakeInitUploadStream) Context() context.Context     { return stream.ctx }
func (stream *fakeInitUploadStream) SendMsg(any) error            { return nil }
func (stream *fakeInitUploadStream) RecvMsg(any) error            { return io.EOF }

type fakeDownloadStream struct {
	header metadata.MD
	chunks [][]byte
	index  int
}

func (stream *fakeDownloadStream) Header() (metadata.MD, error) {
	return stream.header, nil
}

func (stream *fakeDownloadStream) Trailer() metadata.MD     { return nil }
func (stream *fakeDownloadStream) CloseSend() error         { return nil }
func (stream *fakeDownloadStream) Context() context.Context { return context.Background() }
func (stream *fakeDownloadStream) SendMsg(any) error        { return nil }
func (stream *fakeDownloadStream) RecvMsg(any) error        { return io.EOF }

func (stream *fakeDownloadStream) Recv() (*filev1.FileChunk, error) {
	if stream.index >= len(stream.chunks) {
		return nil, io.EOF
	}
	chunk := &filev1.FileChunk{Chunk: stream.chunks[stream.index]}
	stream.index++
	return chunk, nil
}

type fakeImportExportRPC struct {
	createExportCalls int
	createImportCalls int
	lastExport        *importexportv1.CreateExportRequest
	lastImport        *importexportv1.CreateImportRequest
	exportMetadata    metadata.MD
	importMetadata    metadata.MD
}

func (fake *fakeImportExportRPC) PreviewImport(
	context.Context,
	*importexportv1.PreviewImportRequest,
	...grpc.CallOption,
) (*importexportv1.PreviewImportResponse, error) {
	panic("PreviewImport must not be called")
}

func (fake *fakeImportExportRPC) CreateImport(
	ctx context.Context,
	request *importexportv1.CreateImportRequest,
	_ ...grpc.CallOption,
) (*importexportv1.CreateImportResponse, error) {
	fake.createImportCalls++
	fake.lastImport = request
	fake.importMetadata, _ = metadata.FromOutgoingContext(ctx)
	return &importexportv1.CreateImportResponse{ImportTask: &importexportv1.ImportTask{
		Id:     "import_1",
		Status: importexportv1.ImportStatus_IMPORT_STATUS_DONE,
	}}, nil
}

func (fake *fakeImportExportRPC) GetImportStatus(
	context.Context,
	*importexportv1.GetImportStatusRequest,
	...grpc.CallOption,
) (*importexportv1.GetImportStatusResponse, error) {
	panic("GetImportStatus must not be called")
}

func (fake *fakeImportExportRPC) GetImportReport(
	context.Context,
	*importexportv1.GetImportReportRequest,
	...grpc.CallOption,
) (importexportv1.ImportExportService_GetImportReportClient, error) {
	panic("GetImportReport must not be called")
}

func (fake *fakeImportExportRPC) CreateExport(
	ctx context.Context,
	request *importexportv1.CreateExportRequest,
	_ ...grpc.CallOption,
) (*importexportv1.CreateExportResponse, error) {
	fake.createExportCalls++
	fake.lastExport = request
	fake.exportMetadata, _ = metadata.FromOutgoingContext(ctx)
	return &importexportv1.CreateExportResponse{ExportTask: &importexportv1.ExportTask{
		Id:       "export_1",
		Status:   importexportv1.ExportStatus_EXPORT_STATUS_DONE,
		Format:   request.GetFormat(),
		FileName: "客户数据_客户表_全部_20261103.csv",
		Rows:     2,
	}}, nil
}

func (fake *fakeImportExportRPC) ExportStream(
	context.Context,
	*importexportv1.ExportStreamRequest,
	...grpc.CallOption,
) (importexportv1.ImportExportService_ExportStreamClient, error) {
	panic("ExportStream must not be called")
}

var _ rpc.ImportExportRPC = (*fakeImportExportRPC)(nil)
