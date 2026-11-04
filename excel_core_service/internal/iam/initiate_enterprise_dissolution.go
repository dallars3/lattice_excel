// iam/initiate_enterprise_dissolution.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

const enterpriseDissolutionCooldown = 30 * 24 * time.Hour

// InitiateEnterpriseDissolution accepts a manual enterprise deletion request.
func (service *IamService) InitiateEnterpriseDissolution(
	ctx context.Context,
	request *iamv1.InitiateEnterpriseDissolutionRequest,
) (*iamv1.InitiateEnterpriseDissolutionResponse, error) {
	if err := service.requireAdmin(ctx); err != nil {
		return nil, err
	}
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
	now := service.now().UTC()
	switch enterprise.DissolutionStatus {
	case "done":
		return nil, rpcError(deal.CodePermissionDenied)
	case "pending":
		if enterprise.DissolutionDeadline > now.UnixMilli() {
			return &iamv1.InitiateEnterpriseDissolutionResponse{
				DeadlineMs: enterprise.DissolutionDeadline,
			}, nil
		}
	case "revoked":
		if enterprise.DissolutionDeadline > now.UnixMilli() {
			return nil, rpcError(deal.CodePermissionDenied)
		}
	}
	activeOthers, err := service.repository.CountOtherActiveEnterpriseMembers(
		ctx,
		tx,
		enterprise.ID,
		enterprise.OwnerID,
	)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if activeOthers > 0 {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	deadline := now.Add(enterpriseDissolutionCooldown).UnixMilli()
	if err := service.repository.SetEnterpriseDissolutionTx(
		ctx,
		tx,
		enterprise.ID,
		"pending",
		deadline,
	); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueAudit(
		ctx,
		tx,
		"AUDIT_ACTION_ENTERPRISE_DISSOLUTION_ACCEPTED",
		enterprise.ID,
		map[string]any{
			"deadline_ms": deadline,
			"note":        strings.TrimSpace(request.GetNote()),
		},
	); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &iamv1.InitiateEnterpriseDissolutionResponse{DeadlineMs: deadline}, nil
}
