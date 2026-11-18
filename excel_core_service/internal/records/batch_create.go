// records/batch_create.go：批量建行（BatchCreateRecords）。导入 20k 行的写行
// 提速路径：一个批次一个事务，把逐行 CreateRecord 的「每行 20+ 条语句 + 一次
// 事务 + 一次 gRPC」摊薄成每批常数次语句：
//   - 结构/租户/幂等预筛/COUNT 容量/跨表依赖判定每批一次；
//   - 记录、历史、幂等日志、outbox 事件合并为多行语句（base/records_batch.go、
//     base/bus.go EnqueueBatchTx）。
// 行级语义与逐行 CreateRecord 一致：单行校验失败只影响该行（结果含错误码/原因，
// 不回滚整批）；每行独立 client_request_id 幂等（命中幂等日志的行重放既有
// record_id，不重复建行）；新记录 version=1；record.changed 与 calc.job 事件、
// 公式物化（含 #ERROR 哨兵）口径不变。
package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	"excel-libs/eventbus"
	"excel-libs/idgen"
	commonv1 "excel/api/common/v1"
	"excel_core_service/internal/calc"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// batchCreateMaxRows 是单个批量建行请求的行数上限；worker 按 200 行切片。
const batchCreateMaxRows = 500

// batchCreatePlannedRow 是一行在事务前的全部产物：记录、历史、事件、幂等日志
// 与公式物化变更。构建阶段不动库，任何单行失败只淘汰该行。
type batchCreatePlannedRow struct {
	index          int
	row            *recordsv1.BatchCreateRecordRow
	record         base.Record
	changedFields  []*structurev1.Field
	histories      []base.RecordHistory
	events         []eventbus.Event
	oplog          *base.RecordOplogEntry
	formulaChanges []formulaChange
	// 并发幂等抢占重放：同键已由其它事务提交，本行不再插入，直接返回既有 id。
	replayed   bool
	replayedID string
}

// BatchCreateRecords inserts a batch of records in one transaction with
// per-row results. rows：1~500 行；每行独立 client_request_id 幂等。
func (service *Service) BatchCreateRecords(
	ctx context.Context,
	request *recordsv1.BatchCreateRecordsRequest,
) (*recordsv1.BatchCreateRecordsResponse, error) {
	if service == nil || service.repository == nil || request == nil ||
		request.GetTableId() == "" ||
		len(request.GetRowList()) == 0 ||
		len(request.GetRowList()) > batchCreateMaxRows {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	seenIDs := make(map[string]struct{}, len(request.GetRowList()))
	for _, row := range request.GetRowList() {
		if row == nil || !validClientRequestID(row.GetClientRequestId()) {
			return nil, deal.RPCError(deal.CodeInvalidArgument)
		}
		if row.GetClientRequestId() == "" {
			continue
		}
		if _, duplicate := seenIDs[row.GetClientRequestId()]; duplicate {
			return nil, deal.RPCError(deal.CodeInvalidArgument)
		}
		seenIDs[row.GetClientRequestId()] = struct{}{}
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := rowScopeFromContext(ctx); err != nil {
		return nil, err
	}
	if service.outbox == nil || service.bases == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	table, err := service.structure.Table(ctx, request.GetTableId())
	if err != nil {
		return nil, mapRecordError(err)
	}
	if table == nil || table.GetId() != request.GetTableId() {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	baseID := table.GetBaseId()
	// x-base-id 元数据一致性每批校验一次（逐行路径 createRecordInTx 每行一次）。
	if err := validateBaseMetadata(ctx, baseID); err != nil {
		return nil, err
	}
	fields, err := service.structure.Fields(ctx, request.GetTableId())
	if err != nil {
		return nil, mapRecordError(err)
	}
	byID := fieldByID(fields)

	rows := request.GetRowList()
	results := make([]*recordsv1.BatchCreateRecordResult, len(rows))
	planned := make([]*batchCreatePlannedRow, 0, len(rows))
	// Pass 1：逐行校验与产物构建（不动库），单行失败只标记该行结果。
	for index, row := range rows {
		result := &recordsv1.BatchCreateRecordResult{Index: int32(index)}
		results[index] = result
		plan, planErr := service.planBatchCreateRow(
			ctx,
			index,
			row,
			table.GetId(),
			baseID,
			principal.GetUserId(),
			byID,
			fields,
		)
		if planErr != nil {
			result.ErrorCode = batchRowErrorCode(planErr)
			result.Reason = planErr.Error()
			continue
		}
		planned = append(planned, plan)
	}

	// Pass 2：幂等键预筛——命中未过期幂等日志的行直接重放（与 CreateRecord 的
	// replayOplog 同口径），剩余行进入建行事务。
	planned, err = service.replayBatchCreateOplog(ctx, planned, results)
	if err != nil {
		return nil, err
	}
	if len(planned) == 0 {
		return &recordsv1.BatchCreateRecordsResponse{ResultList: results}, nil
	}

	// Pass 3：跨表依赖判定每批一次（逐行路径每 cell 一次，是导入慢的主因之一）；
	// calc.job 事件按（记录 × 依赖字段）构建，与逐行 enqueueCalcJobIfNeeded 一致。
	dependentFields, err := service.resolveBatchCalcDependents(ctx, baseID, table.GetId(), planned)
	if err != nil {
		return nil, err
	}
	for _, plan := range planned {
		for _, field := range plan.changedFields {
			if !dependentFields[field.GetId()] {
				continue
			}
			job, jobErr := service.buildCalcJobEvent(baseID, table.GetId(), plan.record.ID, field.GetId())
			if jobErr != nil {
				return nil, jobErr
			}
			plan.events = append(plan.events, job)
		}
	}

	// Pass 4：一个批次一个事务。
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// 容量上限每批查一次：装得下的行照常写入，装不下的行按行失败（5001），
	// 不预留幂等键（同键可重试）。
	count, err := service.repository.CountActiveRecords(ctx, tx, table.GetId())
	if err != nil {
		return nil, err
	}
	if count+int64(len(planned)) > maxRecordsPerTable {
		overflow := count + int64(len(planned)) - maxRecordsPerTable
		kept := make([]*batchCreatePlannedRow, 0, len(planned))
		for _, plan := range planned {
			if overflow > 0 {
				overflow--
				results[plan.index].ErrorCode = deal.CodeCapacityLimit
				results[plan.index].Reason = "超出单表记录上限"
				continue
			}
			kept = append(kept, plan)
		}
		planned = kept
		if len(planned) == 0 {
			return &recordsv1.BatchCreateRecordsResponse{ResultList: results}, nil
		}
	}

	// 幂等键预留：过期键先清理，再整批多行插入；并发抢占（1062）退回逐行
	// reserveOplog，重复键按既有响应重放（不重复建行）。
	if err := service.reserveBatchOplogRows(ctx, tx, planned); err != nil {
		return nil, err
	}
	recordsToInsert := make([]base.Record, 0, len(planned))
	histories := make([]base.RecordHistory, 0, len(planned))
	events := make([]eventbus.Event, 0, len(planned))
	oplogUpdates := make([]base.RecordOplogEntry, 0, len(planned))
	for _, plan := range planned {
		if plan.replayed {
			continue
		}
		// 公式物化落库（含 #ERROR 哨兵状态）；无公式字段的表零成本。
		for _, change := range plan.formulaChanges {
			if err := service.writeComputedChange(
				withTransaction(ctx, tx),
				plan.record.ID,
				change,
			); err != nil {
				return nil, err
			}
		}
		recordsToInsert = append(recordsToInsert, plan.record)
		histories = append(histories, plan.histories...)
		events = append(events, plan.events...)
		if plan.oplog != nil {
			oplogUpdates = append(oplogUpdates, *plan.oplog)
		}
	}
	if len(recordsToInsert) > 0 {
		if err := service.repository.CreateRecordsTx(ctx, tx, recordsToInsert); err != nil {
			return nil, err
		}
	}
	if len(histories) > 0 {
		if err := service.repository.AppendHistoryRowsTx(ctx, tx, histories); err != nil {
			return nil, err
		}
	}
	if len(events) > 0 {
		if err := service.outboxEnqueueBatch(ctx, tx, events); err != nil {
			return nil, err
		}
	}
	if len(oplogUpdates) > 0 {
		if err := service.repository.UpsertOplogResponsesTx(ctx, tx, oplogUpdates); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit BatchCreateRecords: %w", err)
	}
	for _, plan := range planned {
		results[plan.index].Ok = true
		if plan.replayed {
			results[plan.index].RecordId = plan.replayedID
			continue
		}
		results[plan.index].RecordId = plan.record.ID
	}
	return &recordsv1.BatchCreateRecordsResponse{ResultList: results}, nil
}

// planBatchCreateRow 构建一行的全部产物（记录/历史/事件/幂等日志/公式变更），
// 不触库。单元格校验或公式定义错误返回按行失败原因。
func (service *Service) planBatchCreateRow(
	ctx context.Context,
	index int,
	row *recordsv1.BatchCreateRecordRow,
	tableID, baseID, operator string,
	byID map[string]*structurev1.Field,
	fields []*structurev1.Field,
) (*batchCreatePlannedRow, error) {
	validated := make(map[string]*commonv1.Value, len(row.GetCellsMap()))
	changed := make([]*structurev1.Field, 0, len(row.GetCellsMap()))
	for fieldID, value := range row.GetCellsMap() {
		field := byID[fieldID]
		if field == nil || !fieldIsWritable(field.GetType()) {
			return nil, errors.New("字段不存在或不可写")
		}
		if err := validateFieldValue(field, value); err != nil {
			return nil, err
		}
		validated[fieldID] = value
		changed = append(changed, field)
	}
	recordID, err := newRecordULID()
	if err != nil {
		return nil, err
	}
	now := service.now().UnixMilli()
	record := base.Record{
		ID:        recordID,
		TableID:   tableID,
		Cells:     cloneRecordCells(validated),
		CreatedBy: operator,
		CreatedAt: now,
		Version:   1,
	}
	formulaChanges, err := service.computeFormulaChanges(ctx, &record, fields, changed)
	if err != nil {
		return nil, err
	}
	plan := &batchCreatePlannedRow{
		index:          index,
		row:            row,
		record:         record,
		changedFields:  changed,
		formulaChanges: formulaChanges,
	}
	for _, field := range changed {
		historyID, idErr := newRecordULID()
		if idErr != nil {
			return nil, idErr
		}
		plan.histories = append(plan.histories, base.RecordHistory{
			ID:        historyID,
			RecordID:  record.ID,
			FieldID:   field.GetId(),
			After:     record.Cells[field.GetId()],
			Operator:  operator,
			CreatedAt: now,
		})
	}
	for _, change := range formulaChanges {
		historyID, idErr := newRecordULID()
		if idErr != nil {
			return nil, idErr
		}
		plan.histories = append(plan.histories, base.RecordHistory{
			ID:        historyID,
			RecordID:  record.ID,
			FieldID:   change.FieldID,
			After:     change.After,
			Operator:  operator,
			CreatedAt: now,
		})
	}
	event, err := recordChangedEvent(
		baseID,
		tableID,
		record.ID,
		"",
		nil,
		record.Version,
		operator,
		"create",
		service.now(),
		"records",
	)
	if err != nil {
		return nil, err
	}
	plan.events = append(plan.events, event)
	if row.GetClientRequestId() != "" {
		requestJSON, marshalErr := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(row)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal records oplog request: %w", marshalErr)
		}
		response := &recordsv1.CreateRecordResponse{Record: recordToProto(record)}
		responseJSON, marshalErr := marshalProtoResponse(response)
		if marshalErr != nil {
			return nil, marshalErr
		}
		plan.oplog = &base.RecordOplogEntry{
			ClientRequestID: row.GetClientRequestId(),
			Kind:            oplogKindCreateRecord,
			RequestJSON:     requestJSON,
			ResponseJSON:    responseJSON,
			CreatedAt:       now,
		}
	}
	return plan, nil
}

// replayBatchCreateOplog 用一次 SELECT 预筛幂等键：未过期且同为建行操作的行
// 直接重放既有 record_id（与 CreateRecord replayOplog 同语义）；过期键留给
// reserveBatchOplogRows 清理后复用。kind 冲突按行失败（批量路径的行级语义）。
func (service *Service) replayBatchCreateOplog(
	ctx context.Context,
	planned []*batchCreatePlannedRow,
	results []*recordsv1.BatchCreateRecordResult,
) ([]*batchCreatePlannedRow, error) {
	ids := make([]string, 0, len(planned))
	byID := make(map[string]*batchCreatePlannedRow, len(planned))
	withoutOplog := make([]*batchCreatePlannedRow, 0, len(planned))
	for _, plan := range planned {
		if plan.oplog == nil {
			withoutOplog = append(withoutOplog, plan)
			continue
		}
		ids = append(ids, plan.oplog.ClientRequestID)
		byID[plan.oplog.ClientRequestID] = plan
	}
	if len(ids) > 0 {
		entries, err := service.repository.OplogByClientRequests(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			plan, ok := byID[entry.ClientRequestID]
			if !ok {
				continue
			}
			if service.now().Sub(time.UnixMilli(entry.CreatedAt)) >= idempotencyWindow {
				// 过期键：保留在建行流程里，预留阶段先删除旧键再复用。
				continue
			}
			if entry.Kind != oplogKindCreateRecord {
				results[plan.index].ErrorCode = deal.CodeInvalidArgument
				results[plan.index].Reason = "幂等键已用于其它操作"
				delete(byID, entry.ClientRequestID)
				continue
			}
			var replay recordsv1.CreateRecordResponse
			if err := unmarshalProtoResponse(entry.ResponseJSON, &replay); err != nil {
				results[plan.index].ErrorCode = deal.CodeServiceUnavailable
				results[plan.index].Reason = err.Error()
				delete(byID, entry.ClientRequestID)
				continue
			}
			results[plan.index].Ok = true
			results[plan.index].RecordId = replay.GetRecord().GetId()
			delete(byID, entry.ClientRequestID)
		}
	}
	remaining := make([]*batchCreatePlannedRow, 0, len(planned))
	remaining = append(remaining, withoutOplog...)
	for _, plan := range planned {
		if plan.oplog == nil {
			continue
		}
		if _, keep := byID[plan.oplog.ClientRequestID]; keep {
			remaining = append(remaining, plan)
		}
	}
	return remaining, nil
}

// reserveBatchOplogRows 清理过期幂等键并整批多行预留。并发抢占（1062）时退回
// 逐行 reserveOplog：重复键标记为重放行（不重复建行），其余行继续本批建行。
func (service *Service) reserveBatchOplogRows(
	ctx context.Context,
	tx *sql.Tx,
	planned []*batchCreatePlannedRow,
) error {
	oplogRows := make([]base.RecordOplogEntry, 0, len(planned))
	reservedIDs := make([]string, 0, len(planned))
	for _, plan := range planned {
		if plan.replayed || plan.oplog == nil {
			continue
		}
		oplogRows = append(oplogRows, *plan.oplog)
		reservedIDs = append(reservedIDs, plan.oplog.ClientRequestID)
	}
	if len(oplogRows) == 0 {
		return nil
	}
	if err := service.repository.DeleteExpiredOplogIDsTx(
		ctx,
		tx,
		reservedIDs,
		service.now().Add(-idempotencyWindow).UnixMilli(),
	); err != nil {
		return err
	}
	if err := service.repository.InsertOplogRowsTx(ctx, tx, oplogRows); err != nil {
		var mysqlErr *mysqlDriver.MySQLError
		if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
			return err
		}
		return service.reserveBatchOplogRowsPerRow(ctx, tx, planned)
	}
	return nil
}

// reserveBatchOplogRowsPerRow 是并发幂等抢占的慢路径：逐行清理过期键并预留，
// 重复键按既有响应重放（与 CreateRecord duplicate 分支一致）。
func (service *Service) reserveBatchOplogRowsPerRow(
	ctx context.Context,
	tx *sql.Tx,
	planned []*batchCreatePlannedRow,
) error {
	for _, plan := range planned {
		if plan.replayed || plan.oplog == nil {
			continue
		}
		duplicate, err := service.reserveOplog(
			ctx,
			tx,
			plan.oplog.ClientRequestID,
			oplogKindCreateRecord,
			plan.row,
		)
		if err != nil {
			return err
		}
		if !duplicate {
			continue
		}
		payload, found, replayErr := service.replayOplog(
			ctx,
			plan.oplog.ClientRequestID,
			oplogKindCreateRecord,
		)
		if replayErr != nil {
			return replayErr
		}
		if !found {
			return deal.RPCError(deal.CodeServiceUnavailable)
		}
		var replay recordsv1.CreateRecordResponse
		if err := unmarshalProtoResponse(payload, &replay); err != nil {
			return err
		}
		plan.replayed = true
		plan.replayedID = replay.GetRecord().GetId()
	}
	return nil
}

// resolveBatchCalcDependents 每批一次判定「哪些字段有跨表依赖」，替代逐行路径
// 每个 cell 一次的 HasCrossTableDependents 查询链。
func (service *Service) resolveBatchCalcDependents(
	ctx context.Context,
	baseID, tableID string,
	planned []*batchCreatePlannedRow,
) (map[string]bool, error) {
	provider, ok := service.structure.(CrossTableDependencyProvider)
	if !ok {
		return nil, nil
	}
	fieldIDs := make(map[string]struct{})
	for _, plan := range planned {
		if plan.replayed {
			continue
		}
		for _, field := range plan.changedFields {
			fieldIDs[field.GetId()] = struct{}{}
		}
	}
	dependent := make(map[string]bool, len(fieldIDs))
	for fieldID := range fieldIDs {
		has, err := provider.HasCrossTableDependents(ctx, baseID, tableID, fieldID)
		if err != nil {
			return nil, err
		}
		dependent[fieldID] = has
	}
	return dependent, nil
}

// buildCalcJobEvent 构建与 enqueueCalcJob 相同载荷的 calc.job 事件（批量路径
// 的事件并入 outbox 批量插入）。
func (service *Service) buildCalcJobEvent(
	baseID, tableID, recordID, fieldID string,
) (eventbus.Event, error) {
	jobID, err := idgen.New()
	if err != nil {
		return eventbus.Event{}, err
	}
	// 载荷契约单源（双轴评审第 2 项）：写链 calc.job 经 calc.NewCalcJobPayload
	// 构造，与 calc 消费者/structure 回填种子共用同一类型——线上字段一字不差。
	payload, err := json.Marshal(calc.NewCalcJobPayload(
		jobID,
		tableID,
		[]string{fieldID},
		recordID,
		fieldID,
		nil,
	))
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("marshal calc.job payload: %w", err)
	}
	eventID, err := idgen.New()
	if err != nil {
		return eventbus.Event{}, err
	}
	return eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "calc.job",
			Version:    1,
			BaseID:     baseID,
			OccurredAt: service.now().UnixMilli(),
			Producer:   "records",
		},
		Payload: payload,
	}, nil
}

// outboxEnqueueBatch 优先走 outbox 的批量入队缝；未实现时逐条入队兜底。
func (service *Service) outboxEnqueueBatch(
	ctx context.Context,
	tx *sql.Tx,
	events []eventbus.Event,
) error {
	if batcher, ok := service.outbox.(interface {
		EnqueueBatchTx(ctx context.Context, tx *sql.Tx, events []eventbus.Event) error
	}); ok {
		return batcher.EnqueueBatchTx(ctx, tx, events)
	}
	for _, event := range events {
		if err := service.outbox.Enqueue(ctx, tx, event); err != nil {
			return err
		}
	}
	return nil
}

// batchRowErrorCode 把按行失败原因映射为业务错误码：已是 gRPC 状态的错误（公式
// 定义错误、容量等）按状态映射；单元格校验类普通错误归 1001。
func batchRowErrorCode(err error) int32 {
	if status.Code(err) != codes.Unknown {
		return int32(recordBusinessCode(err))
	}
	return deal.CodeInvalidArgument
}
