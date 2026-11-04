// file/confirm_upload.go：SHA-256 完整性确认与正式落位。
package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	filev1 "excel/api/file/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

// ConfirmUpload verifies the streamed bytes and publishes file metadata.
func (service *Service) ConfirmUpload(
	ctx context.Context,
	request *filev1.ConfirmUploadRequest,
) (*filev1.ConfirmUploadResponse, error) {
	if service == nil || service.repository == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if request == nil || strings.TrimSpace(request.GetFileId()) == "" ||
		strings.TrimSpace(request.GetChecksum()) == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if existing, err := service.repository.Get(ctx, request.GetFileId()); err == nil {
		if strings.EqualFold(existing.Checksum, request.GetChecksum()) {
			return &filev1.ConfirmUploadResponse{FileMeta: fileMetaToProto(existing)}, nil
		}
		return nil, deal.RPCError(deal.CodeFileRejected)
	} else if !errors.Is(err, base.ErrFileNotFound) {
		return nil, err
	}

	service.mu.Lock()
	pending := service.pending[request.GetFileId()]
	if pending != nil {
		delete(service.pending, request.GetFileId())
	}
	service.mu.Unlock()
	if pending == nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if !strings.EqualFold(pending.meta.Checksum, strings.TrimSpace(request.GetChecksum())) {
		service.releasePending(pending)
		return nil, deal.RPCError(deal.CodeFileRejected)
	}

	storagePath, err := service.finalizeUpload(pending)
	if err != nil {
		service.releasePending(pending)
		return nil, err
	}
	pending.meta.StoragePath = storagePath
	if err := service.repository.Create(ctx, pending.meta); err != nil {
		_ = os.Remove(filepath.Join(service.attachDir, storagePath))
		_ = service.quota.Release(ctx, pending.reserved)
		pending.reserved = 0
		return nil, err
	}
	pending.committed = true
	pending.reserved = 0
	return &filev1.ConfirmUploadResponse{
		FileMeta: fileMetaToProto(pending.meta),
	}, nil
}

func (service *Service) finalizeUpload(pending *pendingUpload) (string, error) {
	directory := "files"
	if pending.meta.Usage == "import_temp" {
		directory = filepath.Join("files", "import")
	}
	finalDir := filepath.Join(service.attachDir, directory)
	if err := os.MkdirAll(finalDir, 0o750); err != nil {
		return "", fmt.Errorf("create attachment directory: %w", err)
	}
	filename := pending.meta.ID
	if pending.meta.Ext != "" {
		filename += "." + pending.meta.Ext
	}
	finalPath := filepath.Join(finalDir, filename)
	if err := os.Rename(pending.tempPath, finalPath); err != nil {
		return "", fmt.Errorf("finalize upload: %w", err)
	}
	return filepath.ToSlash(filepath.Join(directory, filename)), nil
}
