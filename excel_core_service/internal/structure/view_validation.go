// structure/view_validation.go：视图配置编码与容量校验。
package structure

import (
	"context"
	"errors"
	"excel_core_service/deal"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
)

const (
	maxViewFilters = 5
	maxViewSorts   = 3
)

func encodeViewConfig(config *structurev1.ViewConfig) ([]byte, error) {
	if config == nil {
		config = &structurev1.ViewConfig{}
	}
	payload, err := protojson.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode structure view config: %w", err)
	}
	return payload, nil
}

func decodeViewConfig(payload []byte) (*structurev1.ViewConfig, error) {
	config := &structurev1.ViewConfig{}
	if len(payload) == 0 {
		return config, nil
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(payload, config); err != nil {
		return nil, fmt.Errorf("decode structure view config: %w", err)
	}
	return config, nil
}

func (service *Service) validateViewConfig(
	ctx context.Context,
	tableID string,
	config *structurev1.ViewConfig,
) error {
	if config == nil {
		return nil
	}
	if len(config.GetFilterList()) > maxViewFilters ||
		len(config.GetSortList()) > maxViewSorts ||
		len(config.GetGroupByList()) > 1 {
		return deal.RPCError(1001)
	}
	if config.GetRowHeight() < structurev1.RowHeight_ROW_HEIGHT_UNSPECIFIED ||
		config.GetRowHeight() > structurev1.RowHeight_ROW_HEIGHT_TALL {
		return deal.RPCError(1001)
	}
	fields, err := service.repository.FieldsByTable(ctx, tableID)
	if err != nil {
		return err
	}
	fieldByID := make(map[string]base.StructureField, len(fields))
	for _, field := range fields {
		fieldByID[field.ID] = field
	}
	for _, fieldID := range config.GetHiddenFieldList() {
		field, ok := fieldByID[fieldID]
		if !ok || field.IsPrimary {
			return deal.RPCError(1001)
		}
	}
	for _, fieldID := range config.GetFrozenFieldList() {
		if _, ok := fieldByID[fieldID]; !ok {
			return deal.RPCError(1001)
		}
	}
	for _, filter := range config.GetFilterList() {
		if filter == nil || filter.GetOp() <= structurev1.ViewFilterOp_VIEW_FILTER_OP_UNSPECIFIED ||
			filter.GetOp() > structurev1.ViewFilterOp_VIEW_FILTER_OP_NONE {
			return deal.RPCError(1001)
		}
		if _, ok := fieldByID[filter.GetFieldId()]; !ok {
			return deal.RPCError(1001)
		}
	}
	for _, sort := range config.GetSortList() {
		if sort == nil || (sort.GetDir() != structurev1.SortDir_SORT_DIR_ASC &&
			sort.GetDir() != structurev1.SortDir_SORT_DIR_DESC) {
			return deal.RPCError(1001)
		}
		if _, ok := fieldByID[sort.GetFieldId()]; !ok {
			return deal.RPCError(1001)
		}
	}
	for _, group := range config.GetGroupByList() {
		if group == nil ||
			group.GetGranularity() < structurev1.Granularity_GRANULARITY_UNSPECIFIED ||
			group.GetGranularity() > structurev1.Granularity_GRANULARITY_DAY {
			return deal.RPCError(1001)
		}
		if _, ok := fieldByID[group.GetFieldId()]; !ok {
			return deal.RPCError(1001)
		}
	}
	return nil
}

func requireStructureName(name string) (string, error) {
	normalized, err := normalizeStructureName(name)
	if err != nil {
		return "", errors.New("参数缺失或非法")
	}
	return normalized, nil
}
