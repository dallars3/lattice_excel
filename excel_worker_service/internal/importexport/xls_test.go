package importexport_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"excel-libs/eventbus"
	importexportv1 "excel/api/importexport/v1"
	structurev1 "excel/api/structure/v1"
)

func seedXLSFile(t *testing.T, harness *importExportHarness, fixture string, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", fixture, err)
	}
	path := "files/" + name
	fullPath := filepath.Join(harness.attachDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(fullPath, content, 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return seedImportFile(t, harness, name, path, content)
}

func TestPreviewImportDetectsXLSColumnsAndHeader(t *testing.T) {
	harness := newImportExportHarness(t)
	fileID := seedXLSFile(t, harness, "Table.xls", "people.xls")

	response, err := harness.client.PreviewImport(
		harness.ctx,
		&importexportv1.PreviewImportRequest{FileId: fileID, SheetIndex: 0},
	)
	if err != nil {
		t.Fatalf("PreviewImport() error = %v", err)
	}
	preview := response.GetPreview()
	if preview.GetTotalRows() != 11 || !preview.GetFirstRowHeaderHint() {
		t.Fatalf("preview metadata = %+v, want 11 rows and header hint", preview)
	}
	wantTypes := []structurev1.FieldType{
		structurev1.FieldType_FIELD_TYPE_TEXT,
		structurev1.FieldType_FIELD_TYPE_TEXT,
		structurev1.FieldType_FIELD_TYPE_TEXT,
	}
	wantNames := []string{"Code", "Name", "Description"}
	columns := preview.GetColumnList()
	if len(columns) != len(wantTypes) {
		t.Fatalf("columns = %+v", columns)
	}
	for index := range columns {
		if columns[index].GetDetectedType() != wantTypes[index] ||
			columns[index].GetName() != wantNames[index] {
			t.Fatalf(
				"column %d = type %s name %q, want type %s name %q",
				index,
				columns[index].GetDetectedType(),
				columns[index].GetName(),
				wantTypes[index],
				wantNames[index],
			)
		}
	}
}

func TestCreateImportXLSAllRowsSucceedAndPublishEvent(t *testing.T) {
	harness := newImportExportHarness(t)
	harness.structure.fields = []*structurev1.Field{
		{Id: "field_code", Name: "Code", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 0},
		{Id: "field_name", Name: "Name", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 1},
		{Id: "field_desc", Name: "Description", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 2},
	}
	fileID := seedXLSFile(t, harness, "Table.xls", "codes.xls")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	subscription, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic:     "import.finished",
		Mode:      eventbus.DeliveryModePubSub,
		QueueSize: 1,
	})
	if err != nil {
		t.Fatalf("Subscribe(import.finished) error = %v", err)
	}
	defer subscription.Close()

	created, err := harness.client.CreateImport(harness.ctx, &importexportv1.CreateImportRequest{
		FileId:         fileID,
		FirstRowHeader: true,
		ColumnConfigList: []*importexportv1.ColumnConfig{
			{Index: 0, Name: "Code", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
			{Index: 1, Name: "Name", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
			{Index: 2, Name: "Description", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
		},
		ClientRequestId: "import-xls-full-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v", err)
	}
	task := created.GetImportTask()
	if task.GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_DONE ||
		task.GetTotalRows() != 11 ||
		task.GetOkRows() != 11 ||
		task.GetFailRows() != 0 {
		t.Fatalf("import task = %+v", task)
	}
	if len(harness.records.batchRequests) != 11 {
		t.Fatalf("BatchUpdate calls = %d, want 11", len(harness.records.batchRequests))
	}
	first := harness.records.batchRequests[0].GetOpList()
	if len(first) != 3 ||
		first[0].GetValue().GetStringValue() != "code1" ||
		first[1].GetValue().GetStringValue() != "name1" ||
		first[2].GetValue().GetStringValue() != "description1" {
		t.Fatalf("first row operations = %+v", first)
	}

	delivery, err := subscription.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive(import.finished) error = %v", err)
	}
	if delivery.Event.Header.Type != "import.finished" ||
		delivery.Event.Header.EventID == "" ||
		delivery.Event.Header.Producer != "importexport" {
		t.Fatalf("event header = %+v", delivery.Event.Header)
	}
	var payload map[string]any
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode import.finished payload: %v", err)
	}
	if payload["task_id"] != task.GetId() ||
		payload["base_id"] != harness.baseID ||
		payload["ok_rows"] != float64(11) ||
		payload["fail_rows"] != float64(0) ||
		payload["enterprise_id"] != harness.enterpriseID ||
		payload["creator_id"] != harness.userID ||
		payload["operator"] != harness.userID {
		t.Fatalf("import.finished payload = %#v", payload)
	}
}

func TestCreateImportXLSContinuesAfterBadRowAndWritesReport(t *testing.T) {
	harness := newImportExportHarness(t)
	harness.structure.fields = []*structurev1.Field{
		{Id: "field_test1", Name: "Test1", Type: structurev1.FieldType_FIELD_TYPE_NUMBER, Position: 0},
		{Id: "field_lorem", Name: "Lorem", Type: structurev1.FieldType_FIELD_TYPE_DATE, Position: 1},
		{Id: "field_ipsum", Name: "Ipsum", Type: structurev1.FieldType_FIELD_TYPE_DATE, Position: 2},
	}
	fileID := seedXLSFile(t, harness, "mixed.xls", "mixed.xls")

	preview, err := harness.client.PreviewImport(
		harness.ctx,
		&importexportv1.PreviewImportRequest{FileId: fileID, SheetIndex: 0},
	)
	if err != nil {
		t.Fatalf("PreviewImport() error = %v", err)
	}
	previewColumns := preview.GetPreview().GetColumnList()
	if len(previewColumns) != 3 ||
		previewColumns[0].GetDetectedType() != structurev1.FieldType_FIELD_TYPE_TEXT ||
		previewColumns[1].GetDetectedType() != structurev1.FieldType_FIELD_TYPE_DATE ||
		previewColumns[2].GetDetectedType() != structurev1.FieldType_FIELD_TYPE_DATE ||
		!preview.GetPreview().GetFirstRowHeaderHint() ||
		preview.GetPreview().GetTotalRows() != 2 {
		t.Fatalf("preview = %+v", preview.GetPreview())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	subscription, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic:     "import.finished",
		Mode:      eventbus.DeliveryModePubSub,
		QueueSize: 1,
	})
	if err != nil {
		t.Fatalf("Subscribe(import.finished) error = %v", err)
	}
	defer subscription.Close()

	created, err := harness.client.CreateImport(harness.ctx, &importexportv1.CreateImportRequest{
		FileId:         fileID,
		FirstRowHeader: true,
		ColumnConfigList: []*importexportv1.ColumnConfig{
			{Index: 0, Name: "Test1", Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
			{Index: 1, Name: "Lorem", Type: structurev1.FieldType_FIELD_TYPE_DATE},
			{Index: 2, Name: "Ipsum", Type: structurev1.FieldType_FIELD_TYPE_DATE},
		},
		ClientRequestId: "import-xls-partial-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v", err)
	}
	task := created.GetImportTask()
	if task.GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_DONE ||
		task.GetTotalRows() != 2 ||
		task.GetOkRows() != 1 ||
		task.GetFailRows() != 1 ||
		task.GetReportFileId() == "" {
		t.Fatalf("import task = %+v", task)
	}
	if len(harness.records.batchRequests) != 1 {
		t.Fatalf("BatchUpdate calls = %d, want 1 successful row", len(harness.records.batchRequests))
	}

	reportStream, err := harness.client.GetImportReport(
		harness.ctx,
		&importexportv1.GetImportReportRequest{TaskId: task.GetId()},
	)
	if err != nil {
		t.Fatalf("GetImportReport() error = %v", err)
	}
	var report []byte
	for {
		chunk, recvErr := reportStream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			t.Fatalf("GetImportReport.Recv() error = %v", recvErr)
		}
		report = append(report, chunk.GetChunk()...)
	}
	reportText := string(report)
	for _, want := range []string{"行号", "Test1", "Avocado", "数字格式无效"} {
		if !strings.Contains(reportText, want) {
			t.Fatalf("report = %q, missing %q", reportText, want)
		}
	}

	delivery, err := subscription.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive(import.finished) error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode import.finished payload: %v", err)
	}
	if payload["task_id"] != task.GetId() ||
		payload["base_id"] != harness.baseID ||
		payload["ok_rows"] != float64(1) ||
		payload["fail_rows"] != float64(1) ||
		payload["enterprise_id"] != harness.enterpriseID ||
		payload["report_file_id"] != task.GetReportFileId() ||
		payload["creator_id"] != harness.userID ||
		payload["operator"] != harness.userID {
		t.Fatalf("import.finished payload = %#v", payload)
	}
}