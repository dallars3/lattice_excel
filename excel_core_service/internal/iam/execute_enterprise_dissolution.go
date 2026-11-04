// iam/execute_enterprise_dissolution.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// ExecuteEnterpriseDissolution completes an accepted request after its deadline.
func (service *IamService) ExecuteEnterpriseDissolution(
	ctx context.Context,
	request *iamv1.ExecuteEnterpriseDissolutionRequest,
) (*emptypb.Empty, error) {
	if request == nil || strings.TrimSpace(request.GetEnterpriseId()) == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	enterprise, err := service.repository.EnterpriseByIDForUpdate(
		ctx,
		tx,
		request.GetEnterpriseId(),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodeConflict)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if enterprise.DissolutionStatus == "done" {
		return &emptypb.Empty{}, nil
	}
	if enterprise.DissolutionStatus != "pending" ||
		service.now().UTC().UnixMilli() < enterprise.DissolutionDeadline {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	memberIDs, err := service.repository.EnterpriseMemberIDs(ctx, tx, enterprise.ID)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	var baseIDs []string
	if service.baseLister != nil {
		baseIDs, err = service.baseLister.ListBases(ctx, enterprise.ID)
		if err != nil {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
	}
	if err := service.repository.SetEnterpriseDissolutionTx(
		ctx,
		tx,
		enterprise.ID,
		"done",
		enterprise.DissolutionDeadline,
	); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.DetachEnterpriseDataTx(ctx, tx, enterprise.ID); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueEvent(ctx, tx, "enterprise.dissolved", struct {
		EnterpriseID string   `json:"enterprise_id"`
		DissolvedAt  int64    `json:"dissolved_at"`
		Note         string   `json:"note,omitempty"`
		BaseIDList   []string `json:"base_id_list"`
		MemberIDs    []string `json:"members_ids"`
	}{
		EnterpriseID: enterprise.ID,
		DissolvedAt:  service.now().UTC().UnixMilli(),
		BaseIDList:   baseIDs,
		MemberIDs:    memberIDs,
	}); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
