// records/update_cell.go：乐观锁单元格更新与同行公式同步求值。
package records

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	"excel-libs/idgen"
	calcv1 "excel/api/calc/v1"
	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// UpdateCell applies one optimistic-locked cell mutation.
func (service *Service) UpdateCell(
	ctx context.Context,
	request *recordsv1.UpdateCellRequest,
) (*recordsv1.UpdateCellResponse, error) {
	if service == nil || service.repository == nil || request == nil ||
		request.GetRecordId() == "" || request.GetFieldId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	scope, err := rowScopeFromContext(ctx)
	if err != nil {
		return nil, err
	}

	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	state, err := service.repository.RecordByIDForUpdate(
		ctx,
		tx,
		request.GetRecordId(),
	)
	if errors.Is(err, base.ErrRecordNotFound) || state.SoftDeletedAt != nil {
		return nil, deal.RPCError(deal.CodeDeletedResource)
	}
	if err != nil {
		return nil, err
	}
	if scope == rowScopeOwn && state.CreatedBy != principal.GetUserId() {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if err := service.validateTableBaseMetadata(ctx, state.TableID); err != nil {
		return nil, err
	}
	if request.GetExpectVersion() != 0 &&
		request.GetExpectVersion() != state.Version {
		// 票 02：4001 data 键对齐接口文档 v0.7 §8.6.3（their_value/their_version），
		// 前端 parseConflictMeta 依赖同键触发三动作冲突弹窗。
		return nil, rpcErrorWithData(deal.CodeConcurrentConflict, map[string]any{
			"their_value":   commonValueAny(state.Cells[request.GetFieldId()]),
			"their_version": state.Version,
		})
	}
	fields, err := service.structure.Fields(ctx, state.TableID)
	if err != nil {
		return nil, mapRecordError(err)
	}
	field := fieldByID(fields)[request.GetFieldId()]
	if field == nil || field.GetTableId() != state.TableID ||
		!fieldIsWritable(field.GetType()) {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if err := validateFieldValue(field, request.GetValue()); err != nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	next, err := service.updateLockedCell(
		ctx,
		tx,
		state,
		field,
		request.GetValue(),
		principal.GetUserId(),
		"update",
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &recordsv1.UpdateCellResponse{
		Value:   request.GetValue(),
		Version: next.Version,
	}, nil
}

type formulaChange struct {
	FieldID string
	Before  *commonv1.Value
	After   *commonv1.Value
	Status  string
	Reason  string
}

func (service *Service) evaluateFormulaClosure(
	ctx context.Context,
	tx *sql.Tx,
	record *base.Record,
	fields []*structurev1.Field,
	changed []*structurev1.Field,
) ([]formulaChange, error) {
	changedNames := make(map[string]struct{}, len(changed))
	for _, field := range changed {
		changedNames[field.GetName()] = struct{}{}
	}
	selected := make(map[string]struct{})
	dependent := make([]*structurev1.Field, 0)
	for pass := 0; pass < len(fields); pass++ {
		progress := false
		for _, field := range fields {
			if field.GetType() != structurev1.FieldType_FIELD_TYPE_FORMULA {
				continue
			}
			if _, exists := selected[field.GetId()]; exists {
				continue
			}
			expression, err := formulaExpression(field)
			if err != nil {
				return nil, rpcErrorWithData(deal.CodeInvalidArgument, map[string]any{
					"formula_errors": map[string]any{field.GetId(): err.Error()},
				})
			}
			if !referencesAny(formulaReferences(expression), changedNames) {
				continue
			}
			dependent = append(dependent, field)
			selected[field.GetId()] = struct{}{}
			changedNames[field.GetName()] = struct{}{}
			progress = true
		}
		if !progress {
			break
		}
	}
	if len(dependent) == 0 {
		return nil, nil
	}
	if service.evaluator == nil || service.computed == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	changes := make([]formulaChange, 0, len(dependent))
	for _, field := range dependent {
		expression, err := formulaExpression(field)
		if err != nil {
			return nil, deal.RPCError(deal.CodeInvalidArgument)
		}
		response, err := service.evaluator.Evaluate(
			ctx,
			evaluationRequest(fields, record.Cells, expression),
		)
		if err != nil {
			return nil, rpcErrorWithData(deal.CodeInvalidArgument, map[string]any{
				"formula_errors": map[string]any{field.GetId(): err.Error()},
			})
		}
		if response == nil || len(response.GetResultList()) != 1 ||
			response.GetResultList()[0] == nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		result := response.GetResultList()[0]
		// 工单 09（缺陷 #17，接口文档 §8.5.4）：公式计算失败（除零/空值/运行
		// 期操作数不兼容等）不阻断记录写入，该格按字段语义落 `#ERROR:<原因>`
		// 哨兵；定义类错误（环/字段不存在/类型不匹配）仍在保存校验阶段拒绝。
		value := result.GetValue()
		changeStatus := "IDLE"
		changeReason := ""
		if result.GetIsError() {
			changeReason = result.GetReason()
			value = formulaErrorCellValue(changeReason)
			changeStatus = "ERROR"
		}
		if value == nil {
			value = &commonv1.Value{}
		}
		before := record.Cells[field.GetId()]
		setRecordCell(record.Cells, field.GetId(), value)
		changes = append(changes, formulaChange{
			FieldID: field.GetId(),
			Before:  before,
			After:   value,
			Status:  changeStatus,
			Reason:  changeReason,
		})
	}
	for _, change := range changes {
		if err := service.writeComputedChange(
			withTransaction(ctx, tx),
			record.ID,
			change,
		); err != nil {
			return nil, err
		}
	}
	return changes, nil
}

// writeComputedChange persists one same-row computed cell. Error sentinels
// carry the materialization status and reason (W-14 数据面) when the injected
// writer supports the status seam; plain values keep the IDLE default.
func (service *Service) writeComputedChange(
	ctx context.Context,
	recordID string,
	change formulaChange,
) error {
	if change.Status == "ERROR" {
		if statusWriter, ok := service.computed.(ComputedStatusWriter); ok {
			return statusWriter.WriteComputedStatus(
				ctx,
				recordID,
				change.FieldID,
				change.After,
				change.Status,
				change.Reason,
			)
		}
	}
	return service.computed.WriteComputed(
		ctx,
		recordID,
		change.FieldID,
		change.After,
	)
}

// formulaErrorCellValue encodes a formula evaluation failure as the durable
// sentinel string `#ERROR:<原因>`（无原因时为 `#ERROR`）。与 calc 模块的
// errorCellValue 同一口径；records 不跨模块引用 calc，故在此收口编码规则。
func formulaErrorCellValue(reason string) *commonv1.Value {
	if strings.TrimSpace(reason) == "" {
		return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: "#ERROR"}}
	}
	return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: "#ERROR:" + reason}}
}

func evaluationRequest(
	fields []*structurev1.Field,
	cells map[string]*commonv1.Value,
	expression string,
) *calcv1.EvaluateRequest {
	schema := make(map[string]any, len(fields))
	row := make(map[string]any, len(fields))
	for _, field := range fields {
		schema[field.GetName()] = field.GetType().String()
		if value := cells[field.GetId()]; value != nil {
			row[field.GetName()] = commonValueAny(value)
		}
	}
	schemaMap, _ := structpb.NewStruct(schema)
	rowMap, _ := structpb.NewStruct(row)
	return &calcv1.EvaluateRequest{
		ExpressionList: []string{expression},
		SchemaMap:      schemaMap,
		RowContextMap:  rowMap,
	}
}

func formulaExpression(field *structurev1.Field) (string, error) {
	if field == nil || field.GetConfigMap() == nil {
		return "", errors.New("formula config is required")
	}
	value := field.GetConfigMap().GetFields()["expression"]
	expression := strings.TrimSpace(value.GetStringValue())
	if expression == "" {
		return "", errors.New("formula expression is required")
	}
	return expression, nil
}

func referencesAny(references []string, names map[string]struct{}) bool {
	for _, reference := range references {
		if _, exists := names[reference]; exists {
			return true
		}
	}
	return false
}

func newRecordULID() (string, error) {
	return idgen.New()
}
