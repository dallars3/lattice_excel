// file/get_quota.go：MySQL 池账本与 Redis 计量对账。
package file

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	filev1 "excel/api/file/v1"
	"excel_worker_service/deal"
)

// GetQuota returns the authoritative pool usage and remaining capacity.
func (service *Service) GetQuota(
	ctx context.Context,
	_ *emptypb.Empty,
) (*filev1.GetQuotaResponse, error) {
	if service == nil || service.repository == nil || service.quota == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	used, err := service.repository.UsedBytes(ctx)
	if err != nil {
		return nil, err
	}
	if err := service.quota.Sync(ctx, used); err != nil {
		return nil, err
	}
	limit := service.quota.Limit()
	available := limit - used
	if available < 0 {
		available = 0
	}
	return &filev1.GetQuotaResponse{
		Used:      used,
		Limit:     limit,
		Available: available,
	}, nil
}
