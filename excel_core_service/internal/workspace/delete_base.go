// workspace/delete_base.go：一文件一接口。
package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"excel-libs/eventbus"
	"excel-libs/idgen"
	workspacev1 "excel/api/workspace/v1"
	"google.golang.org/protobuf/types/known/emptypb"

	"excel_core_service/base"
	"excel_core_service/deal"
)

// DeleteBase owner/manager deletes one Base after all cross-module cleanup and
// appends the deletion audit event in the final workspace transaction.
func (service *Service) DeleteBase(
	ctx context.Context,
	request *workspacev1.DeleteBaseRequest,
) (*emptypb.Empty, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetBaseId() == "" || !request.GetConfirm() || service.repository == nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	item, err := service.repository.BaseByID(ctx, request.GetBaseId())
	if errors.Is(err, base.ErrWorkspaceNotFound) {
		// DELETE 语义幂等：Base 已不存在时返回空信封，重复删除不报服务错误。
		return &emptypb.Empty{}, nil
	}
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	if err := service.requireBaseManager(ctx, principal, item); err != nil {
		return nil, err
	}
	if service.structureCleanup == nil || service.outbox == nil ||
		service.recordDeleter == nil || service.comments == nil ||
		service.linksCleaner == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	tableIDs, err := service.baseTableIDs(ctx, item.ID)
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	// 评论必须先于记录清理：评论表不冗余 Base/表归属，只能经 record_id 关联
	// 记录与结构表；记录硬删除后再清评论会失去归属并留下孤儿。
	if err := service.comments.DeleteBaseComments(ctx, item.ID); err != nil {
		return nil, fmt.Errorf("delete Base comments: %w", err)
	}
	if len(tableIDs) > 0 {
		if err := service.recordDeleter.DeleteBaseRows(ctx, item.ID, tableIDs); err != nil {
			return nil, fmt.Errorf("delete Base records: %w", err)
		}
	}
	if err := service.linksCleaner.DeleteAllForBase(ctx, item.ID); err != nil {
		return nil, fmt.Errorf("delete Base links: %w", err)
	}
	if err := service.structureCleanup(ctx, item.ID); err != nil {
		return nil, fmt.Errorf("delete Base structure: %w", err)
	}

	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	defer func() { _ = tx.Rollback() }()
	skipped := make([]string, 0)
	if err := service.enqueueBaseDeleted(
		ctx,
		tx,
		item,
		principal.GetUserId(),
		skipped,
	); err != nil {
		return nil, err
	}
	if err := service.repository.DeleteBaseTx(ctx, tx, item.ID); err != nil {
		if errors.Is(err, base.ErrWorkspaceNotFound) {
			return &emptypb.Empty{}, nil
		}
		return nil, mapWorkspaceError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit Base deletion: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (service *Service) baseTableIDs(
	ctx context.Context,
	baseID string,
) ([]string, error) {
	return service.repository.TableIDsForBase(ctx, baseID)
}

func (service *Service) enqueueBaseDeleted(
	ctx context.Context,
	tx *sql.Tx,
	item base.WorkspaceBase,
	operator string,
	skipped []string,
) error {
	eventID, err := idgen.New()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		Action    string         `json:"action"`
		Operator  string         `json:"operator"`
		Target    string         `json:"target"`
		BaseID    string         `json:"base_id"`
		DetailMap map[string]any `json:"detail_map"`
	}{
		Action:   "AUDIT_ACTION_BASE_DELETED",
		Operator: operator,
		Target:   item.ID,
		BaseID:   item.ID,
		DetailMap: map[string]any{
			"enterprise_id":    item.EnterpriseID,
			"skipped_cleaners": skipped,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal Base deletion audit payload: %w", err)
	}
	if err := service.outbox.Enqueue(ctx, tx, eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "audit.append",
			Version:    1,
			BaseID:     item.ID,
			Seq:        0,
			OccurredAt: service.now().UTC().UnixMilli(),
			Producer:   "workspace",
		},
		Payload: payload,
	}); err != nil {
		return fmt.Errorf("enqueue Base deletion audit: %w", err)
	}
	return nil
}
