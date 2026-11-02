// workspace/duplicate_base.go：一文件一接口。
package workspace

import (
	"context"
	"fmt"

	"excel-libs/idgen"
	workspacev1 "excel/api/workspace/v1"

	"excel_core_service/base"
	"excel_core_service/deal"
)

// DuplicateBase copies Base structure and table rows while leaving comments,
// history and audit data naturally empty in the new Base.
func (service *Service) DuplicateBase(
	ctx context.Context,
	request *workspacev1.DuplicateBaseRequest,
) (*workspacev1.DuplicateBaseResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetBaseId() == "" || service.repository == nil {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	source, err := service.repository.BaseByID(ctx, request.GetBaseId())
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	if err := service.requireBaseManager(ctx, principal, source); err != nil {
		return nil, err
	}
	if service.duplicator == nil || service.records == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	name, err := service.duplicateBaseName(ctx, source)
	if err != nil {
		return nil, err
	}
	baseID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	target := base.WorkspaceBase{
		ID:             baseID,
		EnterpriseID:   source.EnterpriseID,
		OwnerID:        principal.GetUserId(),
		Name:           name,
		MyRole:         "owner",
		LastModifiedAt: now.UnixMilli(),
		LastModifier:   principal.GetUserId(),
		CreatedAt:      now.UnixMilli(),
	}
	if err := service.repository.CreateBase(ctx, target); err != nil {
		return nil, mapWorkspaceError(err)
	}
	tableIDs, err := service.duplicator.DuplicateBase(
		ctx,
		source.EnterpriseID,
		source.ID,
		baseID,
	)
	if err != nil {
		service.cleanupFailedDuplicate(ctx, baseID, nil)
		return nil, err
	}
	for sourceTableID, targetTableID := range tableIDs {
		if err := service.records.DuplicateTableRows(
			ctx,
			sourceTableID,
			targetTableID,
		); err != nil {
			copiedTableIDs := make([]string, 0, len(tableIDs))
			for _, tableID := range tableIDs {
				copiedTableIDs = append(copiedTableIDs, tableID)
			}
			service.cleanupFailedDuplicate(ctx, baseID, copiedTableIDs)
			return nil, err
		}
	}
	persisted, err := service.repository.BaseByID(ctx, baseID)
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	persisted.MyRole = "owner"
	return &workspacev1.DuplicateBaseResponse{
		Base: workspaceBaseToProto(persisted),
	}, nil
}

func (service *Service) duplicateBaseName(
	ctx context.Context,
	source base.WorkspaceBase,
) (string, error) {
	for copyIndex := 1; copyIndex <= 100; copyIndex++ {
		suffix := " 的副本"
		if copyIndex > 1 {
			suffix = fmt.Sprintf(" 的副本%d", copyIndex)
		}
		name := truncatedCopyWithSuffix(source.Name, suffix)
		exists, err := service.repository.BaseNameExists(
			ctx,
			source.EnterpriseID,
			name,
			"",
		)
		if err != nil {
			return "", mapWorkspaceError(err)
		}
		if !exists {
			return name, nil
		}
	}
	return "", rpcError(deal.CodeConflict)
}

func (service *Service) cleanupFailedDuplicate(
	ctx context.Context,
	baseID string,
	tableIDs []string,
) {
	if service.structureCleanup != nil {
		_ = service.structureCleanup(ctx, baseID)
	}
	if service.recordDeleter != nil {
		_ = service.recordDeleter.DeleteBaseRows(ctx, baseID, tableIDs)
	}
	if service.repository != nil {
		_ = service.repository.DeleteBase(ctx, baseID)
	}
}
