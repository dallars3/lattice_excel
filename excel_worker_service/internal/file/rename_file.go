// file/rename_file.go：附件显示名更新。
package file

import (
	"context"
	"errors"

	filev1 "excel/api/file/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

// RenameFile updates the display name for a file in the same authorized Base.
func (service *Service) RenameFile(
	ctx context.Context,
	request *filev1.RenameFileRequest,
) (*filev1.RenameFileResponse, error) {
	if service == nil || service.repository == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if _, err := deal.PrincipalFromContext(ctx); err != nil {
		return nil, err
	}
	if request == nil || request.GetFileId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	displayName, err := cleanDisplayName(request.GetDisplayName())
	if err != nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	meta, err := service.repository.Get(ctx, request.GetFileId())
	if errors.Is(err, base.ErrFileNotFound) {
		return nil, deal.RPCError(deal.CodeDeletedResource)
	}
	if err != nil {
		return nil, err
	}
	if generatedFile(meta) || !sameUploadBase(ctx, meta) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	meta, err = service.repository.Rename(ctx, request.GetFileId(), displayName)
	if err != nil {
		return nil, err
	}
	return &filev1.RenameFileResponse{FileMeta: fileMetaToProto(meta)}, nil
}

func sameUploadBase(ctx context.Context, meta base.FileMeta) bool {
	baseID := incomingMetadata(ctx, "x-base-id")
	return baseID != "" && meta.UploadBase != "" && meta.UploadBase == baseID
}

func generatedFile(meta base.FileMeta) bool {
	return meta.Usage == "import_report" || meta.Usage == "export"
}
