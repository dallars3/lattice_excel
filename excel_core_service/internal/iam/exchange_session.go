// iam/exchange_session.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
	redisstore "excel_core_service/redis"
)

// ExchangeSession is the sole issuer of Principal values for an opaque session ticket.
func (service *IamService) ExchangeSession(
	ctx context.Context,
	request *iamv1.ExchangeSessionRequest,
) (*iamv1.ExchangeSessionResponse, error) {
	if request == nil || request.GetToken() == "" {
		return nil, deal.RPCError(deal.CodeUnauthenticated)
	}
	principal, err := service.sessions.Get(ctx, request.GetToken())
	if errors.Is(err, redisstore.ErrSessionNotFound) {
		return nil, deal.RPCError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	user, err := service.repository.UserByID(ctx, principal.GetUserId())
	if errors.Is(err, sql.ErrNoRows) {
		if err := service.sessions.Delete(ctx, request.GetToken()); err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		return nil, deal.RPCError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	principalChanged := false
	if principal.GetEnterpriseId() != user.EnterpriseID {
		principal.EnterpriseId = user.EnterpriseID
		principalChanged = true
	}
	if principal.GetInternal() != user.Internal {
		principal.Internal = user.Internal
		principalChanged = true
	}
	if principal.GetEnterpriseId() != "" {
		member, err := service.repository.Member(
			ctx,
			principal.GetEnterpriseId(),
			principal.GetUserId(),
		)
		if errors.Is(err, sql.ErrNoRows) || err == nil && member.Status != "active" {
			if err := service.sessions.Delete(ctx, request.GetToken()); err != nil {
				return nil, deal.RPCError(deal.CodeServiceUnavailable)
			}
			return nil, deal.RPCError(deal.CodeUnauthenticated)
		}
		if err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
	}
	if principalChanged {
		if err := service.sessions.Save(ctx, request.GetToken(), principal); err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
	}
	return &iamv1.ExchangeSessionResponse{Principal: principal}, nil
}
