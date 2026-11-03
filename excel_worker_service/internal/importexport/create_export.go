// importexport/create_export.go：当前视图分页拉取、文件生成与审计事件。
package importexport

import (
	"bytes"
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
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"google.golang.org/protobuf/types/known/emptypb"

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

type exportResult struct {
	Task   base.ImportExportTask
	FileID string
}

// CreateExport executes one current-view export and returns its durable task.
func (service *Service) CreateExport(
	ctx context.Context,
	request *importexportv1.CreateExportRequest,
) (*importexportv1.CreateExportResponse, error) {
	if service == nil || service.tasks == nil || service.files == nil ||
		service.records == nil || service.workspace == nil || service.structure == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if request == nil ||
		request.GetBaseId() == "" ||
		request.GetTableId() == "" ||
		request.GetViewId() == "" ||
		(request.GetFormat() != importexportv1.ExportFormat_EXPORT_FORMAT_XLSX &&
			request.GetFormat() != importexportv1.ExportFormat_EXPORT_FORMAT_CSV) {
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
		case lookupErr == nil && existing.Kind != "export":
			return nil, rpcError(deal.CodeInvalidArgument)
		case lookupErr == nil:
			protoTask, conversionErr := exportTaskToProto(existing)
			if conversionErr != nil {
				return nil, conversionErr
			}
			return &importexportv1.CreateExportResponse{ExportTask: protoTask}, nil
		case lookupErr != nil && !errors.Is(lookupErr, base.ErrImportExportTaskNotFound):
			return nil, lookupErr
		}
	}
	enterpriseID := metadataValue(ctx, "x-enterprise-id")
	if enterpriseID == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}

	now := service.now().UTC()
	taskID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	format := strings.ToLower(request.GetFormat().String())
	format = strings.TrimPrefix(format, "export_format_")
	payload, _ := json.Marshal(exportPayload{CreatorID: principal.GetUserId()})
	task, err := service.tasks.Create(ctx, base.ImportExportTask{
		ID:              taskID,
		Kind:            "export",
		Status:          statusPending,
		BaseID:          request.GetBaseId(),
		TableID:         request.GetTableId(),
		ViewID:          request.GetViewId(),
		Format:          format,
		ClientRequestID: request.GetClientRequestId(),
		Payload:         payload,
		CreatedAt:       now.UnixMilli(),
		UpdatedAt:       now.UnixMilli(),
	})
	if err != nil {
		return nil, err
	}
	if task.ID != taskID {
		protoTask, conversionErr := exportTaskToProto(task)
		if conversionErr != nil {
			return nil, conversionErr
		}
		return &importexportv1.CreateExportResponse{ExportTask: protoTask}, nil
	}

	result, err := service.generateExport(
		ctx,
		task,
		principal.GetUserId(),
		enterpriseID,
	)
	if err != nil {
		task.Status = statusFailed
		task.UpdatedAt = service.now().UTC().UnixMilli()
		task.Payload, _ = json.Marshal(exportPayload{
			CreatorID: principal.GetUserId(),
			Error:     err.Error(),
		})
		_ = service.tasks.Update(ctx, task)
		return nil, err
	}
	task = result.Task
	if err := service.tasks.Update(ctx, task); err != nil {
		return nil, err
	}
	if err := service.publishExportAudited(ctx, task, principal.GetUserId(), enterpriseID); err != nil {
		return nil, err
	}
	protoTask, err := exportTaskToProto(task)
	if err != nil {
		return nil, err
	}
	return &importexportv1.CreateExportResponse{ExportTask: protoTask}, nil
}

func (service *Service) generateExport(
	ctx context.Context,
	task base.ImportExportTask,
	operator string,
	enterpriseID string,
) (exportResult, error) {
	coreCtx := forwardingContext(ctx)
	baseName, err := service.exportBaseName(coreCtx, task.BaseID)
	if err != nil {
		return exportResult{}, err
	}
	tableName, err := service.exportTableName(coreCtx, task.BaseID, task.TableID)
	if err != nil {
		return exportResult{}, err
	}
	viewName, err := service.exportViewName(coreCtx, task.TableID, task.ViewID)
	if err != nil {
		return exportResult{}, err
	}
	fieldsResponse, err := service.structure.ListFields(
		coreCtx,
		&structurev1.ListFieldsRequest{TableId: task.TableID},
	)
	if err != nil {
		return exportResult{}, err
	}
	viewConfig, err := service.structure.GetViewConfig(
		coreCtx,
		&structurev1.GetViewConfigRequest{ViewId: task.ViewID},
	)
	if err != nil {
		return exportResult{}, err
	}
	hidden := make(map[string]struct{})
	for _, fieldID := range viewConfig.GetViewConfig().GetHiddenFieldList() {
		hidden[fieldID] = struct{}{}
	}
	fields := append([]*structurev1.Field(nil), fieldsResponse.GetFieldList()...)
	sort.Slice(fields, func(left, right int) bool {
		return fields[left].GetPosition() < fields[right].GetPosition()
	})
	visibleFields := make([]*structurev1.Field, 0, len(fields))
	for _, field := range fields {
		if _, isHidden := hidden[field.GetId()]; !isHidden {
			visibleFields = append(visibleFields, field)
		}
	}

	records, err := service.queryAllViewRows(coreCtx, task)
	if err != nil {
		return exportResult{}, err
	}
	rowList := make([]string, 0, len(records))
	tableRows := make([][]string, 0, len(records)+1)
	header := make([]string, len(visibleFields))
	for index, field := range visibleFields {
		header[index] = field.GetName()
	}
	tableRows = append(tableRows, header)
	for _, record := range records {
		rowList = append(rowList, record.GetId())
		row := make([]string, len(visibleFields))
		for index, field := range visibleFields {
			row[index] = exportValue(record.GetCellsMap()[field.GetId()])
		}
		tableRows = append(tableRows, row)
	}
	content, err := encodeExport(tableRows, task.Format, tableName)
	if err != nil {
		return exportResult{}, err
	}
	fileName := fmt.Sprintf(
		"%s_%s_%s_%s.%s",
		safeFileNamePart(baseName),
		safeFileNamePart(tableName),
		safeFileNamePart(viewName),
		service.now().Format("20060102"),
		task.Format,
	)
	fileID, err := service.storeExportFile(
		ctx,
		operator,
		fileName,
		content,
	)
	if err != nil {
		return exportResult{}, err
	}
	task.Status = statusDone
	task.FileID = fileID
	task.TotalRows = int32(len(records))
	task.OKRows = int32(len(records))
	task.UpdatedAt = service.now().UTC().UnixMilli()
	task.Payload, _ = json.Marshal(exportPayload{
		FileName:     fileName,
		Path:         filepath.ToSlash(filepath.Join("exports", task.ID+"."+task.Format)),
		RowList:      rowList,
		CreatorID:    operator,
		EnterpriseID: enterpriseID,
	})
	return exportResult{Task: task, FileID: fileID}, nil
}

func (service *Service) exportBaseName(ctx context.Context, baseID string) (string, error) {
	response, err := service.workspace.GetHome(ctx, &emptypb.Empty{})
	if err != nil {
		return "", err
	}
	if base := findBase(response.GetHome(), baseID); base != nil {
		return base.GetName(), nil
	}
	return "", rpcError(deal.CodeDeletedResource)
}

func findBase(home *workspacev1.Home, baseID string) *workspacev1.Base {
	if home == nil {
		return nil
	}
	lists := [][]*workspacev1.Base{
		home.GetRecentList(),
		home.GetCreatedList(),
		home.GetSharedList(),
		home.GetStarList(),
	}
	for _, list := range lists {
		for _, base := range list {
			if base.GetId() == baseID {
				return base
			}
		}
	}
	return nil
}

func (service *Service) exportTableName(
	ctx context.Context,
	baseID string,
	tableID string,
) (string, error) {
	response, err := service.structure.ListTables(
		ctx,
		&structurev1.ListTablesRequest{BaseId: baseID},
	)
	if err != nil {
		return "", err
	}
	for _, table := range response.GetTableList() {
		if table.GetId() == tableID {
			return table.GetName(), nil
		}
	}
	return "", rpcError(deal.CodeDeletedResource)
}

func (service *Service) exportViewName(
	ctx context.Context,
	tableID string,
	viewID string,
) (string, error) {
	response, err := service.structure.ListViews(
		ctx,
		&structurev1.ListViewsRequest{TableId: tableID},
	)
	if err != nil {
		return "", err
	}
	for _, view := range response.GetViewList() {
		if view.GetId() == viewID {
			return view.GetName(), nil
		}
	}
	return "", rpcError(deal.CodeDeletedResource)
}

func (service *Service) queryAllViewRows(
	ctx context.Context,
	task base.ImportExportTask,
) ([]*recordsv1.Record, error) {
	records := make([]*recordsv1.Record, 0)
	cursor := ""
	seen := make(map[string]struct{})
	for {
		response, err := service.records.QueryView(
			ctx,
			&recordsv1.QueryViewRequest{
				BaseId:  task.BaseID,
				TableId: task.TableID,
				ViewId:  task.ViewID,
				Cursor:  cursor,
				Limit:   100,
			},
		)
		if err != nil {
			return nil, err
		}
		records = append(records, response.GetRecordList()...)
		if !response.GetHasMore() {
			return records, nil
		}
		cursor = response.GetNextCursor()
		if cursor == "" {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
		if _, duplicate := seen[cursor]; duplicate {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
		seen[cursor] = struct{}{}
	}
}

func exportValue(value *commonv1.Value) string {
	if value == nil || value.GetKind() == nil {
		return ""
	}
	switch kind := value.GetKind().(type) {
	case *commonv1.Value_StringValue:
		return kind.StringValue
	case *commonv1.Value_IntValue:
		return strconv.FormatInt(kind.IntValue, 10)
	case *commonv1.Value_DoubleValue:
		return strconv.FormatFloat(kind.DoubleValue, 'f', -1, 64)
	case *commonv1.Value_BoolValue:
		return strconv.FormatBool(kind.BoolValue)
	case *commonv1.Value_DateValueMs:
		return time.UnixMilli(kind.DateValueMs).Format("2006-01-02 15:04:05")
	case *commonv1.Value_ListValue:
		values := make([]string, 0, len(kind.ListValue.GetValueList()))
		for _, item := range kind.ListValue.GetValueList() {
			values = append(values, exportValue(item))
		}
		return strings.Join(values, ",")
	default:
		return ""
	}
}

func encodeExport(rows [][]string, format string, sheetName string) ([]byte, error) {
	switch format {
	case "csv":
		var buffer bytes.Buffer
		writer := csv.NewWriter(&buffer)
		for _, row := range rows {
			if err := writer.Write(row); err != nil {
				return nil, err
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, err
		}
		return buffer.Bytes(), nil
	case "xlsx":
		workbook := excelize.NewFile()
		sheet := sanitizeSheetName(sheetName)
		if sheet == "" {
			sheet = "导出"
		}
		if sheet != "Sheet1" {
			if err := workbook.SetSheetName("Sheet1", sheet); err != nil {
				return nil, err
			}
		}
		for rowIndex, row := range rows {
			cell, _ := excelize.CoordinatesToCellName(1, rowIndex+1)
			values := make([]any, len(row))
			for index := range row {
				values[index] = row[index]
			}
			if err := workbook.SetSheetRow(sheet, cell, &values); err != nil {
				_ = workbook.Close()
				return nil, err
			}
		}
		var buffer bytes.Buffer
		if err := workbook.Write(&buffer); err != nil {
			_ = workbook.Close()
			return nil, err
		}
		_ = workbook.Close()
		return buffer.Bytes(), nil
	default:
		return nil, rpcError(deal.CodeInvalidArgument)
	}
}

func sanitizeSheetName(value string) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer(
		":", "_", "\\", "_", "/", "_", "?", "_", "*", "_", "[", "_", "]", "_",
	)
	value = replacer.Replace(value)
	if len([]rune(value)) > 31 {
		value = string([]rune(value)[:31])
	}
	return value
}

func safeFileNamePart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_").Replace(value)
	if value == "" {
		return "未命名"
	}
	return value
}

func (service *Service) storeExportFile(
	ctx context.Context,
	operator string,
	fileName string,
	content []byte,
) (string, error) {
	fileID, err := idgen.New()
	if err != nil {
		return "", err
	}
	format := strings.ToLower(filepath.Ext(fileName))
	relativePath := filepath.ToSlash(filepath.Join("exports", fileID+format))
	fullPath := filepath.Join(service.attachDir, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(fullPath, content, 0o640); err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	meta := base.FileMeta{
		ID:          fileID,
		Name:        fileName,
		DisplayName: fileName,
		Ext:         format,
		Mime:        exportMIME(format),
		Size:        int64(len(content)),
		Usage:       "export",
		Uploader:    operator,
		StoragePath: relativePath,
		Checksum:    hex.EncodeToString(sum[:]),
		CreatedAt:   service.now().UTC().UnixMilli(),
	}
	if err := service.files.Create(ctx, meta); err != nil {
		_ = os.Remove(fullPath)
		return "", err
	}
	return fileID, nil
}

func exportMIME(extension string) string {
	switch extension {
	case ".csv":
		return "text/csv"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	default:
		return "application/octet-stream"
	}
}

func (service *Service) publishExportAudited(
	ctx context.Context,
	task base.ImportExportTask,
	operator string,
	enterpriseID string,
) error {
	if service.bus == nil {
		return errors.New("export event bus is required")
	}
	var stored exportPayload
	if err := json.Unmarshal(task.Payload, &stored); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"operator":      operator,
		"base_id":       task.BaseID,
		"table_id":      task.TableID,
		"view_id":       task.ViewID,
		"format":        task.Format,
		"row_list":      stored.RowList,
		"enterprise_id": enterpriseID,
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
			Type:       "export.audited",
			Version:    1,
			BaseID:     task.BaseID,
			OccurredAt: service.now().UTC().UnixMilli(),
			Producer:   "importexport",
		},
		Payload: payload,
	})
}
