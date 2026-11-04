// share/force_close_by_manager.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// ForceCloseByManager lets an enterprise manager close any open anyone link.
func (service *Service) ForceCloseByManager(
	ctx context.Context,
	request *sharev1.ForceCloseByManagerRequest,
) (*emptypb.Empty, error) {
	if request == nil || strings.TrimSpace(request.GetLinkId()) == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	link, err := service.lockLinkReference(ctx, tx, request.GetLinkId())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, deal.RPCError(deal.CodeInvalidArgument)
		}
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if link.Scope != "anyone" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	enterpriseID, err := service.bases.EnterpriseID(ctx, link.BaseID)
	if err != nil || enterpriseID == "" || principal.GetEnterpriseId() != enterpriseID {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	memberRole, err := service.members.Role(ctx, enterpriseID, principal.GetUserId())
	if err != nil ||
		(memberRole != iamv1.MemberRole_MEMBER_ROLE_OWNER &&
			memberRole != iamv1.MemberRole_MEMBER_ROLE_MANAGER) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if service.baseOwner == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	creatorID, err := service.baseOwner.OwnerID(ctx, link.BaseID)
	if err != nil || creatorID == "" {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	link.Enabled = false
	link.UpdatedAt = service.now().UTC().UnixMilli()
	if err := service.repository.UpsertLink(ctx, tx, link); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	eventID, err := service.enqueueLinkClosed(
		ctx,
		tx,
		linkClosedPayload{
			BaseID:        link.BaseID,
			LinkID:        link.BaseID,
			EnterpriseID:  enterpriseID,
			Operator:      principal.GetUserId(),
			Reason:        "manager_force_close",
			ManagerUserID: principal.GetUserId(),
			CreatorID:     creatorID,
		},
	)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueAuditAppend(
		ctx,
		tx,
		eventID,
		link.BaseID,
		enterpriseID,
		"AUDIT_ACTION_LINK_FORCE_CLOSED",
		principal.GetUserId(),
		link.BaseID,
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}

func (service *Service) lockLinkReference(
	ctx context.Context,
	tx *sql.Tx,
	reference string,
) (base.ShareLink, error) {
	link, err := service.repository.LinkByBaseIDForUpdate(ctx, tx, reference)
	if !errors.Is(err, sql.ErrNoRows) {
		return link, err
	}
	return service.repository.LinkByTokenForUpdate(ctx, tx, reference)
}
