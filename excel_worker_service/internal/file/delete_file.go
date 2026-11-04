// file/delete_file.go：附件元数据、磁盘本与池计量的回收。
package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/types/known/emptypb"

	filev1 "excel/api/file/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

// DeleteFile removes metadata, bytes and the pool reservation.
func (service *Service) DeleteFile(
	ctx context.Context,
	request *filev1.DeleteFileRequest,
) (*emptypb.Empty, error) {
	if service == nil || service.repository == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if _, err := deal.PrincipalFromContext(ctx); err != nil {
		return nil, err
	}
	if request == nil || request.GetFileId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	meta, err := service.repository.Get(ctx, request.GetFileId())
	if errors.Is(err, base.ErrFileNotFound) {
		return &emptypb.Empty{}, nil
	}
	if err != nil {
		return nil, err
	}
	if generatedFile(meta) || !sameUploadBase(ctx, meta) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	deleted, err := service.repository.Delete(ctx, request.GetFileId())
	if err != nil {
		return nil, err
	}
	if err := os.Remove(filepath.Join(service.attachDir, deleted.StoragePath)); err != nil &&
		!os.IsNotExist(err) {
		service.logger.Warn(
			"remove orphaned file bytes",
			"file_id", deleted.ID,
			"error", err,
		)
	}
	if service.quota != nil {
		if err := service.quota.Release(ctx, deleted.Size); err != nil {
			return nil, err
		}
	}
	return &emptypb.Empty{}, nil
}
