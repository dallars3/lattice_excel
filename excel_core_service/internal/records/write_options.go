// records/write_options.go：写链 seam、事件、幂等与结构适配。
package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"excel-libs/eventbus"
	"excel-libs/idgen"
	calcv1 "excel/api/calc/v1"
	commonv1 "excel/api/common/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

const idempotencyWindow = 24 * time.Hour

// FormulaEvaluator is the calc module's synchronous same-row evaluation seam.
type FormulaEvaluator interface {
	Evaluate(
		ctx context.Context,
		request *calcv1.EvaluateRequest,
	) (*calcv1.EvaluateResponse, error)
}

// ComputedWriter persists formula materializations. Writers participating in a
// records transaction can obtain the active transaction from TransactionFromContext.
type ComputedWriter interface {
	WriteComputed(
		ctx context.Context,
		recordID, fieldID string,
		value *commonv1.Value,
	) error
}

// BaseContextProvider resolves the Base and tenant for one table.
type BaseContextProvider interface {
	ContextByTable(
		ctx context.Context,
		tableID string,
	) (baseID, enterpriseID string, err error)
}

// EventOutbox is the transactional event seam implemented by bus.Outbox.
type EventOutbox interface {
	Enqueue(ctx context.Context, tx *sql.Tx, event eventbus.Event) error
}

// WriterOptions contains the optional write-chain dependencies. The original
// five-argument NewService call remains valid for read-only tests and callers.
type WriterOptions struct {
	Outbox         EventOutbox
	Bases          BaseContextProvider
	Evaluator      FormulaEvaluator
	ComputedWriter ComputedWriter
}

// FieldAccessor is an optional extension implemented by the production
// structure accessor for batch operations that start from a field id.
type FieldAccessor interface {
	Field(ctx context.Context, fieldID string) (*structurev1.Field, error)
}

// CrossTableDependencyProvider reports whether a changed field feeds lookup or
// formula work in another table.
type CrossTableDependencyProvider interface {
	HasCrossTableDependents(
		ctx context.Context,
		baseID, tableID, fieldID string,
	) (bool, error)
}

type transactionContextKey struct{}

// TransactionFromContext lets an injected ComputedWriter persist on the same
// business transaction as the records mutation.
func TransactionFromContext(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(transactionContextKey{}).(*sql.Tx)
	return tx, ok && tx != nil
}

func withTransaction(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, transactionContextKey{}, tx)
}

func recordChangedEvent(
	baseID, tableID, recordID, fieldID string,
	value *commonv1.Value,
	version int64,
	operator, changeType string,
	occurredAt time.Time,
	producer string,
) (eventbus.Event, error) {
	payloadMap := map[string]any{
		"record_id":   recordID,
		"version":     version,
		"operator":    operator,
		"change_type": changeType,
		"table_id":    tableID,
	}
	if fieldID != "" {
		payloadMap["field_id"] = fieldID
		payloadMap["value"] = commonValueAny(value)
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("marshal record.changed payload: %w", err)
	}
	eventID, err := idgen.New()
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("generate record.changed event id: %w", err)
	}
	if producer == "" {
		producer = "records"
	}
	return eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "record.changed",
			Version:    1,
			BaseID:     baseID,
			OccurredAt: occurredAt.UnixMilli(),
			Producer:   producer,
		},
		Payload: payload,
	}, nil
}

func recordDeletedEvent(
	baseID, tableID, enterpriseID string,
	recordIDs []string,
	occurredAt time.Time,
) (eventbus.Event, error) {
	payload, err := json.Marshal(map[string]any{
		"record_id_list": recordIDs,
		"enterprise_id":  enterpriseID,
		"table_id":       tableID,
	})
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("marshal record.deleted payload: %w", err)
	}
	eventID, err := idgen.New()
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("generate record.deleted event id: %w", err)
	}
	return eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "record.deleted",
			Version:    1,
			BaseID:     baseID,
			OccurredAt: occurredAt.UnixMilli(),
			Producer:   "records",
		},
		Payload: payload,
	}, nil
}

func (service *Service) enqueueCalcJobIfNeeded(
	ctx context.Context,
	tx *sql.Tx,
	baseID, tableID, recordID, fieldID string,
) error {
	provider, ok := service.structure.(CrossTableDependencyProvider)
	if !ok {
		return nil
	}
	dependent, err := provider.HasCrossTableDependents(
		ctx,
		baseID,
		tableID,
		fieldID,
	)
	if err != nil {
		return err
	}
	if !dependent {
		return nil
	}
	jobID, err := idgen.New()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"job_id":        jobID,
		"table_id":      tableID,
		"field_id_list": []string{fieldID},
		"trigger": map[string]string{
			"record_id": recordID,
			"field_id":  fieldID,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal calc.job payload: %w", err)
	}
	eventID, err := idgen.New()
	if err != nil {
		return err
	}
	return service.outbox.Enqueue(ctx, tx, eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "calc.job",
			Version:    1,
			BaseID:     baseID,
			OccurredAt: service.now().UnixMilli(),
			Producer:   "records",
		},
		Payload: payload,
	})
}

func formulaReferences(expression string) []string {
	references := make([]string, 0)
	seen := make(map[string]struct{})
	for index := 0; index < len(expression); index++ {
		if expression[index] != '{' {
			continue
		}
		end := strings.IndexByte(expression[index+1:], '}')
		if end < 0 {
			break
		}
		name := strings.TrimSpace(expression[index+1 : index+1+end])
		index += end + 1
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		references = append(references, name)
	}
	return references
}

func cloneRecordCells(
	cells map[string]*commonv1.Value,
) map[string]*commonv1.Value {
	clone := make(map[string]*commonv1.Value, len(cells))
	for fieldID, value := range cells {
		clone[fieldID] = value
	}
	return clone
}

func setRecordCell(
	cells map[string]*commonv1.Value,
	fieldID string,
	value *commonv1.Value,
) {
	if isClearedCell(value) {
		delete(cells, fieldID)
		return
	}
	cells[fieldID] = value
}

func isClearedCell(value *commonv1.Value) bool {
	return value == nil || value.GetKind() == nil
}

func fieldIsWritable(fieldType structurev1.FieldType) bool {
	return fieldType != structurev1.FieldType_FIELD_TYPE_FORMULA &&
		fieldType != structurev1.FieldType_FIELD_TYPE_LOOKUP
}

func validateFieldValue(
	field *structurev1.Field,
	value *commonv1.Value,
) error {
	if field == nil {
		return errors.New("field is required")
	}
	if !fieldIsWritable(field.GetType()) {
		return errors.New("field is read-only")
	}
	if isClearedCell(value) {
		return nil
	}
	switch field.GetType() {
	case structurev1.FieldType_FIELD_TYPE_TEXT:
		text, ok := value.GetKind().(*commonv1.Value_StringValue)
		if !ok || utf8.RuneCountInString(text.StringValue) > 10000 {
			return errors.New("text value must be a string")
		}
	case structurev1.FieldType_FIELD_TYPE_URL:
		text, ok := value.GetKind().(*commonv1.Value_StringValue)
		if !ok || !validHTTPURL(text.StringValue) {
			return errors.New("url value must use http or https")
		}
	case structurev1.FieldType_FIELD_TYPE_NUMBER:
		switch kind := value.GetKind().(type) {
		case *commonv1.Value_IntValue:
		case *commonv1.Value_DoubleValue:
			if math.IsNaN(kind.DoubleValue) || math.IsInf(kind.DoubleValue, 0) {
				return errors.New("number value must be finite")
			}
		default:
			return errors.New("number value must be numeric")
		}
	case structurev1.FieldType_FIELD_TYPE_DATE:
		var millis int64
		switch kind := value.GetKind().(type) {
		case *commonv1.Value_IntValue:
			millis = kind.IntValue
		case *commonv1.Value_DateValueMs:
			millis = kind.DateValueMs
		default:
			return errors.New("date value must be milliseconds")
		}
		minimum := time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
		if millis < minimum {
			return errors.New("date value predates 1900-01-01")
		}
	case structurev1.FieldType_FIELD_TYPE_SELECT:
		optionID, ok := value.GetKind().(*commonv1.Value_StringValue)
		if !ok || !fieldHasOption(field, optionID.StringValue) {
			return errors.New("select option does not exist")
		}
	case structurev1.FieldType_FIELD_TYPE_MULTISELECT:
		items, ok := value.GetKind().(*commonv1.Value_ListValue)
		if !ok {
			return errors.New("multiselect value must be a list")
		}
		for _, item := range items.ListValue.GetValueList() {
			if !fieldHasOption(field, item.GetStringValue()) {
				return errors.New("multiselect option does not exist")
			}
		}
	case structurev1.FieldType_FIELD_TYPE_PERSON,
		structurev1.FieldType_FIELD_TYPE_RELATION:
		if !isStringOrStringList(value) {
			return errors.New("value must be an id or id list")
		}
	case structurev1.FieldType_FIELD_TYPE_ATTACHMENT:
		items, ok := value.GetKind().(*commonv1.Value_ListValue)
		if !ok || len(items.ListValue.GetValueList()) > 10 {
			return errors.New("attachment value must contain at most 10 ids")
		}
		for _, item := range items.ListValue.GetValueList() {
			if item.GetStringValue() == "" {
				return errors.New("attachment ids must be strings")
			}
		}
	default:
		return errors.New("unsupported field type")
	}
	return nil
}

func isStringOrStringList(value *commonv1.Value) bool {
	switch kind := value.GetKind().(type) {
	case *commonv1.Value_StringValue:
		return kind.StringValue != ""
	case *commonv1.Value_ListValue:
		for _, item := range kind.ListValue.GetValueList() {
			if item.GetStringValue() == "" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func fieldHasOption(field *structurev1.Field, optionID string) bool {
	if optionID == "" || field.GetConfigMap() == nil {
		return false
	}
	options := field.GetConfigMap().GetFields()["option_list"].GetListValue()
	for _, option := range options.GetValues() {
		if option.GetStructValue().GetFields()["id"].GetStringValue() == optionID {
			return true
		}
	}
	return false
}

func validHTTPURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "{") {
		var value struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return false
		}
		raw = value.URL
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func rpcErrorWithData(code int, data map[string]any) error {
	spec := deal.ErrorSpecFor(code)
	detailMap, err := structpb.NewStruct(data)
	if err != nil {
		return deal.RPCError(code)
	}
	withDetails, err := status.New(spec.GRPCCode, spec.Message).WithDetails(
		&errdetails.ErrorInfo{Reason: strconv.Itoa(code)},
		&commonv1.ErrorDetail{
			Code:      int32(code),
			Message:   spec.Message,
			DetailMap: detailMap,
		},
	)
	if err != nil {
		return deal.RPCError(code)
	}
	return withDetails.Err()
}

func commonValueAny(value *commonv1.Value) any {
	if value == nil {
		return nil
	}
	switch kind := value.GetKind().(type) {
	case *commonv1.Value_StringValue:
		return kind.StringValue
	case *commonv1.Value_IntValue:
		return kind.IntValue
	case *commonv1.Value_DoubleValue:
		return kind.DoubleValue
	case *commonv1.Value_BoolValue:
		return kind.BoolValue
	case *commonv1.Value_DateValueMs:
		return kind.DateValueMs
	case *commonv1.Value_ListValue:
		values := make([]any, 0, len(kind.ListValue.GetValueList()))
		for _, item := range kind.ListValue.GetValueList() {
			values = append(values, commonValueAny(item))
		}
		return values
	default:
		return nil
	}
}

func (service *Service) replayOplog(
	ctx context.Context,
	clientRequestID, kind string,
) ([]byte, bool, error) {
	if clientRequestID == "" {
		return nil, false, nil
	}
	entry, err := service.repository.OplogByClientRequest(ctx, clientRequestID)
	if errors.Is(err, base.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if service.now().Sub(time.UnixMilli(entry.CreatedAt)) >= idempotencyWindow {
		return nil, false, nil
	}
	if entry.Kind != kind {
		return nil, false, deal.RPCError(deal.CodeInvalidArgument)
	}
	return entry.ResponseJSON, true, nil
}

func (service *Service) reserveOplog(
	ctx context.Context,
	tx *sql.Tx,
	clientRequestID, kind string,
	request proto.Message,
) (bool, error) {
	requestJSON, err := (protojson.MarshalOptions{
		UseProtoNames: true,
	}).Marshal(request)
	if err != nil {
		return false, fmt.Errorf("marshal records oplog request: %w", err)
	}
	now := service.now().UnixMilli()
	if err := service.repository.DeleteExpiredOplogTx(
		ctx,
		tx,
		clientRequestID,
		now-idempotencyWindow.Milliseconds(),
	); err != nil {
		return false, err
	}
	inserted, err := service.repository.InsertOplogTx(ctx, tx, base.RecordOplogEntry{
		ClientRequestID: clientRequestID,
		Kind:            kind,
		RequestJSON:     requestJSON,
		ResponseJSON:    []byte(`{}`),
		CreatedAt:       now,
	})
	if err != nil {
		return false, err
	}
	return !inserted, nil
}

func marshalProtoResponse(message proto.Message) ([]byte, error) {
	payload, err := (protojson.MarshalOptions{
		UseProtoNames: true,
	}).Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshal records response: %w", err)
	}
	return payload, nil
}

func unmarshalProtoResponse(payload []byte, message proto.Message) error {
	if err := (protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}).Unmarshal(payload, message); err != nil {
		return fmt.Errorf("unmarshal records response: %w", err)
	}
	return nil
}

func (service *Service) fieldByID(
	ctx context.Context,
	fieldID string,
) (*structurev1.Field, error) {
	accessor, ok := service.structure.(FieldAccessor)
	if !ok {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	field, err := accessor.Field(ctx, fieldID)
	if errors.Is(err, base.ErrStructureNotFound) {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if err != nil {
		return nil, err
	}
	if field == nil || field.GetId() != fieldID {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	return field, nil
}

func recordBusinessCode(err error) int {
	if err == nil {
		return 0
	}
	grpcStatus := status.Convert(err)
	for _, detail := range grpcStatus.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			if code, parseErr := strconv.Atoi(info.GetReason()); parseErr == nil {
				return code
			}
		}
	}
	switch grpcStatus.Code() {
	case codes.InvalidArgument:
		return deal.CodeInvalidArgument
	case codes.Aborted:
		return deal.CodeConcurrentConflict
	case codes.NotFound:
		return deal.CodeDeletedResource
	case codes.PermissionDenied:
		return deal.CodePermissionDenied
	default:
		return deal.CodeServiceUnavailable
	}
}

func validateBaseMetadata(ctx context.Context, baseID string) error {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil
	}
	values := incoming.Get("x-base-id")
	if len(values) == 0 {
		return nil
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) != baseID {
		return deal.RPCError(deal.CodeInvalidArgument)
	}
	return nil
}

func (service *Service) validateTableBaseMetadata(
	ctx context.Context,
	tableID string,
) error {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok || len(incoming.Get("x-base-id")) == 0 {
		return nil
	}
	if service.bases == nil {
		return deal.RPCError(deal.CodeServiceUnavailable)
	}
	baseID, _, err := service.bases.ContextByTable(ctx, tableID)
	if err != nil {
		return err
	}
	return validateBaseMetadata(ctx, baseID)
}

func validClientRequestID(value string) bool {
	if len(value) > 128 {
		return false
	}
	for _, current := range []byte(value) {
		if current < 0x20 || current > 0x7e {
			return false
		}
	}
	return true
}

// NewStructureBaseContextProvider adapts the structure repository to the Base
// context seam while keeping tenant identity on the authenticated principal.
func NewStructureBaseContextProvider(
	repository *base.StructureRepository,
) BaseContextProvider {
	return structureBaseContextProvider{repository: repository}
}

type structureBaseContextProvider struct {
	repository *base.StructureRepository
}

func (provider structureBaseContextProvider) ContextByTable(
	ctx context.Context,
	tableID string,
) (string, string, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return "", "", err
	}
	table, err := provider.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		tableID,
	)
	if err != nil {
		return "", "", fmt.Errorf("resolve Base for table %s: %w", tableID, err)
	}
	return table.BaseID, table.EnterpriseID, nil
}

func (accessor structureViewAccessor) Field(
	ctx context.Context,
	fieldID string,
) (*structurev1.Field, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	field, err := accessor.repository.FieldByID(ctx, fieldID)
	if err != nil {
		return nil, err
	}
	if _, err := accessor.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		field.TableID,
	); err != nil {
		return nil, err
	}
	return accessor.fieldToProto(field)
}

func (accessor structureViewAccessor) HasCrossTableDependents(
	ctx context.Context,
	baseID, tableID, fieldID string,
) (bool, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return false, err
	}
	changedField, err := accessor.repository.FieldByID(ctx, fieldID)
	if err != nil {
		return false, err
	}
	tables, err := accessor.repository.TablesByBase(
		ctx,
		principal.GetEnterpriseId(),
		baseID,
	)
	if err != nil {
		return false, err
	}
	for _, table := range tables {
		if table.ID == tableID {
			continue
		}
		fields, err := accessor.Fields(ctx, table.ID)
		if err != nil {
			return false, err
		}
		for _, field := range fields {
			switch field.GetType() {
			case structurev1.FieldType_FIELD_TYPE_LOOKUP:
				targetFieldID := field.GetConfigMap().GetFields()["target_field_id"].GetStringValue()
				if targetFieldID == fieldID {
					return true, nil
				}
			case structurev1.FieldType_FIELD_TYPE_FORMULA:
				expression := field.GetConfigMap().GetFields()["expression"].GetStringValue()
				for _, reference := range formulaReferences(expression) {
					if reference == changedField.Name {
						return true, nil
					}
				}
			}
		}
	}
	return false, nil
}

// NewCalcComputedWriter adapts CalcRepository materialization persistence to
// the transaction-aware ComputedWriter seam.
func NewCalcComputedWriter(
	repository *base.CalcRepository,
) ComputedWriter {
	return calcComputedWriter{repository: repository}
}

type calcComputedWriter struct {
	repository *base.CalcRepository
}

func (writer calcComputedWriter) WriteComputed(
	ctx context.Context,
	recordID, fieldID string,
	value *commonv1.Value,
) error {
	tx, ok := TransactionFromContext(ctx)
	if !ok {
		return errors.New("records transaction is required for computed writes")
	}
	valueJSON, err := (protojson.MarshalOptions{
		UseProtoNames: true,
	}).Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal computed value: %w", err)
	}
	return writer.repository.SaveMaterialized(ctx, tx, base.CalcMaterializedRecord{
		RecordID:  recordID,
		FieldID:   fieldID,
		ValueJSON: valueJSON,
		Status:    "IDLE",
	})
}
