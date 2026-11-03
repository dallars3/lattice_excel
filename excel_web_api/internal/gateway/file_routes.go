// gateway/file_routes.go：H-77 ~ H-81 文件流路由。
package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"google.golang.org/grpc/metadata"

	filev1 "excel/api/file/v1"
	sharev1 "excel/api/share/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

const maxGatewayUploadBytes = 20 << 20

type fileRoutes struct {
	pipeline *Pipeline
	client   rpc.FileRPC
}

// RegisterFileRoutes binds the two-stage upload and file streaming endpoints.
func RegisterFileRoutes(pipeline *Pipeline, client rpc.FileRPC) {
	if pipeline == nil {
		panic("file routes pipeline is required")
	}
	routes := &fileRoutes{pipeline: pipeline, client: client}
	pipeline.BaseAuth(
		http.MethodPost,
		"/api/v1/bases/{base_id}/files",
		sharev1.Action_ACTION_CREATE_RECORD.String(),
		http.HandlerFunc(routes.upload),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/files",
		http.HandlerFunc(routes.upload),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/files/{file_id}/download",
		http.HandlerFunc(routes.download),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/files/{file_id}/thumb",
		http.HandlerFunc(routes.thumb),
	)
	pipeline.BaseAuth(
		http.MethodPatch,
		"/api/v1/bases/{base_id}/files/{file_id}",
		sharev1.Action_ACTION_CREATE_RECORD.String(),
		http.HandlerFunc(routes.rename),
	)
	pipeline.SessionRequired(
		http.MethodPatch,
		"/api/v1/files/{file_id}",
		http.HandlerFunc(routes.rename),
	)
	pipeline.BaseAuth(
		http.MethodDelete,
		"/api/v1/bases/{base_id}/files/{file_id}",
		sharev1.Action_ACTION_CREATE_RECORD.String(),
		http.HandlerFunc(routes.delete),
	)
	pipeline.SessionRequired(
		http.MethodDelete,
		"/api/v1/files/{file_id}",
		http.HandlerFunc(routes.delete),
	)
}

func (routes *fileRoutes) upload(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	request.Body = http.MaxBytesReader(
		response,
		request.Body,
		maxGatewayUploadBytes+(1<<20),
	)
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		writeCodeError(response, deal.CodeFileRejected)
		return
	}
	baseID := fileBaseID(request)
	usage, ok := parseFileUsage(request.FormValue("usage"))
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	action := sharev1.Action_ACTION_CREATE_RECORD
	if usage == filev1.FileUsage_FILE_USAGE_IMPORT_TEMP {
		action = sharev1.Action_ACTION_IMPORT
	}
	if baseID == "" || !routes.authorizeFileWrite(response, request, baseID, action) {
		return
	}
	file, header, err := request.FormFile("file")
	if err != nil || header.Size <= 0 || header.Size > maxGatewayUploadBytes {
		writeCodeError(response, deal.CodeFileRejected)
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = mime.TypeByExtension(ext)
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	ctx := workerRequestContext(request, baseID)
	stream, err := routes.client.InitUpload(ctx)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	if err := stream.Send(&filev1.InitUploadRequest{
		Name:  header.Filename,
		Ext:   ext,
		Mime:  mimeType,
		Size:  header.Size,
		Usage: usage,
	}); err != nil {
		writeBusinessError(response, err)
		return
	}
	checksum := sha256.New()
	buffer := make([]byte, 64<<10)
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			chunk := append([]byte(nil), buffer[:count]...)
			_, _ = checksum.Write(chunk)
			if sendErr := stream.Send(&filev1.InitUploadRequest{Chunk: chunk}); sendErr != nil {
				writeBusinessError(response, sendErr)
				return
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			writeCodeError(response, deal.CodeFileRejected)
			return
		}
	}
	initialized, err := stream.CloseAndRecv()
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	confirmed, err := routes.client.ConfirmUpload(ctx, &filev1.ConfirmUploadRequest{
		FileId:   initialized.GetFileMeta().GetId(),
		Checksum: hex.EncodeToString(checksum.Sum(nil)),
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeFileMeta(response, confirmed.GetFileMeta())
}

func (routes *fileRoutes) download(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	fileID := strings.TrimSpace(request.PathValue("file_id"))
	if fileID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	stream, err := routes.client.DownloadStream(
		workerRequestContext(request, ""),
		&filev1.DownloadStreamRequest{FileId: fileID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	proxyFileStream(response, stream, "attachment")
}

func (routes *fileRoutes) thumb(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	fileID := strings.TrimSpace(request.PathValue("file_id"))
	if fileID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	stream, err := routes.client.GetThumb(
		workerRequestContext(request, ""),
		&filev1.GetThumbRequest{FileId: fileID},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	proxyFileStream(response, stream, "inline")
}

func (routes *fileRoutes) rename(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	fileID := strings.TrimSpace(request.PathValue("file_id"))
	if fileID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
	}
	if err := decodeJSONBody(response, request, &body); err != nil ||
		strings.TrimSpace(body.DisplayName) == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	baseID := fileBaseID(request)
	if baseID == "" || !routes.authorizeFileWrite(
		response,
		request,
		baseID,
		sharev1.Action_ACTION_CREATE_RECORD,
	) {
		return
	}
	result, err := routes.client.RenameFile(
		workerRequestContext(request, baseID),
		&filev1.RenameFileRequest{FileId: fileID, DisplayName: body.DisplayName},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeFileMeta(response, result.GetFileMeta())
}

func (routes *fileRoutes) delete(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	fileID := strings.TrimSpace(request.PathValue("file_id"))
	if fileID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	baseID := fileBaseID(request)
	if baseID == "" || !routes.authorizeFileWrite(
		response,
		request,
		baseID,
		sharev1.Action_ACTION_CREATE_RECORD,
	) {
		return
	}
	if _, err := routes.client.DeleteFile(
		workerRequestContext(request, baseID),
		&filev1.DeleteFileRequest{FileId: fileID},
	); err != nil {
		writeBusinessError(response, err)
		return
	}
	writeSuccess(response, map[string]any{})
}

func (routes *fileRoutes) authorizeFileWrite(
	response http.ResponseWriter,
	request *http.Request,
	baseID string,
	action sharev1.Action,
) bool {
	if authorization, ok := AuthorizationFromContext(request.Context()); ok {
		if authorization.BaseID != baseID {
			writeCodeError(response, deal.CodePermissionDenied)
			return false
		}
		if authorization.Action == action {
			return true
		}
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

func fileBaseID(request *http.Request) string {
	if value := strings.TrimSpace(request.PathValue("base_id")); value != "" {
		return value
	}
	if authorization, ok := AuthorizationFromContext(request.Context()); ok {
		return authorization.BaseID
	}
	if request.Form != nil {
		if value := strings.TrimSpace(request.Form.Get("base_id")); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(request.URL.Query().Get("base_id")); value != "" {
		return value
	}
	return strings.TrimSpace(request.Header.Get("X-Base-Id"))
}

func workerRequestContext(request *http.Request, baseID string) context.Context {
	ctx := request.Context()
	if principal, ok := PrincipalFromContext(ctx); ok &&
		principal.GetEnterpriseId() != "" {
		ctx = metadata.AppendToOutgoingContext(
			ctx,
			"x-enterprise-id",
			principal.GetEnterpriseId(),
		)
	}
	if baseID != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-base-id", baseID)
	}
	if tableID := strings.TrimSpace(request.Header.Get("X-Table-Id")); tableID != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-table-id", tableID)
	}
	return ctx
}

func parseFileUsage(value string) (filev1.FileUsage, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" {
		normalized = "ATTACHMENT"
	}
	if !strings.HasPrefix(normalized, "FILE_USAGE_") {
		normalized = "FILE_USAGE_" + normalized
	}
	usage, ok := filev1.FileUsage_value[normalized]
	if !ok || filev1.FileUsage(usage) == filev1.FileUsage_FILE_USAGE_UNSPECIFIED {
		return filev1.FileUsage_FILE_USAGE_UNSPECIFIED, false
	}
	return filev1.FileUsage(usage), true
}

func writeFileMeta(response http.ResponseWriter, meta *filev1.FileMeta) {
	payload, err := protoJSON(meta)
	if err != nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	writeSuccess(response, payload)
}

type fileChunkStream interface {
	Header() (metadata.MD, error)
	Recv() (*filev1.FileChunk, error)
}

func proxyFileStream(
	response http.ResponseWriter,
	stream fileChunkStream,
	disposition string,
) {
	header, err := stream.Header()
	if err != nil && len(header) == 0 {
		writeBusinessError(response, err)
		return
	}
	filename := firstHeader(header, "x-file-name")
	mimeType := firstHeader(header, "x-file-mime")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	response.Header().Set("Content-Type", mimeType)
	if filename != "" {
		response.Header().Set(
			"Content-Disposition",
			mime.FormatMediaType(disposition, map[string]string{"filename": filename}),
		)
	}
	response.WriteHeader(http.StatusOK)
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			return
		}
		if recvErr != nil {
			return
		}
		if _, writeErr := response.Write(chunk.GetChunk()); writeErr != nil {
			return
		}
	}
}

func firstHeader(header metadata.MD, key string) string {
	values := header.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
