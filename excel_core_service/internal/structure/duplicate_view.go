package structure

import (
	"context"
	"fmt"
	"strings"

	"excel-libs/idgen"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
)

// DuplicateView copies view configuration without sharing mutable state.
func (service *Service) DuplicateView(
	ctx context.Context,
	request *structurev1.DuplicateViewRequest,
) (*structurev1.DuplicateViewResponse, error) {
	principal, _, err := service.memberPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	source, err := service.repository.ViewByID(ctx, request.GetViewId())
	if err != nil {
		return nil, mapStructureError(err)
	}
	if _, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		source.TableID,
	); err != nil {
		return nil, mapStructureError(err)
	}
	name, err := service.uniqueViewCopyName(ctx, source.TableID, source.Name)
	if err != nil {
		return nil, err
	}
	count, err := service.repository.CountViews(ctx, source.TableID)
	if err != nil {
		return nil, err
	}
	id, err := idgen.New()
	if err != nil {
		return nil, err
	}
	view := base.StructureView{
		ID:        id,
		TableID:   source.TableID,
		Name:      name,
		Type:      source.Type,
		Config:    append([]byte(nil), source.Config...),
		Position:  count,
		CreatedAt: service.now().UnixMilli(),
	}
	if err := service.repository.CreateView(ctx, view); err != nil {
		return nil, err
	}
	return &structurev1.DuplicateViewResponse{View: viewToProto(view)}, nil
}

func (service *Service) uniqueViewCopyName(
	ctx context.Context,
	tableID, sourceName string,
) (string, error) {
	for copyIndex := 1; copyIndex <= 100; copyIndex++ {
		suffix := " 的副本"
		if copyIndex > 1 {
			suffix = fmt.Sprintf(" 的副本%d", copyIndex)
		}
		baseName := sourceName
		maxBaseRunes := maxStructureNameRunes - len([]rune(suffix))
		runes := []rune(strings.TrimSpace(baseName))
		if len(runes) > maxBaseRunes {
			runes = runes[:maxBaseRunes]
		}
		name := string(runes) + suffix
		exists, err := service.repository.ViewNameExists(ctx, tableID, name, "")
		if err != nil {
			return "", err
		}
		if !exists {
			return name, nil
		}
	}
	return "", rpcError(1001)
}
