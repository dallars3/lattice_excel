package structure

import (
	"context"
	"excel_core_service/deal"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
)

// UpdateOptions replaces select options and the allow-create switch.
func (service *Service) UpdateOptions(
	ctx context.Context,
	request *structurev1.UpdateOptionsRequest,
) (*structurev1.UpdateOptionsResponse, error) {
	principal, _, err := service.managerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	field, err := service.repository.FieldByID(ctx, request.GetFieldId())
	if err != nil {
		return nil, mapStructureError(err)
	}
	if field.Type != "FIELD_TYPE_SELECT" && field.Type != "FIELD_TYPE_MULTISELECT" {
		return nil, deal.RPCError(1001)
	}
	if _, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		field.TableID,
	); err != nil {
		return nil, mapStructureError(err)
	}
	optionList := make([]any, 0, len(request.GetOptionList()))
	for _, option := range request.GetOptionList() {
		optionList = append(optionList, map[string]any{
			"id":    option.GetId(),
			"name":  option.GetName(),
			"color": option.GetColor(),
		})
	}
	config, options, err := normalizeSelectConfig(map[string]any{
		"option_list":  optionList,
		"allow_create": request.GetAllowCreate(),
	})
	if err != nil {
		return nil, deal.RPCError(1001)
	}
	payload, err := base.EncodeStructureConfig(config)
	if err != nil {
		return nil, err
	}
	field.Config = payload
	if err := service.repository.UpdateField(ctx, field, options); err != nil {
		return nil, err
	}
	projected, err := fieldToProto(field)
	if err != nil {
		return nil, err
	}
	return &structurev1.UpdateOptionsResponse{Field: projected}, nil
}
