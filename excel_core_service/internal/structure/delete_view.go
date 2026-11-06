package structure

import (
	"context"
	"fmt"

	"excel_core_service/deal"

	"google.golang.org/protobuf/types/known/emptypb"

	structurev1 "excel/api/structure/v1"
)

// DeleteView removes one view while retaining at least one view per table.
func (service *Service) DeleteView(
	ctx context.Context,
	request *structurev1.DeleteViewRequest,
) (*emptypb.Empty, error) {
	principal, _, err := service.memberPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := service.repository.ViewByID(ctx, request.GetViewId())
	if err != nil {
		return nil, mapStructureError(err)
	}
	table, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		view.TableID,
	)
	if err != nil {
		return nil, mapStructureError(err)
	}
	count, err := service.repository.CountViews(ctx, view.TableID)
	if err != nil {
		return nil, err
	}
	if count <= 1 {
		return nil, deal.RPCError(1001)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.DeleteViewTx(ctx, tx, view.ID); err != nil {
		return nil, mapStructureError(err)
	}
	// 缺陷 #18（W-11）：视图删除成功与 view_deleted 广播事件同事务落 outbox。
	if err := service.publishStructureChanged(
		ctx,
		tx,
		table,
		"",
		structureChangeViewDeleted,
		principal.GetUserId(),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit DeleteView: %w", err)
	}
	return &emptypb.Empty{}, nil
}
