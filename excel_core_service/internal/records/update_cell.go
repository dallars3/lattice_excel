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
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
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
		return nil, rpcError(deal.CodeDeletedResource)
	}
	if err != nil {
		return nil, err
	}
	if scope == rowScopeOwn && state.CreatedBy != principal.GetUserId() {
		return nil, rpcError(deal.CodeDeletedResource)
	}
	if err := service.validateTableBaseMetadata(ctx, state.TableID); err != nil {
		return nil, err
	}
	if request.GetExpectVersion() != 0 &&
		request.GetExpectVersion() != state.Version {
		return nil, rpcErrorWithData(deal.CodeConcurrentConflict, map[string]any{
			"current_value":   commonValueAny(state.Cells[request.GetFieldId()]),
			"current_version": state.Version,
		})
	}
	fields, err := service.structure.Fields(ctx, state.TableID)
	if err != nil {
		return nil, mapRecordError(err)
	}
	field := fieldByID(fields)[request.GetFieldId()]
	if field == nil || field.GetTableId() != state.TableID ||
		!fieldIsWritable(field.GetType()) {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if err := validateFieldValue(field, request.GetValue()); err != nil {
		return nil, rpcError(deal.CodeInvalidArgument)
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
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	changes := make([]formulaChange, 0, len(dependent))
	for _, field := range dependent {
		expression, err := formulaExpression(field)
		if err != nil {
			return nil, rpcError(deal.CodeInvalidArgument)
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
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
		result := response.GetResultList()[0]
		if result.GetIsError() {
			return nil, rpcErrorWithData(deal.CodeInvalidArgument, map[string]any{
				"formula_errors": map[string]any{field.GetId(): result.GetReason()},
			})
		}
		value := result.GetValue()
		if value == nil {
			value = &commonv1.Value{}
		}
		before := record.Cells[field.GetId()]
		setRecordCell(record.Cells, field.GetId(), value)
		changes = append(changes, formulaChange{
			FieldID: field.GetId(),
			Before:  before,
			After:   value,
		})
	}
	for _, change := range changes {
		if err := service.computed.WriteComputed(
			withTransaction(ctx, tx),
			record.ID,
			change.FieldID,
			*change.After,
		); err != nil {
			return nil, err
		}
	}
	return changes, nil
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
