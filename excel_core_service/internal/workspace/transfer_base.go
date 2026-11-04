// workspace/transfer_base.go：一文件一接口。
package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"excel-libs/eventbus"
	"excel-libs/idgen"
	iamv1 "excel/api/iam/v1"
	workspacev1 "excel/api/workspace/v1"
	"google.golang.org/protobuf/types/known/emptypb"

	"excel_core_service/base"
	"excel_core_service/deal"
)

// TransferBase changes the Base owner while preserving stars and recent access.
func (service *Service) TransferBase(
	ctx context.Context,
	request *workspacev1.TransferBaseRequest,
) (*emptypb.Empty, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetBaseId() == "" || request.GetNewOwnerId() == "" ||
		service.repository == nil || service.roles == nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	item, err := service.repository.BaseByID(ctx, request.GetBaseId())
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	if err := service.requireBaseManager(ctx, principal, item); err != nil {
		return nil, err
	}
	targetRole, err := service.roles.Role(
		ctx,
		item.EnterpriseID,
		request.GetNewOwnerId(),
	)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if targetRole == iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.TransferBaseTx(
		ctx,
		tx,
		item.ID,
		request.GetNewOwnerId(),
		principal.GetUserId(),
		service.now(),
	); err != nil {
		return nil, mapWorkspaceError(err)
	}
	if service.outbox != nil {
		if err := service.enqueueBaseTransferred(
			ctx,
			tx,
			item,
			request.GetNewOwnerId(),
			principal.GetUserId(),
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit Base transfer: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (service *Service) enqueueBaseTransferred(
	ctx context.Context,
	tx *sql.Tx,
	item base.WorkspaceBase,
	newOwnerID, operator string,
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
		Action:   "AUDIT_ACTION_BASE_OWNERSHIP_TRANSFERRED",
		Operator: operator,
		Target:   item.ID,
		BaseID:   item.ID,
		DetailMap: map[string]any{
			"enterprise_id":     item.EnterpriseID,
			"previous_owner_id": item.OwnerID,
			"new_owner_id":      newOwnerID,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal Base transfer audit payload: %w", err)
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
		return fmt.Errorf("enqueue Base transfer audit: %w", err)
	}
	return nil
}
