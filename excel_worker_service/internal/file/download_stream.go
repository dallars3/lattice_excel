// file/download_stream.go：文件元数据校验与服务端分块下传。
package file

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	filev1 "excel/api/file/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

// DownloadStream streams a stored file in bounded chunks.
func (service *Service) DownloadStream(
	request *filev1.DownloadStreamRequest,
	stream filev1.FileService_DownloadStreamServer,
) error {
	if service == nil || service.repository == nil {
		return rpcError(deal.CodeServiceUnavailable)
	}
	if request == nil || request.GetFileId() == "" {
		return rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(stream.Context())
	if err != nil {
		return err
	}
	meta, err := service.repository.Get(stream.Context(), request.GetFileId())
	if errors.Is(err, base.ErrFileNotFound) {
		return rpcError(deal.CodeDeletedResource)
	}
	if err != nil {
		return err
	}
	if generatedFile(meta) && meta.Uploader != principal.GetUserId() {
		return rpcError(deal.CodePermissionDenied)
	}
	file, err := os.Open(filepath.Join(service.attachDir, meta.StoragePath))
	if err != nil {
		if os.IsNotExist(err) {
			return rpcError(deal.CodeDeletedResource)
		}
		return err
	}
	defer file.Close()
	setFileHeaders(stream.Context(), meta.DisplayName, meta.Mime)
	return streamChunks(file, func(chunk []byte) error {
		return stream.Send(&filev1.FileChunk{Chunk: chunk})
	})
}

func streamChunks(file io.Reader, send func([]byte) error) error {
	buffer := make([]byte, streamChunk)
	for {
		count, err := file.Read(buffer)
		if count > 0 {
			chunk := append([]byte(nil), buffer[:count]...)
			if sendErr := send(chunk); sendErr != nil {
				return sendErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
