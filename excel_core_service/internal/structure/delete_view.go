package structure

import (
	"context"
	"excel_core_service/deal"

	"google.golang.org/protobuf/types/known/emptypb"

	structurev1 "excel/api/structure/v1"
)

// DeleteView removes one view while retaining at least one view per table.
func (service *Service) DeleteView(
	ctx context.Context,
	request *structurev1.DeleteViewRequest,
) (*emptypb.Empty, error) {
	principal, _, err := service.memberPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := service.repository.ViewByID(ctx, request.GetViewId())
	if err != nil {
		return nil, mapStructureError(err)
	}
	if _, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		view.TableID,
	); err != nil {
		return nil, mapStructureError(err)
	}
	count, err := service.repository.CountViews(ctx, view.TableID)
	if err != nil {
		return nil, err
	}
	if count <= 1 {
		return nil, deal.RPCError(1001)
	}
	if err := service.repository.DeleteView(ctx, view.ID); err != nil {
		return nil, mapStructureError(err)
	}
	return &emptypb.Empty{}, nil
}
