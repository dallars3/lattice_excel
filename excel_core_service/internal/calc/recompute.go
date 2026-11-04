// calc/recompute.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package calc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	"excel-libs/eventbus"
	calcapi "excel/api/calc/v1"
	commonv1 "excel/api/common/v1"
	"excel_core_service/base"
)

type calcJobPayload struct {
	JobID       string   `json:"job_id"`
	TableID     string   `json:"table_id"`
	FieldIDList []string `json:"field_id_list"`
	Trigger     struct {
		RecordID string `json:"record_id"`
		FieldID  string `json:"field_id"`
	} `json:"trigger"`
}

type calcJob struct {
	JobID           string
	BaseID          string
	TableID         string
	FieldIDList     []string
	RecordIDList    []string
	TriggerRecordID string
	TriggerFieldID  string
}

type computedCell struct {
	recordID    string
	field       FieldDefinition
	value       *commonv1.Value
	valueJSON   []byte
	status      string
	formulaHash string
}

// Recompute executes a caller-supplied cross-row recompute job. EventBus
// delivery uses the same idempotent processCalcJob path.
func (service *Service) Recompute(
	ctx context.Context,
	request *calcapi.RecomputeRequest,
) (*calcapi.RecomputeResponse, error) {
	if request == nil || request.GetJobId() == "" || len(request.GetFieldIdList()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "job_id and field_id_list are required")
	}
	if service == nil || service.schema == nil {
		return nil, status.Error(codes.FailedPrecondition, "calc schema provider is required")
	}
	fields, err := service.schema.Fields(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "load calc schema: %v", err)
	}
	tableID := ""
	fieldIDs := make(map[string]struct{}, len(request.GetFieldIdList()))
	for _, fieldID := range request.GetFieldIdList() {
		fieldIDs[fieldID] = struct{}{}
	}
	for _, field := range fields {
		if _, ok := fieldIDs[field.ID]; ok {
			if tableID == "" {
				tableID = field.TableID
			} else if tableID != field.TableID {
				return nil, status.Error(codes.InvalidArgument, "field_id_list must belong to one table")
			}
		}
	}
	if tableID == "" {
		return nil, status.Error(codes.InvalidArgument, "unknown field_id in field_id_list")
	}
	triggerRecordID := ""
	if len(request.GetRowList()) > 0 {
		triggerRecordID = request.GetRowList()[0]
	}
	job := calcJob{
		JobID:           request.GetJobId(),
		BaseID:          request.GetBaseId(),
		TableID:         tableID,
		FieldIDList:     append([]string(nil), request.GetFieldIdList()...),
		RecordIDList:    append([]string(nil), request.GetRowList()...),
		TriggerRecordID: triggerRecordID,
	}
	if err := service.processCalcJob(ctx, job); err != nil {
		return nil, status.Errorf(codes.Internal, "recompute calc job: %v", err)
	}
	return &calcapi.RecomputeResponse{Accepted: true}, nil
}

func (service *Service) processCalcJob(ctx context.Context, job calcJob) error {
	if service == nil || service.repository == nil {
		return errors.New("calc repository is required")
	}
	if service.schema == nil {
		return errors.New("calc schema provider is required")
	}
	if service.materializer == nil {
		return errors.New("calc cell materializer is required")
	}
	if service.outbox == nil {
		return errors.New("calc outbox is required")
	}
	if job.JobID == "" || job.TableID == "" {
		return errors.New("calc job_id and table_id are required")
	}
	payload, err := json.Marshal(calcJobPayload{
		JobID:       job.JobID,
		TableID:     job.TableID,
		FieldIDList: job.FieldIDList,
		Trigger: struct {
			RecordID string `json:"record_id"`
			FieldID  string `json:"field_id"`
		}{
			RecordID: job.TriggerRecordID,
			FieldID:  job.TriggerFieldID,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal calc job payload: %w", err)
	}
	created, err := service.repository.CreateJob(ctx, job.JobID, job.TableID, payload)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}

	cells, err := service.computeCells(ctx, job)
	if err != nil {
		_ = service.repository.FailJob(ctx, job.JobID)
		return err
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		_ = service.repository.FailJob(ctx, job.JobID)
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, cell := range cells {
		if err := service.repository.SaveMaterialized(ctx, tx, base.CalcMaterializedRecord{
			RecordID:    cell.recordID,
			FieldID:     cell.field.ID,
			ValueJSON:   cell.valueJSON,
			FormulaHash: cell.formulaHash,
			Status:      cell.status,
		}); err != nil {
			_ = service.repository.FailJob(ctx, job.JobID)
			return err
		}
		event, err := service.recordChangedEvent(job, cell)
		if err != nil {
			_ = service.repository.FailJob(ctx, job.JobID)
			return err
		}
		if err := service.outbox.Enqueue(ctx, tx, event); err != nil {
			_ = service.repository.FailJob(ctx, job.JobID)
			return fmt.Errorf("enqueue record.changed: %w", err)
		}
	}
	if err := service.repository.FinishJob(ctx, tx, job.JobID, "SUCCESS"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit calc job: %w", err)
	}
	return nil
}

func (service *Service) computeCells(ctx context.Context, job calcJob) ([]computedCell, error) {
	fields, err := service.schema.Fields(ctx)
	if err != nil {
		return nil, fmt.Errorf("load calc schema: %w", err)
	}
	graph, err := buildDependencyGraph(fields)
	if err != nil {
		return nil, err
	}
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	seeds := make([]FieldRef, 0, len(job.FieldIDList)+1)
	for _, fieldID := range job.FieldIDList {
		seeds = append(seeds, FieldRef{TableID: job.TableID, FieldID: fieldID})
	}
	if job.TriggerFieldID != "" {
		seeds = append(seeds, FieldRef{TableID: job.TableID, FieldID: job.TriggerFieldID})
	}
	affected, err := graph.affectedFields(seeds)
	if err != nil {
		return nil, err
	}
	ordered := graph.topologicalOrder(affected)
	recordsByTable, err := service.recordsByTable(ctx, job, graph, ordered)
	if err != nil {
		return nil, err
	}
	fieldsByTable := make(map[string][]FieldDefinition)
	for _, field := range fields {
		fieldsByTable[field.TableID] = append(fieldsByTable[field.TableID], field)
	}

	cells := make([]computedCell, 0)
	for _, reference := range ordered {
		field := graph.nodes[reference]
		if field.Type != "FIELD_TYPE_FORMULA" || field.Formula == "" {
			continue
		}
		for _, recordID := range recordsByTable[reference.TableID] {
			row, err := service.readEvalRow(ctx, reference.TableID, recordID, field)
			if err != nil {
				return nil, err
			}
			evaluationContext := &evalContext{
				schema:    schemaTypes(fieldsByTable[reference.TableID]),
				row:       row,
				now:       service.now,
				location:  service.location,
				functions: functionRegistry,
			}
			result := evaluateExpression(field.Formula, evaluationContext)
			value := result.GetValue()
			if value == nil {
				value = &commonv1.Value{}
			}
			cellStatus := "IDLE"
			if result.GetIsError() {
				cellStatus = "ERROR"
			}
			if err := service.materializer.WriteComputed(
				ctx,
				recordID,
				field.ID,
				value,
			); err != nil {
				return nil, fmt.Errorf("write computed %s/%s: %w", recordID, field.ID, err)
			}
			valueJSON, err := protojson.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("marshal computed value: %w", err)
			}
			sum := sha256.Sum256([]byte(field.Formula))
			cells = append(cells, computedCell{
				recordID:    recordID,
				field:       field,
				value:       value,
				valueJSON:   valueJSON,
				status:      cellStatus,
				formulaHash: hex.EncodeToString(sum[:]),
			})
		}
	}
	return cells, nil
}

func (service *Service) recordsByTable(
	ctx context.Context,
	job calcJob,
	graph *dependencyGraph,
	fields []FieldRef,
) (map[string][]string, error) {
	initialRecords := append([]string(nil), job.RecordIDList...)
	if len(initialRecords) == 0 && job.TriggerRecordID != "" {
		initialRecords = append(initialRecords, job.TriggerRecordID)
	}
	records := map[string][]string{job.TableID: uniqueStrings(initialRecords)}
	for _, reference := range fields {
		if reference.TableID == job.TableID || len(records[reference.TableID]) > 0 {
			continue
		}
		finder, ok := service.materializer.(RelationRecordFinder)
		if !ok {
			return nil, fmt.Errorf(
				"跨表重算到 %s 需要 RelationRecordFinder 接缝",
				reference.TableID,
			)
		}
		relationFields := incomingRelationFields(graph, reference.TableID, job.TableID)
		if len(relationFields) == 0 {
			return nil, fmt.Errorf(
				"跨表重算到 %s 缺少指向表 %s 的关联字段",
				reference.TableID,
				job.TableID,
			)
		}
		related := make([]string, 0)
		for _, relationField := range relationFields {
			recordIDs, err := finder.RowsReferencing(
				ctx,
				relationField.TableID,
				relationField.ID,
				job.TriggerRecordID,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"resolve related records through %s/%s: %w",
					relationField.TableID,
					relationField.ID,
					err,
				)
			}
			related = append(related, recordIDs...)
		}
		records[reference.TableID] = uniqueStrings(related)
	}
	for tableID, recordIDs := range records {
		if len(recordIDs) == 0 {
			return nil, fmt.Errorf("calc table %s has no affected records", tableID)
		}
	}
	return records, nil
}

func incomingRelationFields(
	graph *dependencyGraph,
	sourceTableID, targetTableID string,
) []FieldDefinition {
	fields := make([]FieldDefinition, 0)
	for _, reference := range graph.sortedNodes() {
		field := graph.nodes[reference]
		if field.TableID != sourceTableID || field.Relation == nil {
			continue
		}
		if field.Relation.TableID == targetTableID {
			fields = append(fields, field)
		}
	}
	return fields
}

func (service *Service) readEvalRow(
	ctx context.Context,
	tableID, recordID string,
	formula FieldDefinition,
) (map[string]any, error) {
	row := make(map[string]any)
	reader, ok := service.materializer.(RowContextReader)
	if !ok {
		if !formulaNeedsRow(formula) {
			return row, nil
		}
		return nil, fmt.Errorf("公式 %s 需要 RowContextReader 接缝", formula.ID)
	}
	values, err := reader.ReadRowContext(ctx, tableID, recordID)
	if err != nil {
		return nil, fmt.Errorf("read row context %s/%s: %w", tableID, recordID, err)
	}
	for name, value := range values {
		row[name] = commonValueAny(value)
	}
	return row, nil
}

func schemaTypes(fields []FieldDefinition) map[string]fieldType {
	schema := make(map[string]fieldType, len(fields))
	for _, field := range fields {
		parsed, err := parseFieldType(field.Type)
		if err == nil {
			schema[field.Name] = parsed
		}
	}
	return schema
}

func commonValueAny(value *commonv1.Value) any {
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
		items := make([]any, 0, len(kind.ListValue.GetValueList()))
		for _, item := range kind.ListValue.GetValueList() {
			items = append(items, commonValueAny(item))
		}
		return items
	default:
		return nil
	}
}

func formulaNeedsRow(formula FieldDefinition) bool {
	if len(formula.FormulaRefs) > 0 {
		return true
	}
	parsed, err := parseFormula(formula.Formula)
	if err != nil {
		return true
	}
	return len(expressionReferences(parsed)) > 0
}

func (service *Service) recordChangedEvent(job calcJob, cell computedCell) (eventbus.Event, error) {
	valueJSON, err := protojson.Marshal(cell.value)
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("marshal record.changed value: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"record_id":   cell.recordID,
		"field_id":    cell.field.ID,
		"value":       json.RawMessage(valueJSON),
		"change_type": "calc",
	})
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("marshal record.changed payload: %w", err)
	}
	eventID, err := newULID(service.now())
	if err != nil {
		return eventbus.Event{}, err
	}
	return eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "record.changed",
			Version:    1,
			BaseID:     job.BaseID,
			Seq:        0,
			OccurredAt: service.now().UnixMilli(),
			Producer:   "calc",
		},
		Payload: payload,
	}, nil
}

func newULID(now time.Time) (string, error) {
	var raw [16]byte
	milliseconds := uint64(now.UnixMilli())
	raw[0] = byte(milliseconds >> 40)
	raw[1] = byte(milliseconds >> 32)
	raw[2] = byte(milliseconds >> 24)
	raw[3] = byte(milliseconds >> 16)
	raw[4] = byte(milliseconds >> 8)
	raw[5] = byte(milliseconds)
	if _, err := io.ReadFull(rand.Reader, raw[6:]); err != nil {
		return "", fmt.Errorf("generate ULID randomness: %w", err)
	}
	encoding := base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)
	return encoding.EncodeToString(raw[:]), nil
}

func (graph *dependencyGraph) affectedFields(seeds []FieldRef) (map[FieldRef]struct{}, error) {
	reverse := make(map[FieldRef]map[FieldRef]struct{}, len(graph.nodes))
	for source, dependencies := range graph.edges {
		for dependency := range dependencies {
			if reverse[dependency] == nil {
				reverse[dependency] = make(map[FieldRef]struct{})
			}
			reverse[dependency][source] = struct{}{}
		}
	}
	affected := make(map[FieldRef]struct{})
	queue := make([]FieldRef, 0, len(seeds))
	for _, seed := range seeds {
		if seed.FieldID == "" {
			continue
		}
		if _, ok := graph.nodes[seed]; !ok {
			return nil, fmt.Errorf("重算字段不存在 %s", seed)
		}
		if _, exists := affected[seed]; !exists {
			affected[seed] = struct{}{}
			queue = append(queue, seed)
		}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for dependent := range reverse[current] {
			if _, exists := affected[dependent]; exists {
				continue
			}
			affected[dependent] = struct{}{}
			queue = append(queue, dependent)
		}
	}
	return affected, nil
}

func (graph *dependencyGraph) topologicalOrder(nodes map[FieldRef]struct{}) []FieldRef {
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[FieldRef]int, len(nodes))
	order := make([]FieldRef, 0, len(nodes))
	var visit func(FieldRef)
	visit = func(node FieldRef) {
		if _, requested := nodes[node]; !requested {
			return
		}
		if state[node] == visiting || state[node] == visited {
			return
		}
		state[node] = visiting
		for _, dependency := range graph.sortedNeighbors(node) {
			visit(dependency)
		}
		state[node] = visited
		order = append(order, node)
	}
	sorted := make([]FieldRef, 0, len(nodes))
	for node := range nodes {
		sorted = append(sorted, node)
	}
	sort.Slice(sorted, func(left, right int) bool {
		return sorted[left].String() < sorted[right].String()
	})
	for _, node := range sorted {
		visit(node)
	}
	return order
}
