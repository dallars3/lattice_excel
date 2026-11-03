// file/init_upload.go：客户端流上传、黑名单、20MB 与全站池容量校验。
package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"excel-libs/idgen"
	filev1 "excel/api/file/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
	workerredis "excel_worker_service/redis"
)

// InitUpload streams one file into the usage-specific temporary area.
func (service *Service) InitUpload(stream filev1.FileService_InitUploadServer) error {
	if service == nil || service.repository == nil || service.quota == nil {
		return rpcError(deal.CodeServiceUnavailable)
	}
	principal, err := principalFromContext(stream.Context())
	if err != nil {
		return err
	}
	first, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		return rpcError(deal.CodeInvalidArgument)
	}
	if err != nil {
		return err
	}
	name, ext, err := validateUploadMetadata(first)
	if err != nil {
		return err
	}
	if err := inspectChunk(first.GetChunk()); err != nil {
		return err
	}
	if err := service.reserveUpload(stream.Context(), first.GetSize()); err != nil {
		return err
	}

	fileID, err := idgen.New()
	if err != nil {
		_ = service.quota.Release(stream.Context(), first.GetSize())
		return err
	}
	tempDir := filepath.Join(service.attachDir, "tmp")
	if err := os.MkdirAll(tempDir, 0o750); err != nil {
		_ = service.quota.Release(stream.Context(), first.GetSize())
		return fmt.Errorf("create upload temp directory: %w", err)
	}
	tempPath := filepath.Join(tempDir, fileID+".part")
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		_ = service.quota.Release(stream.Context(), first.GetSize())
		return fmt.Errorf("create upload temp file: %w", err)
	}

	checksum := sha256.New()
	written, streamErr := writeUploadStream(stream, first, file, checksum)
	closeErr := file.Close()
	if streamErr == nil {
		streamErr = closeErr
	}
	if streamErr == nil && written != first.GetSize() {
		streamErr = rpcError(deal.CodeFileRejected)
	}
	if streamErr != nil {
		_ = os.Remove(tempPath)
		_ = service.quota.Release(stream.Context(), first.GetSize())
		return streamErr
	}

	meta := base.FileMeta{
		ID:          fileID,
		Name:        name,
		DisplayName: name,
		Ext:         ext,
		Mime:        normalizeMIME(first.GetMime()),
		Size:        first.GetSize(),
		Usage:       usageName(first.GetUsage()),
		Uploader:    principal.GetUserId(),
		UploadBase:  incomingMetadata(stream.Context(), "x-base-id"),
		Checksum:    hex.EncodeToString(checksum.Sum(nil)),
		CreatedAt:   service.now().UTC().UnixMilli(),
	}
	service.mu.Lock()
	service.pending[fileID] = &pendingUpload{
		meta:     meta,
		tempPath: tempPath,
		reserved: first.GetSize(),
	}
	service.mu.Unlock()
	return stream.SendAndClose(&filev1.InitUploadResponse{
		FileMeta: fileMetaToProto(meta),
	})
}

func (service *Service) reserveUpload(ctx context.Context, size int64) error {
	used, err := service.repository.UsedBytes(ctx)
	if err != nil {
		return err
	}
	if err := service.quota.EnsureInitialized(ctx, used); err != nil {
		return err
	}
	if err := service.quota.Reserve(ctx, size); err != nil {
		if errors.Is(err, workerredis.ErrQuotaExceeded) {
			return rpcError(deal.CodeFileRejected)
		}
		return err
	}
	return nil
}

func writeUploadStream(
	stream filev1.FileService_InitUploadServer,
	first *filev1.InitUploadRequest,
	file *os.File,
	checksum hash.Hash,
) (int64, error) {
	var written int64
	request := first
	for {
		chunk := request.GetChunk()
		if len(chunk) > 0 {
			if written+int64(len(chunk)) > maxFileBytes {
				return written, rpcError(deal.CodeFileRejected)
			}
			count, err := file.Write(chunk)
			written += int64(count)
			if err != nil {
				return written, fmt.Errorf("write upload chunk: %w", err)
			}
			_, _ = checksum.Write(chunk[:count])
		}
		next, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return written, nil
		}
		if err != nil {
			return written, err
		}
		if err := inspectChunk(next.GetChunk()); err != nil {
			return written, err
		}
		request = next
	}
}
