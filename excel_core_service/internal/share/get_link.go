// share/get_link.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"
	"database/sql"
	"errors"

	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// GetLink returns the current Base link configuration.
func (service *Service) GetLink(
	ctx context.Context,
	request *sharev1.GetLinkRequest,
) (*sharev1.GetLinkResponse, error) {
	if request == nil || request.GetBaseId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if _, err := service.requireAction(
		ctx,
		request.GetBaseId(),
		sharev1.Action_ACTION_MANAGE_SHARE,
	); err != nil {
		return nil, err
	}
	link, err := service.repository.LinkByBaseID(ctx, request.GetBaseId())
	switch {
	case errors.Is(err, sql.ErrNoRows):
		link = base.ShareLink{
			BaseID: request.GetBaseId(),
			Scope:  "none",
		}
	case err != nil:
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &sharev1.GetLinkResponse{LinkConfig: protoLinkConfig(link)}, nil
}
