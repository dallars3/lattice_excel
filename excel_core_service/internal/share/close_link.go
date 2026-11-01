// share/close_link.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"
	"database/sql"
	"errors"

	"google.golang.org/protobuf/types/known/emptypb"

	sharev1 "excel/api/share/v1"
	"excel_core_service/deal"
)

// CloseLink disables the Base link and emits link.closed in the same transaction.
func (service *Service) CloseLink(
	ctx context.Context,
	request *sharev1.CloseLinkRequest,
) (*emptypb.Empty, error) {
	if request == nil || request.GetBaseId() == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := service.requireAction(
		ctx,
		request.GetBaseId(),
		sharev1.Action_ACTION_MANAGE_SHARE,
	)
	if err != nil {
		return nil, err
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	link, err := service.repository.LinkByBaseIDForUpdate(ctx, tx, request.GetBaseId())
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return &emptypb.Empty{}, nil
	case err != nil:
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	link.Enabled = false
	link.UpdatedAt = service.now().UTC().UnixMilli()
	if err := service.repository.UpsertLink(ctx, tx, link); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueLinkClosed(ctx, tx, link.BaseID, principal.GetUserId(), "closed"); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
