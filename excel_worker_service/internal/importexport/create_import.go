// importexport/create_import.go：预览后的逐行校验、core 写入与错误报告。
package importexport

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	worker_rpc "excel_worker_service/rpc"
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

// CreateImport persists one import task and executes its rows synchronously.
func (service *Service) CreateImport(
	ctx context.Context,
	request *importexportv1.CreateImportRequest,
) (*importexportv1.CreateImportResponse, error) {
	if err := requireImportTask(service); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if service.records == nil || service.workspace == nil ||
		service.structure == nil || service.bus == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if request == nil || request.GetFileId() == "" || request.GetSheetIndex() < 0 {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
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
			return nil, rpcError(deal.CodeInvalidArgument)
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

	enterpriseID := metadataValue(ctx, "x-enterprise-id")
	if enterpriseID == "" {
		task.Status = statusFailed
		task.Payload, _ = json.Marshal(map[string]any{
			"creator_id":         principal.GetUserId(),
			"dead_letter":        true,
			"dead_letter_reason": "x-enterprise-id is required",
		})
		task.UpdatedAt = service.now().UTC().UnixMilli()
		if updateErr := service.tasks.Update(ctx, task); updateErr != nil {
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

	table, err := service.parseImportFile(
		ctx,
		principal.GetUserId(),
		request.GetFileId(),
		int(request.GetSheetIndex()),
	)
	if err != nil {
		return nil, service.failImport(ctx, task, err)
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
	if baseID == "" {
		if len(preparedRows) > 0 {
			baseID, tableID, err = service.createImportBase(
				ctx,
				task,
				table,
				columns,
				principal,
			)
			if err != nil {
				return nil, service.failImport(ctx, task, err)
			}
		}
	} else {
		if tableID == "" {
			return nil, service.failImport(
				ctx,
				task,
				rpcError(deal.CodeInvalidArgument),
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

	okRows, batchFailures := service.writePreparedRows(
		ctx,
		task.ID,
		tableID,
		preparedRows,
		columns,
	)
	task.OKRows = int32(okRows)
	failures = append(failures, batchFailures...)
	task.FailRows = int32(len(failures))
	if len(failures) > 0 {
		reportID, reportErr := service.writeImportReport(
			ctx,
			task,
			principal.GetUserId(),
			failures,
		)
		if reportErr != nil {
			return nil, service.failImport(ctx, task, reportErr)
		}
		task.ReportFileID = reportID
	}
	task.Status = statusDone
	task.UpdatedAt = service.now().UTC().UnixMilli()
	task.Payload, _ = json.Marshal(map[string]any{
		"creator_id": principal.GetUserId(),
	})
	if err := service.tasks.Update(ctx, task); err != nil {
		return nil, err
	}
	if err := service.publishImportFinished(ctx, task, enterpriseID); err != nil {
		return nil, err
	}
	protoTask, err := taskToProto(task)
	if err != nil {
		return nil, err
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
			return nil, rpcError(deal.CodeInvalidArgument)
		}
		if _, duplicate := configured[int(config.GetIndex())]; duplicate {
			return nil, rpcError(deal.CodeInvalidArgument)
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
		return "", "", rpcError(deal.CodeServiceUnavailable)
	}
	coreCtx := forwardingContext(ctx)
	fileName := taskFileName(service, ctx, task.FileID)
	baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	if strings.TrimSpace(baseName) == "" {
		baseName = "导入数据"
	}
	createdBase, err := service.workspace.CreateBase(
		coreCtx,
		&workspacev1.CreateBaseRequest{Name: baseName},
	)
	if err != nil {
		return "", "", err
	}
	if createdBase == nil || createdBase.GetBase().GetId() == "" {
		return "", "", rpcError(deal.CodeServiceUnavailable)
	}
	tableName := strings.TrimSpace(table.SheetName)
	if tableName == "" || tableName == "CSV" {
		tableName = "导入数据"
	}
	createdTable, err := service.structure.CreateTable(
		coreCtx,
		&structurev1.CreateTableRequest{
			BaseId: createdBase.GetBase().GetId(),
			Name:   tableName,
		},
	)
	if err != nil {
		return "", "", err
	}
	if createdTable == nil || createdTable.GetTable().GetId() == "" {
		return "", "", rpcError(deal.CodeServiceUnavailable)
	}
	for index := range columns {
		if columns[index].Ignore {
			continue
		}
		config, _ := structpb.NewStruct(map[string]any{})
		createdField, fieldErr := service.structure.CreateField(
			coreCtx,
			&structurev1.CreateFieldRequest{
				TableId:   createdTable.GetTable().GetId(),
				Name:      columns[index].Name,
				Type:      columns[index].Type,
				ConfigMap: config,
			},
		)
		if fieldErr != nil {
			return "", "", fieldErr
		}
		if createdField == nil || createdField.GetField().GetId() == "" {
			return "", "", rpcError(deal.CodeServiceUnavailable)
		}
		columns[index].FieldID = createdField.GetField().GetId()
	}
	return createdBase.GetBase().GetId(), createdTable.GetTable().GetId(), nil
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
		return rpcError(deal.CodeServiceUnavailable)
	}
	response, err := service.structure.ListFields(
		forwardingContext(ctx),
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
			return rpcError(deal.CodeInvalidArgument)
		}
	}
	return nil
}

func (service *Service) writePreparedRows(
	ctx context.Context,
	taskID string,
	tableID string,
	rows []preparedImportRow,
	columns []importColumn,
) (int, []importFailure) {
	if service.records == nil {
		return 0, []importFailure{{
			Reason: "records service unavailable",
		}}
	}
	okRows := 0
	failures := make([]importFailure, 0)
	coreCtx := forwardingContext(ctx)
	for _, row := range rows {
		operations := make([]*recordsv1.BatchOperation, 0, len(row.Values))
		for _, column := range columns {
			if column.Ignore {
				continue
			}
			value := row.Values[column.Index]
			if value == nil {
				continue
			}
			operations = append(operations, &recordsv1.BatchOperation{
				FieldId: column.FieldID,
				Value:   value,
			})
		}
		if len(operations) == 0 {
			okRows++
			continue
		}
		result, err := service.records.BatchUpdate(
			coreCtx,
			&recordsv1.BatchUpdateRequest{
				OpList:          operations,
				ClientRequestId: fmt.Sprintf("%s:%d", taskID, row.RowNumber),
			},
		)
		if err != nil {
			failures = append(failures, importFailure{
				RowNumber: row.RowNumber,
				Reason:    err.Error(),
			})
			continue
		}
		if len(result.GetFailRowList()) > 0 || hasFailedOperation(result) {
			reason := "records rejected row"
			if failed := result.GetFailRowList(); len(failed) > 0 {
				reason = fmt.Sprintf("records error %d", failed[0].GetErrorCode())
			}
			failures = append(failures, importFailure{
				RowNumber: row.RowNumber,
				Reason:    reason,
			})
			continue
		}
		okRows++
	}
	return okRows, failures
}

func hasFailedOperation(response *recordsv1.BatchUpdateResponse) bool {
	for _, result := range response.GetResultList() {
		if !result.GetOk() {
			return true
		}
	}
	return false
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
	_ = json.Unmarshal(task.Payload, &stored)
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

func (service *Service) failImport(
	ctx context.Context,
	task base.ImportExportTask,
	cause error,
) error {
	task.Status = statusFailed
	task.UpdatedAt = service.now().UTC().UnixMilli()
	payload := make(map[string]any)
	if len(task.Payload) > 0 {
		_ = json.Unmarshal(task.Payload, &payload)
	}
	payload["error"] = cause.Error()
	payload["dead_letter"] = true
	task.Payload, _ = json.Marshal(payload)
	if err := service.tasks.Update(ctx, task); err != nil {
		service.logger.Error("mark import failed", "task_id", task.ID, "error", err)
	}
	return cause
}

func forwardingContext(ctx context.Context) context.Context {
	return worker_rpc.ForwardMetadata(
		ctx,
		"x-principal",
		"x-enterprise-id",
		"x-base-id",
		"x-request-id",
	)
}
