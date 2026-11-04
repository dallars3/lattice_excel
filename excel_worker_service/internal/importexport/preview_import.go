// importexport/preview_import.go：xlsx/CSV 读取、首行提示与列类型识别。
package importexport

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/extrame/xls"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"

	importexportv1 "excel/api/importexport/v1"
	structurev1 "excel/api/structure/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
)

type parsedTable struct {
	SheetName   string
	Rows        [][]string
	Header      []string
	DataRows    [][]string
	HeaderGuess bool
}

// PreviewImport parses one worksheet and returns inferred column metadata.
func (service *Service) PreviewImport(
	ctx context.Context,
	request *importexportv1.PreviewImportRequest,
) (*importexportv1.PreviewImportResponse, error) {
	if service == nil || service.files == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if request == nil || request.GetFileId() == "" || request.GetSheetIndex() < 0 {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	table, err := service.parseImportFile(
		ctx,
		principal.GetUserId(),
		request.GetFileId(),
		int(request.GetSheetIndex()),
	)
	if err != nil {
		return nil, err
	}
	return &importexportv1.PreviewImportResponse{
		Preview: previewFromTable(table),
	}, nil
}

func (service *Service) parseImportFile(
	ctx context.Context,
	userID string,
	fileID string,
	sheetIndex int,
) (parsedTable, error) {
	meta, err := service.files.Get(ctx, fileID)
	if errors.Is(err, base.ErrFileNotFound) {
		return parsedTable{}, rpcError(deal.CodeDeletedResource)
	}
	if err != nil {
		return parsedTable{}, err
	}
	if meta.Uploader != userID {
		return parsedTable{}, rpcError(deal.CodePermissionDenied)
	}
	if meta.Size <= 0 || meta.Size > maxImportFileBytes {
		return parsedTable{}, rpcError(deal.CodeFileRejected)
	}
	path := filepath.Join(service.attachDir, filepath.FromSlash(meta.StoragePath))
	switch strings.ToLower(strings.TrimPrefix(meta.Ext, ".")) {
	case "xlsx":
		return parseXLSX(path, sheetIndex)
	case "xls":
		return parseXLS(path, sheetIndex)
	case "csv":
		return parseCSV(path)
	default:
		return parsedTable{}, rpcError(deal.CodeInvalidArgument)
	}
}

func parseXLSX(path string, sheetIndex int) (parsedTable, error) {
	workbook, err := excelize.OpenFile(path)
	if err != nil {
		return parsedTable{}, rpcError(deal.CodeInvalidArgument)
	}
	defer workbook.Close()
	sheets := workbook.GetSheetList()
	if sheetIndex < 0 || sheetIndex >= len(sheets) {
		return parsedTable{}, rpcError(deal.CodeInvalidArgument)
	}
	rows, err := workbook.GetRows(sheets[sheetIndex])
	if err != nil {
		return parsedTable{}, rpcError(deal.CodeInvalidArgument)
	}
	return buildParsedTable(sheets[sheetIndex], rows)
}

func parseCSV(path string) (parsedTable, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return parsedTable{}, rpcError(deal.CodeDeletedResource)
		}
		return parsedTable{}, err
	}
	if len(content) > maxImportFileBytes {
		return parsedTable{}, rpcError(deal.CodeFileRejected)
	}
	content = bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(content) {
		content, err = simplifiedchinese.GBK.NewDecoder().Bytes(content)
		if err != nil {
			return parsedTable{}, rpcError(deal.CodeInvalidArgument)
		}
	}
	reader := csv.NewReader(bytes.NewReader(content))
	reader.FieldsPerRecord = -1
	rows := make([][]string, 0)
	for {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return parsedTable{}, rpcError(deal.CodeInvalidArgument)
		}
		rows = append(rows, row)
		if len(rows) > maxImportRows+1 {
			return parsedTable{}, rpcError(deal.CodeCapacityLimit)
		}
	}
	return buildParsedTable("CSV", rows)
}

func parseXLS(path string, sheetIndex int) (parsedTable, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return parsedTable{}, rpcError(deal.CodeDeletedResource)
		}
		return parsedTable{}, rpcError(deal.CodeInvalidArgument)
	}
	defer file.Close()

	workbook, err := xls.OpenReader(file, "utf-8")
	if err != nil {
		return parsedTable{}, rpcError(deal.CodeInvalidArgument)
	}
	if sheetIndex < 0 || sheetIndex >= workbook.NumSheets() {
		return parsedTable{}, rpcError(deal.CodeInvalidArgument)
	}
	sheet := workbook.GetSheet(sheetIndex)
	if sheet == nil {
		return parsedTable{}, rpcError(deal.CodeInvalidArgument)
	}
	rows := make([][]string, 0, int(sheet.MaxRow)+1)
	for index := 0; index <= int(sheet.MaxRow); index++ {
		row := xlsRow(sheet, index)
		if row == nil {
			rows = append(rows, nil)
			continue
		}
		cells := make([]string, 0, row.LastCol()+1)
		for column := 0; column <= row.LastCol(); column++ {
			cells = append(cells, row.Col(column))
		}
		for len(cells) > 0 && strings.TrimSpace(cells[len(cells)-1]) == "" {
			cells = cells[:len(cells)-1]
		}
		rows = append(rows, cells)
	}
	return buildParsedTable(sheet.Name, rows)
}

// xlsRow returns the row at index, or nil when the row is absent. The
// extrame/xls Row accessor dereferences nil for missing rows, so guard it.
func xlsRow(sheet *xls.WorkSheet, index int) (row *xls.Row) {
	defer func() {
		if recover() != nil {
			row = nil
		}
	}()
	return sheet.Row(index)
}

func buildParsedTable(sheetName string, rows [][]string) (parsedTable, error) {
	rows = trimEmptyRows(rows)
	if len(rows) > maxImportRows+1 {
		return parsedTable{}, rpcError(deal.CodeCapacityLimit)
	}
	if len(rows) == 0 {
		return parsedTable{SheetName: sheetName, Rows: [][]string{}}, nil
	}
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	if width == 0 {
		return parsedTable{SheetName: sheetName, Rows: [][]string{}}, nil
	}
	normalized := make([][]string, len(rows))
	for index, row := range rows {
		normalized[index] = make([]string, width)
		copy(normalized[index], row)
	}
	table := parsedTable{
		SheetName:   sheetName,
		Rows:        normalized,
		HeaderGuess: looksLikeHeader(normalized[0]),
	}
	if table.HeaderGuess {
		table.Header = append([]string(nil), normalized[0]...)
		table.DataRows = normalized[1:]
	} else {
		table.Header = defaultColumnNames(width)
		table.DataRows = normalized
	}
	return table, nil
}

func trimEmptyRows(rows [][]string) [][]string {
	start := 0
	for start < len(rows) && rowEmpty(rows[start]) {
		start++
	}
	end := len(rows)
	for end > start && rowEmpty(rows[end-1]) {
		end--
	}
	return rows[start:end]
}

func rowEmpty(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func looksLikeHeader(row []string) bool {
	nonEmpty := 0
	for _, value := range row {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		nonEmpty++
		if _, ok := parseNumber(value); ok {
			return false
		}
		if _, ok := parseDateMS(value); ok {
			return false
		}
		if isHTTPURL(value) {
			return false
		}
	}
	return nonEmpty > 0
}

func defaultColumnNames(width int) []string {
	names := make([]string, width)
	for index := range names {
		names[index] = "列" + excelColumnName(index)
	}
	return names
}

func excelColumnName(index int) string {
	name := ""
	for index >= 0 {
		name = string(rune('A'+index%26)) + name
		index = index/26 - 1
	}
	return name
}

func previewFromTable(table parsedTable) *importexportv1.Preview {
	columns := make([]*importexportv1.PreviewColumn, len(table.Header))
	for index, name := range table.Header {
		samples := make([]string, 0, importPreviewRows)
		for rowIndex := 0; rowIndex < len(table.DataRows) && rowIndex < importPreviewRows; rowIndex++ {
			if index < len(table.DataRows[rowIndex]) {
				samples = append(samples, table.DataRows[rowIndex][index])
			}
		}
		columns[index] = &importexportv1.PreviewColumn{
			Index:           int32(index),
			Name:            strings.TrimSpace(name),
			DetectedType:    inferFieldType(samples),
			SampleValueList: samples,
			Ignore:          false,
		}
	}
	return &importexportv1.Preview{
		ColumnList:         columns,
		FirstRowHeaderHint: table.HeaderGuess,
		TotalRows:          int32(len(table.DataRows)),
	}
}

func inferFieldType(samples []string) structurev1.FieldType {
	nonEmpty := 0
	numbers, dates, urls := 0, 0, 0
	for _, sample := range samples {
		sample = strings.TrimSpace(sample)
		if sample == "" {
			continue
		}
		nonEmpty++
		if _, ok := parseNumber(sample); ok {
			numbers++
		}
		if _, ok := parseDateMS(sample); ok {
			dates++
		}
		if isHTTPURL(sample) {
			urls++
		}
	}
	switch {
	case nonEmpty == 0:
		return structurev1.FieldType_FIELD_TYPE_TEXT
	case numbers == nonEmpty:
		return structurev1.FieldType_FIELD_TYPE_NUMBER
	case dates == nonEmpty:
		return structurev1.FieldType_FIELD_TYPE_DATE
	case urls == nonEmpty:
		return structurev1.FieldType_FIELD_TYPE_URL
	default:
		return structurev1.FieldType_FIELD_TYPE_TEXT
	}
}

func parseNumber(value string) (float64, bool) {
	normalized := strings.TrimSpace(value)
	normalized = strings.ReplaceAll(normalized, ",", "")
	normalized = strings.TrimSpace(strings.TrimSuffix(normalized, "%"))
	normalized = strings.TrimLeft(normalized, "¥￥$€£")
	number, err := strconv.ParseFloat(normalized, 64)
	return number, err == nil
}

func parseDateMS(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if !strings.ContainsAny(value, "-/年月日") {
		return 0, false
	}
	formats := []string{
		"2006-01-02",
		"2006-1-2",
		"2006/01/02",
		"2006/1/2",
		"01/02/2006",
		"1/2/2006",
		"2006年1月2日",
		"2006年01月02日",
		"2006-01-02 15:04:05",
		"2006-1-2 15:04:05",
		"2006/01/02 15:04:05",
		"2006/1/2 15:04:05",
		time.RFC3339,
	}
	for _, layout := range formats {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed.UnixMilli(), true
		}
	}
	return 0, false
}

func isHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func requireImportTask(service *Service) error {
	if service == nil || service.tasks == nil || service.files == nil {
		return fmt.Errorf("importexport service dependencies are incomplete")
	}
	return nil
}
