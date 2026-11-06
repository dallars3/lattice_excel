package structure

import (
	"context"
	"fmt"

	structurev1 "excel/api/structure/v1"
)

// UpdateViewConfig validates and immediately persists view configuration.
func (service *Service) UpdateViewConfig(
	ctx context.Context,
	request *structurev1.UpdateViewConfigRequest,
) (*structurev1.UpdateViewConfigResponse, error) {
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
	if err := service.validateViewConfig(ctx, view.TableID, request.GetViewConfig()); err != nil {
		return nil, err
	}
	configPayload, err := encodeViewConfig(request.GetViewConfig())
	if err != nil {
		return nil, err
	}
	view.Config = configPayload
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.UpdateViewTx(ctx, tx, view); err != nil {
		return nil, mapStructureError(err)
	}
	// 缺陷 #18（W-11）：视图配置写成功即与写同事务发布结构事件。
	if err := service.publishStructureChanged(
		ctx,
		tx,
		table,
		"",
		structureChangeViewUpdated,
		principal.GetUserId(),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit UpdateViewConfig: %w", err)
	}
	config, err := decodeViewConfig(configPayload)
	if err != nil {
		return nil, err
	}
	return &structurev1.UpdateViewConfigResponse{ViewConfig: config}, nil
}
