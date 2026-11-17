package structure

import (
	"context"
	"fmt"

	"excel_core_service/deal"

	structurev1 "excel/api/structure/v1"
)

// RenameView renames one view for any enterprise member.
func (service *Service) RenameView(
	ctx context.Context,
	request *structurev1.RenameViewRequest,
) (*structurev1.RenameViewResponse, error) {
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
	name, err := normalizeStructureName(request.GetName())
	if err != nil {
		return nil, deal.RPCError(1001)
	}
	exists, err := service.repository.ViewNameExists(ctx, view.TableID, name, view.ID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, deal.RPCError(deal.CodeConflict)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.RenameViewTx(ctx, tx, view.ID, name); err != nil {
		return nil, mapStructureError(err)
	}
	// 缺陷 #18（W-11）：视图写成功即与写同事务发布结构事件。
	if err := service.publishStructureChanged(
		ctx,
		tx,
		table,
		"",
		structureChangeViewRenamed,
		principal.GetUserId(),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit RenameView: %w", err)
	}
	view.Name = name
	return &structurev1.RenameViewResponse{View: viewToProto(view)}, nil
}
