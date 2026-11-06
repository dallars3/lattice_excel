// importexport/create_import.go：预览后的逐行校验、core 写入与错误报告。
package importexport

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"excel-libs/eventbus"
	"excel-libs/idgen"
	commonv1 "excel/api/common/v1"
	importexportv1 "excel/api/importexport/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	workspacev1 "excel/api/workspace/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

type importColumn struct {
	Index   int
	Name    string
	Type    structurev1.FieldType
	Ignore  bool
	FieldID string
}

type preparedImportRow struct {
	RowNumber int
	Values    map[int]*commonv1.Value
}

type importFailure struct {
	RowNumber int
	Column    string
	Value     string
	Reason    string
}

// CreateImport persists one import task, builds the Base/table skeleton, then
// returns the IMPORTING task immediately while rows are written in the
// background（TC-140-08：响应早返回，导入中的 Base 隔离由此可观测）.
func (service *Service) CreateImport(
	ctx context.Context,
	request *importexportv1.CreateImportRequest,
) (*importexportv1.CreateImportResponse, error) {
	if err := requireImportTask(service); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if service.records == nil || service.workspace == nil ||
		service.structure == nil || service.bus == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if request == nil || request.GetFileId() == "" || request.GetSheetIndex() < 0 {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetClientRequestId() != "" {
		existing, lookupErr := service.tasks.GetByClientRequestID(
			ctx,
			request.GetClientRequestId(),
		)
		switch {
		case lookupErr == nil && existing.Kind != "import":
			return nil, deal.RPCError(deal.CodeInvalidArgument)
		case lookupErr == nil:
			protoTask, conversionErr := taskToProto(existing)
			if conversionErr != nil {
				return nil, conversionErr
			}
			return &importexportv1.CreateImportResponse{ImportTask: protoTask}, nil
		case lookupErr != nil && !errors.Is(lookupErr, base.ErrImportExportTaskNotFound):
			return nil, lookupErr
		}
	}

	enterpriseID := metadataValue(ctx, "x-enterprise-id")
	if enterpriseID == "" {
		return service.recordImportDeadLetter(ctx, request, principal)
	}

	// 解析与 sheet_index 校验先行（缺陷 #22）：越界 Sheet / 不可解析文件在任务
	// 创建前返回 1001，不产生导入任务；同一 file_id 修正映射后可直接重试。
	table, err := service.parseImportFile(
		ctx,
		principal.GetUserId(),
		request.GetFileId(),
		int(request.GetSheetIndex()),
	)
	if err != nil {
		return nil, err
	}

	now := service.now().UTC().UnixMilli()
	taskID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	creatorPayload, _ := json.Marshal(map[string]any{
		"creator_id": principal.GetUserId(),
	})
	task, err := service.tasks.Create(ctx, base.ImportExportTask{
		ID:              taskID,
		Kind:            "import",
		Status:          statusImporting,
		FileID:          request.GetFileId(),
		ClientRequestID: request.GetClientRequestId(),
		Payload:         creatorPayload,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return nil, err
	}
	if task.ID != taskID {
		protoTask, conversionErr := taskToProto(task)
		if conversionErr != nil {
			return nil, conversionErr
		}
		return &importexportv1.CreateImportResponse{ImportTask: protoTask}, nil
	}

	header, dataRows := applyHeaderChoice(table, request.GetFirstRowHeader())
	columns, err := buildImportColumns(
		request.GetColumnConfigList(),
		header,
		dataRows,
	)
	if err != nil {
		return nil, service.failImport(ctx, task, err)
	}

	preparedRows, failures := prepareImportRows(dataRows, columns, request.GetFirstRowHeader())
	task.TotalRows = int32(len(dataRows))
	task.FailRows = int32(len(failures))
	task.OKRows = int32(len(preparedRows))

	baseID := metadataValue(ctx, "x-base-id")
	tableID := metadataValue(ctx, "x-table-id")
	createdBase := baseID == ""
	if createdBase {
		if len(preparedRows) > 0 {
			baseID, tableID, err = service.createImportBase(
				ctx,
				task,
				table,
				columns,
				principal,
			)
			if err != nil {
				// 失败也要带出已建 Base，供 failImport 清理半成品（ticket 16）。
				task.BaseID = baseID
				return nil, service.failImport(ctx, task, err)
			}
		}
	} else {
		if tableID == "" {
			return nil, service.failImport(
				ctx,
				task,
				deal.RPCError(deal.CodeInvalidArgument),
			)
		}
		if err := service.bindExistingImportFields(
			ctx,
			tableID,
			columns,
		); err != nil {
			return nil, service.failImport(ctx, task, err)
		}
	}
	task.BaseID = baseID
	task.TableID = tableID

	// tfx / TC-140-08（导入中隔离）：解析、任务行与 Base/Table 骨架已在请求内
	// 完成（一次性快速 RPC），响应按此刻已知状态（IMPORTING + base_id + 行数
	// 计数）立即返回；剩余的写行、错误报告与终态落地转入后台，20k 行导入不再
	// 占住 HTTP 请求直到超时。protoTask 在 goroutine 启动前构建，避免数据竞争。
	protoTask, err := taskToProto(task)
	if err != nil {
		return nil, err
	}
	go service.runImportWrites(
		ctx,
		task,
		enterpriseID,
		principal.GetUserId(),
		columns,
		preparedRows,
		failures,
		createdBase,
	)
	return &importexportv1.CreateImportResponse{ImportTask: protoTask}, nil
}

// backgroundImportParent 返回后台写行工作上下文的父级：
//   - 进程内直调（如单元测试）：请求 ctx 由调用方持有，取消即真实中断信号，
//     写行必须随之中止（缺陷 #25 的断连语义）；
//   - 经 gRPC 到达：grpc-go 在响应送达后立即取消 handler ctx（传输层收尾，
//     与客户端是否断连无关），此处必须解除绑定，否则每个转入后台的导入都会被
//     误杀成 FAILED；断连在异步化之后只可能发生在响应送达前，由请求内的
//     failImport 路径处理。
func backgroundImportParent(ctx context.Context) context.Context {
	if grpc.ServerTransportStreamFromContext(ctx) != nil {
		return context.WithoutCancel(ctx)
	}
	return ctx
}

// runImportWrites 执行导入任务创建后的剩余工作：逐行写入 core、生成行级错误
// 报告并落地终态（DONE/FAILED + import.finished）。写行使用从请求上下文派生的
// 工作 context（见 backgroundImportParent）：断连时立即中止并转入失败路径；
// 失败/完成收尾改用 WithoutCancel 的独立 context（缺陷 #25），状态更新、半成品
// Base 清理与完成事件不随请求取消一起丢失。task 为调用点的副本，响应已在
// CreateImport 中先行构建，此处继续修改不会产生数据竞争。
func (service *Service) runImportWrites(
	ctx context.Context,
	task base.ImportExportTask,
	enterpriseID string,
	userID string,
	columns []importColumn,
	preparedRows []preparedImportRow,
	preparedFailures []importFailure,
	createdBase bool,
) {
	workCtx, cancelWork := context.WithCancel(backgroundImportParent(ctx))
	defer cancelWork()

	okRows, batchFailures, writeErr := service.writePreparedRows(
		workCtx,
		task.ID,
		task.BaseID,
		task.TableID,
		preparedRows,
		columns,
	)
	task.OKRows = int32(okRows)
	failures := append(preparedFailures, batchFailures...)
	task.FailRows = int32(len(failures))
	if writeErr != nil {
		// 请求已取消（缺陷 #25）：failImport 用独立 context 落失败状态并清理。
		if err := service.failImport(workCtx, task, writeErr); err != nil {
			service.logger.Error("importexport: import write aborted",
				"task_id", task.ID, "error", err)
		}
		return
	}
	if len(failures) > 0 {
		reportID, reportErr := service.writeImportReport(workCtx, task, userID, failures)
		if reportErr != nil {
			if err := service.failImport(workCtx, task, reportErr); err != nil {
				service.logger.Error("importexport: import report failed",
					"task_id", task.ID, "error", err)
			}
			return
		}
		task.ReportFileID = reportID
	}
	if task.TotalRows > 0 && okRows == 0 {
		if err := service.failAllRowsImport(workCtx, task, createdBase); err != nil {
			service.logger.Error("importexport: fail all rows import",
				"task_id", task.ID, "error", err)
		}
		return
	}
	task.Status = statusDone
	task.UpdatedAt = service.now().UTC().UnixMilli()
	task.Payload, _ = json.Marshal(map[string]any{
		"creator_id": userID,
	})
	// 完成态与 import.finished 必须可靠发出（缺陷 #25）：客户端在收尾瞬间断连
	// 不能让已完成的导入永远停留在「导入中」锁定态，解绑取消信号。
	finishCtx, cancelFinish := context.WithTimeout(context.WithoutCancel(workCtx), importFinishTimeout)
	defer cancelFinish()
	if err := service.tasks.Update(finishCtx, task); err != nil {
		service.logger.Error("importexport: mark import done", "task_id", task.ID, "error", err)
		return
	}
	if err := service.publishImportFinished(finishCtx, task, enterpriseID); err != nil {
		service.logger.Error("importexport: publish import finished", "task_id", task.ID, "error", err)
	}
}

// recordImportDeadLetter 记录缺失企业归属的死信导入任务并返回失败信封。状态
// 更新使用独立于请求的 context（缺陷 #25）：客户端断连后失败状态也能落地。
func (service *Service) recordImportDeadLetter(
	ctx context.Context,
	request *importexportv1.CreateImportRequest,
	principal *commonv1.Principal,
) (*importexportv1.CreateImportResponse, error) {
	now := service.now().UTC().UnixMilli()
	taskID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	creatorPayload, _ := json.Marshal(map[string]any{
		"creator_id": principal.GetUserId(),
	})
	task, err := service.tasks.Create(ctx, base.ImportExportTask{
		ID:              taskID,
		Kind:            "import",
		Status:          statusImporting,
		FileID:          request.GetFileId(),
		ClientRequestID: request.GetClientRequestId(),
		Payload:         creatorPayload,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return nil, err
	}
	if task.ID != taskID {
		protoTask, conversionErr := taskToProto(task)
		if conversionErr != nil {
			return nil, conversionErr
		}
		return &importexportv1.CreateImportResponse{ImportTask: protoTask}, nil
	}
	task.Status = statusFailed
	task.Payload, _ = json.Marshal(map[string]any{
		"creator_id":         principal.GetUserId(),
		"dead_letter":        true,
		"dead_letter_reason": "x-enterprise-id is required",
	})
	task.UpdatedAt = service.now().UTC().UnixMilli()
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), importFinishTimeout)
	defer cancel()
	if updateErr := service.tasks.Update(finishCtx, task); updateErr != nil {
		return nil, updateErr
	}
	service.logger.Error(
		"import task moved to dead letter",
		"task_id", task.ID,
		"reason", "missing enterprise id",
	)
	protoTask, conversionErr := taskToProto(task)
	if conversionErr != nil {
		return nil, conversionErr
	}
	return &importexportv1.CreateImportResponse{ImportTask: protoTask}, nil
}

func applyHeaderChoice(table parsedTable, firstRowHeader bool) ([]string, [][]string) {
	if len(table.Rows) == 0 {
		return nil, nil
	}
	if firstRowHeader {
		header := append([]string(nil), table.Rows[0]...)
		return header, table.Rows[1:]
	}
	return defaultColumnNames(len(table.Rows[0])), table.Rows
}

func buildImportColumns(
	configs []*importexportv1.ColumnConfig,
	header []string,
	dataRows [][]string,
) ([]importColumn, error) {
	configured := make(map[int]*importexportv1.ColumnConfig)
	for _, config := range configs {
		if config == nil || config.GetIndex() < 0 || int(config.GetIndex()) >= len(header) {
			return nil, deal.RPCError(deal.CodeInvalidArgument)
		}
		if _, duplicate := configured[int(config.GetIndex())]; duplicate {
			return nil, deal.RPCError(deal.CodeInvalidArgument)
		}
		configured[int(config.GetIndex())] = config
	}
	columns := make([]importColumn, 0, len(header))
	for index, name := range header {
		config := configured[index]
		column := importColumn{
			Index: index,
			Name:  strings.TrimSpace(name),
		}
		if column.Name == "" {
			column.Name = defaultColumnNames(len(header))[index]
		}
		if config != nil {
			column.Ignore = config.GetIgnore()
			column.Type = config.GetType()
			if strings.TrimSpace(config.GetName()) != "" {
				column.Name = strings.TrimSpace(config.GetName())
			}
		}
		if column.Type == structurev1.FieldType_FIELD_TYPE_UNSPECIFIED {
			samples := make([]string, 0, importPreviewRows)
			for rowIndex := 0; rowIndex < len(dataRows) && rowIndex < importPreviewRows; rowIndex++ {
				samples = append(samples, dataRows[rowIndex][index])
			}
			column.Type = inferFieldType(samples)
		}
		columns = append(columns, column)
	}
	return columns, nil
}

func prepareImportRows(
	dataRows [][]string,
	columns []importColumn,
	firstRowHeader bool,
) ([]preparedImportRow, []importFailure) {
	prepared := make([]preparedImportRow, 0, len(dataRows))
	failures := make([]importFailure, 0)
	for rowIndex, row := range dataRows {
		rowNumber := rowIndex + 1
		if firstRowHeader {
			rowNumber++
		}
		values := make(map[int]*commonv1.Value)
		var rowFailure *importFailure
		for _, column := range columns {
			if column.Ignore {
				continue
			}
			raw := ""
			if column.Index < len(row) {
				raw = row[column.Index]
			}
			value, err := convertImportValue(raw, column.Type)
			if err != nil {
				rowFailure = &importFailure{
					RowNumber: rowNumber,
					Column:    column.Name,
					Value:     raw,
					Reason:    err.Error(),
				}
				continue
			}
			if value != nil {
				values[column.Index] = value
			}
		}
		if rowFailure != nil {
			failures = append(failures, *rowFailure)
			continue
		}
		prepared = append(prepared, preparedImportRow{
			RowNumber: rowNumber,
			Values:    values,
		})
	}
	return prepared, failures
}

func convertImportValue(raw string, fieldType structurev1.FieldType) (*commonv1.Value, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	switch fieldType {
	case structurev1.FieldType_FIELD_TYPE_NUMBER:
		number, ok := parseNumber(raw)
		if !ok {
			return nil, fmt.Errorf("数字格式无效")
		}
		return &commonv1.Value{Kind: &commonv1.Value_DoubleValue{DoubleValue: number}}, nil
	case structurev1.FieldType_FIELD_TYPE_DATE:
		date, ok := parseDateMS(raw)
		if !ok {
			return nil, fmt.Errorf("日期格式无效")
		}
		return &commonv1.Value{Kind: &commonv1.Value_DateValueMs{DateValueMs: date}}, nil
	case structurev1.FieldType_FIELD_TYPE_URL:
		if !isHTTPURL(raw) {
			return nil, fmt.Errorf("URL 格式无效")
		}
		return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: raw}}, nil
	default:
		return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: raw}}, nil
	}
}

func (service *Service) createImportBase(
	ctx context.Context,
	task base.ImportExportTask,
	table parsedTable,
	columns []importColumn,
	principal *commonv1.Principal,
) (string, string, error) {
	if service.workspace == nil || service.structure == nil {
		return "", "", deal.RPCError(deal.CodeServiceUnavailable)
	}
	coreCtx := coreCallContext(ctx, "", "")
	fileName := taskFileName(service, ctx, task.FileID)
	baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	if strings.TrimSpace(baseName) == "" {
		baseName = "导入数据"
	}
	createdBase, err := service.workspace.CreateBase(
		coreCtx,
		&workspacev1.CreateBaseRequest{Name: baseName, ImportPending: true},
	)
	if err != nil {
		return "", "", err
	}
	if createdBase == nil || createdBase.GetBase().GetId() == "" {
		return "", "", deal.RPCError(deal.CodeServiceUnavailable)
	}
	tableName := strings.TrimSpace(table.SheetName)
	if tableName == "" || tableName == "CSV" {
		tableName = "导入数据"
	}
	baseID := createdBase.GetBase().GetId()
	coreCtx = coreCallContext(ctx, baseID, "")
	createdTable, err := service.structure.CreateTable(
		coreCtx,
		&structurev1.CreateTableRequest{
			BaseId: baseID,
			Name:   tableName,
		},
	)
	if err != nil {
		// Base 已建、表未建：带出 baseID 供失败清理。
		return createdBase.GetBase().GetId(), "", err
	}
	if createdTable == nil || createdTable.GetTable().GetId() == "" {
		return createdBase.GetBase().GetId(), "", deal.RPCError(deal.CodeServiceUnavailable)
	}
	tableID := createdTable.GetTable().GetId()
	if err := service.createImportFields(
		coreCallContext(ctx, baseID, tableID),
		tableID,
		columns,
	); err != nil {
		return baseID, "", err
	}
	return baseID, tableID, nil
}

// createImportFields 按源文件的列创建字段。structure.CreateTable 会自动带一个默认
// 主字段「标题」（ticket 02）：直接追加列会多出一列空字段，且源文件恰有「标题」列
// 时 CreateField 会因重名失败而整份导入失败。因此先复用默认主字段——源文件有同名列
// 直接占位；否则首个文本列改名占用它（主字段优先承载首列）。
func (service *Service) createImportFields(
	ctx context.Context,
	tableID string,
	columns []importColumn,
) error {
	reusable := service.reusableImportField(ctx, tableID)
	byName := make(map[string]string, 1)
	if reusable != nil {
		byName[strings.TrimSpace(reusable.GetName())] = reusable.GetId()
	}
	adopted := reusable == nil
	for index := range columns {
		if columns[index].Ignore {
			continue
		}
		name := strings.TrimSpace(columns[index].Name)
		if fieldID := byName[name]; fieldID != "" {
			columns[index].FieldID = fieldID
			continue
		}
		if !adopted && columns[index].Type == structurev1.FieldType_FIELD_TYPE_TEXT {
			if _, err := service.structure.RenameField(ctx, &structurev1.RenameFieldRequest{
				FieldId: reusable.GetId(),
				Name:    name,
			}); err == nil {
				delete(byName, strings.TrimSpace(reusable.GetName()))
				adopted = true
				columns[index].FieldID = reusable.GetId()
				byName[name] = columns[index].FieldID
				continue
			}
			// 改名失败（如列名超长）时退回新建列，导入仍可继续。
		}
		config, _ := structpb.NewStruct(map[string]any{})
		createdField, fieldErr := service.structure.CreateField(
			ctx,
			&structurev1.CreateFieldRequest{
				TableId:   tableID,
				Name:      columns[index].Name,
				Type:      columns[index].Type,
				ConfigMap: config,
			},
		)
		if fieldErr != nil {
			return fieldErr
		}
		if createdField == nil || createdField.GetField().GetId() == "" {
			return deal.RPCError(deal.CodeServiceUnavailable)
		}
		columns[index].FieldID = createdField.GetField().GetId()
		byName[name] = columns[index].FieldID
	}
	return nil
}

// reusableImportField 返回刚建表自带的默认主字段；表里不止一个字段（说明不是新建
// 的空表）时返回 nil，避免在既有表上误改字段。
func (service *Service) reusableImportField(
	ctx context.Context,
	tableID string,
) *structurev1.Field {
	response, err := service.structure.ListFields(
		ctx,
		&structurev1.ListFieldsRequest{TableId: tableID},
	)
	if err != nil {
		return nil
	}
	fields := response.GetFieldList()
	if len(fields) != 1 || !fields[0].GetIsPrimary() {
		return nil
	}
	return fields[0]
}

func taskFileName(service *Service, ctx context.Context, fileID string) string {
	meta, err := service.files.Get(ctx, fileID)
	if err != nil {
		return "导入数据"
	}
	return meta.DisplayName
}

func (service *Service) bindExistingImportFields(
	ctx context.Context,
	tableID string,
	columns []importColumn,
) error {
	if service.structure == nil {
		return deal.RPCError(deal.CodeServiceUnavailable)
	}
	response, err := service.structure.ListFields(
		coreCallContext(ctx, "", tableID),
		&structurev1.ListFieldsRequest{TableId: tableID},
	)
	if err != nil {
		return err
	}
	fields := append([]*structurev1.Field(nil), response.GetFieldList()...)
	sort.Slice(fields, func(left, right int) bool {
		return fields[left].GetPosition() < fields[right].GetPosition()
	})
	byName := make(map[string]string, len(fields))
	for _, field := range fields {
		byName[strings.TrimSpace(field.GetName())] = field.GetId()
	}
	for index := range columns {
		if columns[index].Ignore {
			continue
		}
		if fieldID := byName[columns[index].Name]; fieldID != "" {
			columns[index].FieldID = fieldID
			continue
		}
		if columns[index].Index < len(fields) {
			columns[index].FieldID = fields[columns[index].Index].GetId()
		}
		if columns[index].FieldID == "" {
			return deal.RPCError(deal.CodeInvalidArgument)
		}
	}
	return nil
}

func (service *Service) writePreparedRows(
	ctx context.Context,
	taskID string,
	baseID string,
	tableID string,
	rows []preparedImportRow,
	columns []importColumn,
) (int, []importFailure, error) {
	if service.records == nil {
		return 0, []importFailure{{
			Reason: "records service unavailable",
		}}, nil
	}
	okRows := 0
	failures := make([]importFailure, 0)
	coreCtx := coreCallContext(ctx, baseID, tableID)
	for _, row := range rows {
		// 客户端断连（缺陷 #25）：立即停止写行并转入失败路径清理半成品 Base，
		// 而不是把剩余行逐个记成失败后照常返回完成态。
		if err := ctx.Err(); err != nil {
			return okRows, failures, err
		}
		cells := make(map[string]*commonv1.Value, len(row.Values))
		for _, column := range columns {
			if column.Ignore {
				continue
			}
			value := row.Values[column.Index]
			if value == nil {
				continue
			}
			cells[column.FieldID] = value
		}
		if len(cells) == 0 {
			okRows++
			continue
		}
		// 一行 CSV/XLSX 对应一条记录：CreateRecord 一次写入整行单元格，
		// 避免逐单元格 BatchUpdate 把一行拆成多条记录。
		result, err := service.records.CreateRecord(
			coreCtx,
			&recordsv1.CreateRecordRequest{
				TableId:         tableID,
				CellsMap:        cells,
				ClientRequestId: fmt.Sprintf("%s:%d", taskID, row.RowNumber),
			},
		)
		if err != nil {
			if ctx.Err() != nil {
				return okRows, failures, ctx.Err()
			}
			failures = append(failures, importFailure{
				RowNumber: row.RowNumber,
				Reason:    err.Error(),
			})
			continue
		}
		if result.GetRecord().GetId() == "" {
			failures = append(failures, importFailure{
				RowNumber: row.RowNumber,
				Reason:    "records rejected row",
			})
			continue
		}
		okRows++
	}
	return okRows, failures, nil
}

func (service *Service) writeImportReport(
	ctx context.Context,
	task base.ImportExportTask,
	userID string,
	failures []importFailure,
) (string, error) {
	reportID, err := idgen.New()
	if err != nil {
		return "", err
	}
	relativePath := filepath.ToSlash(filepath.Join("reports", reportID+".csv"))
	fullPath := filepath.Join(service.attachDir, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		return "", err
	}
	file, err := os.OpenFile(fullPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return "", err
	}
	writer := csv.NewWriter(file)
	writeErr := writer.Write([]string{"行号", "列名", "原值", "原因"})
	for _, failure := range failures {
		if writeErr != nil {
			break
		}
		writeErr = writer.Write([]string{
			fmt.Sprintf("%d", failure.RowNumber),
			failure.Column,
			failure.Value,
			failure.Reason,
		})
	}
	writer.Flush()
	if writeErr == nil {
		writeErr = writer.Error()
	}
	closeErr := file.Close()
	if writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(fullPath)
		return "", writeErr
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	meta := base.FileMeta{
		ID:          reportID,
		Name:        "import-errors.csv",
		DisplayName: "导入错误报告.csv",
		Ext:         ".csv",
		Mime:        "text/csv",
		Size:        int64(len(content)),
		Usage:       "import_report",
		Uploader:    userID,
		StoragePath: relativePath,
		Checksum:    hex.EncodeToString(sum[:]),
		CreatedAt:   service.now().UTC().UnixMilli(),
	}
	if err := service.files.Create(ctx, meta); err != nil {
		_ = os.Remove(fullPath)
		return "", err
	}
	return reportID, nil
}

func (service *Service) publishImportFinished(
	ctx context.Context,
	task base.ImportExportTask,
	enterpriseID string,
) error {
	if service.bus == nil {
		return errors.New("import event bus is required")
	}
	var stored struct {
		CreatorID string `json:"creator_id"`
	}
	if err := json.Unmarshal(task.Payload, &stored); err != nil {
		return fmt.Errorf("decode import task payload: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"task_id":        task.ID,
		"base_id":        task.BaseID,
		"ok_rows":        task.OKRows,
		"fail_rows":      task.FailRows,
		"report_file_id": task.ReportFileID,
		"enterprise_id":  enterpriseID,
		"creator_id":     stored.CreatorID,
		"operator":       stored.CreatorID,
	})
	if err != nil {
		return err
	}
	eventID, err := idgen.New()
	if err != nil {
		return err
	}
	return service.bus.Publish(ctx, eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "import.finished",
			Version:    1,
			BaseID:     task.BaseID,
			OccurredAt: service.now().UTC().UnixMilli(),
			Producer:   "importexport",
		},
		Payload: payload,
	})
}

// failAllRowsImport 全部行失败时把任务置为 FAILED 并清理本次新建的 Base。
// 仅在后台写行（runImportWrites）中调用：响应已提前返回，这里只落地终态。
func (service *Service) failAllRowsImport(
	ctx context.Context,
	task base.ImportExportTask,
	createdBase bool,
) error {
	task.Status = statusFailed
	task.UpdatedAt = service.now().UTC().UnixMilli()
	payload := map[string]any{
		"creator_id":  creatorIDFromPayload(task.Payload),
		"dead_letter": true,
		"error":       "all rows failed",
	}
	task.Payload, _ = json.Marshal(payload)
	// 失败收尾（状态更新 + 半成品 Base 清理）用独立于请求的 context（缺陷 #25），
	// 客户端断连后清理动作仍然执行；Base 删除在 core 侧幂等。
	finishCtx, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), importFinishTimeout)
	defer cancelFinish()
	if createdBase {
		// 全行失败等同于任务失败：删除本次导入新建的 Base，避免留下可访问的空壳。
		service.cleanupFailedImportBase(finishCtx, task)
		task.BaseID = ""
		task.TableID = ""
	}
	if err := service.tasks.Update(finishCtx, task); err != nil {
		return err
	}
	return nil
}

func creatorIDFromPayload(payload []byte) string {
	var stored struct {
		CreatorID string `json:"creator_id"`
	}
	if len(payload) == 0 {
		return ""
	}
	if err := json.Unmarshal(payload, &stored); err != nil {
		return ""
	}
	return stored.CreatorID
}

func (service *Service) failImport(
	ctx context.Context,
	task base.ImportExportTask,
	cause error,
) error {
	task.Status = statusFailed
	task.UpdatedAt = service.now().UTC().UnixMilli()
	payload := make(map[string]any)
	if len(task.Payload) > 0 {
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			// 失败记录必须落地：解码失败仅记 ERROR 并降级继续（原始字段可能丢失）。
			slog.Error("importexport: decode task payload for failure record",
				"task_id", task.ID, "error", err)
		}
	}
	payload["error"] = cause.Error()
	payload["dead_letter"] = true
	task.Payload, _ = json.Marshal(payload)
	// 失败收尾（状态更新 + 半成品 Base 清理）用独立于请求的 context（缺陷 #25）：
	// 客户端断连把请求上下文取消后，失败状态与清理仍必须落地；Base 删除在
	// core 侧幂等，重复触发安全。
	finishCtx, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), importFinishTimeout)
	defer cancelFinish()
	if err := service.tasks.Update(finishCtx, task); err != nil {
		service.logger.Error("mark import failed", "task_id", task.ID, "error", err)
	}
	service.cleanupFailedImportBase(finishCtx, task)
	return cause
}

// cleanupFailedImportBase removes the half-built importing Base after a failed
// import（ticket 16：导入中的 Base 不可访问；失败即清理，进度走任务状态与报告）。
func (service *Service) cleanupFailedImportBase(
	ctx context.Context,
	task base.ImportExportTask,
) {
	if task.BaseID == "" || service.workspace == nil {
		return
	}
	if _, err := service.workspace.DeleteBase(
		coreCallContext(ctx, task.BaseID, ""),
		&workspacev1.DeleteBaseRequest{BaseId: task.BaseID, Confirm: true},
	); err != nil {
		service.logger.Error("importexport: cleanup failed import base",
			"base_id", task.BaseID, "error", err)
	}
}
