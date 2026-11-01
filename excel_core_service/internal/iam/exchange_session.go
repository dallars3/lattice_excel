// iam/exchange_session.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
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
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	principal, err := service.sessions.Get(ctx, request.GetToken())
	if errors.Is(err, redisstore.ErrSessionNotFound) {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &iamv1.ExchangeSessionResponse{Principal: principal}, nil
}
