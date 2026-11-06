package workspace_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"

	"excel-libs/idgen"
	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	workspacev1 "excel/api/workspace/v1"

	"excel_core_service/base"
)

// mustCalcValueJSON 把单元格值编码为物化表 value_json 载荷。
func mustCalcValueJSON(t *testing.T, value *commonv1.Value) []byte {
	t.Helper()
	payload, err := base.EncodeRecordCell(value)
	if err != nil {
		t.Fatalf("encode calc value: %v", err)
	}
	return payload
}

// queryUnsolvedCommentCounts 经 records RPC 读回页内未解决评论数。
func queryUnsolvedCommentCounts(
	t *testing.T,
	harness *workspaceHarness,
	userID, enterpriseID, baseID, tableID string,
) map[string]int32 {
	t.Helper()
	response, err := harness.recordsClient.QueryView(
		queryContext(t, userID, enterpriseID),
		&recordsv1.QueryViewRequest{BaseId: baseID, TableId: tableID},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	return response.GetUnsolvedCommentCountMap()
}

func textCellValue(text string) *commonv1.Value {
	return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: text}}
}

func numberCellValue(number int64) *commonv1.Value {
	return &commonv1.Value{Kind: &commonv1.Value_IntValue{IntValue: number}}
}

// fieldIDsByName 经结构 RPC 读取字段清单并返回 name→id 投影。
func fieldIDsByName(
	t *testing.T,
	harness *workspaceHarness,
	userID, enterpriseID, tableID string,
) map[string]string {
	t.Helper()
	response, err := harness.structureClient.ListFields(
		authenticatedContext(t, userID, enterpriseID),
		&structurev1.ListFieldsRequest{TableId: tableID},
	)
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	ids := make(map[string]string, len(response.GetFieldList()))
	for _, field := range response.GetFieldList() {
		ids[field.GetName()] = field.GetId()
	}
	return ids
}

// queryRecords 经 records RPC 读回表内记录（外部行为缝）。
func queryRecords(
	t *testing.T,
	harness *workspaceHarness,
	userID, enterpriseID, baseID, tableID string,
) []*recordsv1.Record {
	t.Helper()
	response, err := harness.recordsClient.QueryView(
		queryContext(t, userID, enterpriseID),
		&recordsv1.QueryViewRequest{BaseId: baseID, TableId: tableID},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	return response.GetRecordList()
}

func TestDuplicateBaseCopiesStructureAndRecordsOnly(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	source, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "产品计划",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}

	response, err := harness.client.DuplicateBase(ctx, &workspacev1.DuplicateBaseRequest{
		BaseId: source.GetBase().GetId(),
	})
	if err != nil {
		t.Fatalf("DuplicateBase() error = %v", err)
	}
	duplicate := response.GetBase()
	if duplicate.GetId() == source.GetBase().GetId() {
		t.Fatal("DuplicateBase() reused the source Base id")
	}
	if !strings.Contains(duplicate.GetName(), "副本") {
		t.Fatalf("DuplicateBase() name = %q, want 副本 suffix", duplicate.GetName())
	}

	sourceTables, err := harness.structure.TablesByBase(
		authenticatedIncomingContext(t, testOwner, testEnterprise),
		testEnterprise,
		source.GetBase().GetId(),
	)
	if err != nil {
		t.Fatalf("source TablesByBase() error = %v", err)
	}
	duplicateTables, err := harness.structure.TablesByBase(
		authenticatedIncomingContext(t, testOwner, testEnterprise),
		testEnterprise,
		duplicate.GetId(),
	)
	if err != nil {
		t.Fatalf("duplicate TablesByBase() error = %v", err)
	}
	if len(duplicateTables) != len(sourceTables) || len(duplicateTables) == 0 {
		t.Fatalf("duplicate tables = %d, source tables = %d", len(duplicateTables), len(sourceTables))
	}
	for index := range sourceTables {
		if duplicateTables[index].ID == sourceTables[index].ID {
			t.Fatalf("table[%d] reused source id", index)
		}
		if duplicateTables[index].Name != sourceTables[index].Name {
			t.Fatalf("table[%d] name = %q, want %q",
				index, duplicateTables[index].Name, sourceTables[index].Name)
		}
		fields, err := harness.structure.FieldsByTable(
			authenticatedIncomingContext(t, testOwner, testEnterprise),
			duplicateTables[index].ID,
		)
		if err != nil {
			t.Fatalf("duplicate FieldsByTable() error = %v", err)
		}
		if len(fields) != 3 || fields[0].Name != "标题" || !fields[0].IsPrimary {
			t.Fatalf("duplicate fields = %+v", fields)
		}
		views, err := harness.structure.ViewsByTable(
			authenticatedIncomingContext(t, testOwner, testEnterprise),
			duplicateTables[index].ID,
		)
		if err != nil {
			t.Fatalf("duplicate ViewsByTable() error = %v", err)
		}
		if len(views) != 1 || views[0].Name != "表格" {
			t.Fatalf("duplicate views = %+v", views)
		}
	}
	if len(harness.records.calls) != 1 {
		t.Fatalf("record duplication calls = %d, want 1 atomic Base-level call", len(harness.records.calls))
	}
	if len(harness.records.calls[0]) != len(sourceTables) {
		t.Fatalf("record duplication table map = %v, want one pair per table", harness.records.calls[0])
	}
}

func TestDuplicateBaseCopiesRecordsWithNewRecordSemantics(t *testing.T) {
	harness := newWorkspaceFullHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	created, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "盘点清单t03",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	sourceBaseID := created.GetBase().GetId()
	tablesResponse, err := harness.structureClient.ListTables(
		ctx,
		&structurev1.ListTablesRequest{BaseId: sourceBaseID},
	)
	if err != nil {
		t.Fatalf("source ListTables() error = %v", err)
	}
	sourceTable := tablesResponse.GetTableList()[0]
	if _, err := harness.structureClient.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId: sourceTable.GetId(),
		Name:    "数量",
		Type:    structurev1.FieldType_FIELD_TYPE_NUMBER,
	}); err != nil {
		t.Fatalf("CreateField(数量) error = %v", err)
	}
	sourceFields := fieldIDsByName(t, harness, testOwner, testEnterprise, sourceTable.GetId())
	past := harness.now.Add(-time.Hour)
	seedRecord(t, harness, sourceTable.GetId(), testMember, past, 7, map[string]*commonv1.Value{
		sourceFields["标题"]: textCellValue("货物A"),
		sourceFields["数量"]: numberCellValue(3),
	})
	seedRecord(t, harness, sourceTable.GetId(), testManager, past, 4, map[string]*commonv1.Value{
		sourceFields["标题"]: textCellValue("货物B"),
		sourceFields["数量"]: numberCellValue(9),
	})

	response, err := harness.client.DuplicateBase(ctx, &workspacev1.DuplicateBaseRequest{
		BaseId: sourceBaseID,
	})
	if err != nil {
		t.Fatalf("DuplicateBase() error = %v", err)
	}
	copyBaseID := response.GetBase().GetId()
	copyTablesResponse, err := harness.structureClient.ListTables(
		ctx,
		&structurev1.ListTablesRequest{BaseId: copyBaseID},
	)
	if err != nil {
		t.Fatalf("copy ListTables() error = %v", err)
	}
	var copyTable *structurev1.Table
	for _, table := range copyTablesResponse.GetTableList() {
		if table.GetName() == sourceTable.GetName() {
			copyTable = table
			break
		}
	}
	if copyTable == nil {
		t.Fatalf("copy tables = %+v, want table %q", copyTablesResponse.GetTableList(), sourceTable.GetName())
	}
	copyFields := fieldIDsByName(t, harness, testOwner, testEnterprise, copyTable.GetId())
	if _, ok := copyFields["数量"]; !ok {
		t.Fatalf("copy fields = %v, want 数量 copied", copyFields)
	}

	copiedRecords := queryRecords(t, harness, testOwner, testEnterprise, copyBaseID, copyTable.GetId())
	if len(copiedRecords) != 2 {
		t.Fatalf("copied records = %d, want 2", len(copiedRecords))
	}
	cellsByText := make(map[string]*recordsv1.Record, len(copiedRecords))
	for _, record := range copiedRecords {
		title := record.GetCellsMap()[copyFields["标题"]].GetStringValue()
		cellsByText[title] = record
	}
	for title, quantity := range map[string]int64{"货物A": 3, "货物B": 9} {
		record, ok := cellsByText[title]
		if !ok {
			t.Fatalf("copied records miss title %q: %+v", title, copiedRecords)
		}
		if got := record.GetCellsMap()[copyFields["数量"]]; !proto.Equal(got, numberCellValue(quantity)) {
			t.Fatalf("record %q 数量 = %v, want %d", title, got, quantity)
		}
		if record.GetVersion() != 1 {
			t.Fatalf("record %q version = %d, want 1 (new-record semantics)", title, record.GetVersion())
		}
		if record.GetCreatedBy() != testOwner {
			t.Fatalf("record %q created_by = %q, want duplicating operator %q", title, record.GetCreatedBy(), testOwner)
		}
		if record.GetCreatedAt() != harness.now.UnixMilli() {
			t.Fatalf("record %q created_at = %d, want %d", title, record.GetCreatedAt(), harness.now.UnixMilli())
		}
	}
}

func TestDuplicateBaseRemapsRelationCellsToCopiedRecords(t *testing.T) {
	harness := newWorkspaceFullHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	created, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "项目跟踪t03",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	sourceBaseID := created.GetBase().GetId()
	sourceTables, err := harness.structureClient.ListTables(
		ctx,
		&structurev1.ListTablesRequest{BaseId: sourceBaseID},
	)
	if err != nil {
		t.Fatalf("source ListTables() error = %v", err)
	}
	gridTable := sourceTables.GetTableList()[0]
	taskTableResponse, err := harness.structureClient.CreateTable(
		ctx,
		&structurev1.CreateTableRequest{BaseId: sourceBaseID, Name: "任务"},
	)
	if err != nil {
		t.Fatalf("CreateTable(任务) error = %v", err)
	}
	taskTable := taskTableResponse.GetTable()
	relationConfig, err := structpb.NewStruct(map[string]any{
		"target_table_id": taskTable.GetId(),
		"allow_multiple":  false,
	})
	if err != nil {
		t.Fatalf("structpb.NewStruct() error = %v", err)
	}
	if _, err := harness.structureClient.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId:   gridTable.GetId(),
		Name:      "关联任务",
		Type:      structurev1.FieldType_FIELD_TYPE_RELATION,
		ConfigMap: relationConfig,
	}); err != nil {
		t.Fatalf("CreateField(关联任务) error = %v", err)
	}
	taskFields := fieldIDsByName(t, harness, testOwner, testEnterprise, taskTable.GetId())
	taskRecord := seedRecord(t, harness, taskTable.GetId(), testOwner, harness.now.Add(-time.Hour), 1, map[string]*commonv1.Value{
		taskFields["标题"]: textCellValue("任务X"),
	})
	gridFields := fieldIDsByName(t, harness, testOwner, testEnterprise, gridTable.GetId())
	seedRecord(t, harness, gridTable.GetId(), testOwner, harness.now.Add(-time.Hour), 1, map[string]*commonv1.Value{
		gridFields["标题"]:    textCellValue("卡片1"),
		gridFields["关联任务"]: textCellValue(taskRecord.ID),
	})

	response, err := harness.client.DuplicateBase(ctx, &workspacev1.DuplicateBaseRequest{
		BaseId: sourceBaseID,
	})
	if err != nil {
		t.Fatalf("DuplicateBase() error = %v", err)
	}
	copyBaseID := response.GetBase().GetId()
	copyTables, err := harness.structureClient.ListTables(
		ctx,
		&structurev1.ListTablesRequest{BaseId: copyBaseID},
	)
	if err != nil {
		t.Fatalf("copy ListTables() error = %v", err)
	}
	byName := make(map[string]string, len(copyTables.GetTableList()))
	for _, table := range copyTables.GetTableList() {
		byName[table.GetName()] = table.GetId()
	}
	copiedTaskRecords := queryRecords(t, harness, testOwner, testEnterprise, copyBaseID, byName["任务"])
	if len(copiedTaskRecords) != 1 {
		t.Fatalf("copied task records = %d, want 1", len(copiedTaskRecords))
	}
	copiedTaskID := copiedTaskRecords[0].GetId()
	if copiedTaskID == taskRecord.ID {
		t.Fatal("copied task record reused the source record id")
	}
	copiedGridRecords := queryRecords(t, harness, testOwner, testEnterprise, copyBaseID, byName["表格"])
	if len(copiedGridRecords) != 1 {
		t.Fatalf("copied grid records = %d, want 1", len(copiedGridRecords))
	}
	copiedGridFields := fieldIDsByName(t, harness, testOwner, testEnterprise, byName["表格"])
	if got := copiedGridRecords[0].GetCellsMap()[copiedGridFields["关联任务"]].GetStringValue(); got != copiedTaskID {
		t.Fatalf("copied relation cell = %q, want copied record %q", got, copiedTaskID)
	}
}

func TestDuplicateBaseCarriesFormulaMaterializedValues(t *testing.T) {
	harness := newWorkspaceFullHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	created, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "公式清单t03",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	sourceBaseID := created.GetBase().GetId()
	sourceTables, err := harness.structureClient.ListTables(
		ctx,
		&structurev1.ListTablesRequest{BaseId: sourceBaseID},
	)
	if err != nil {
		t.Fatalf("source ListTables() error = %v", err)
	}
	sourceTable := sourceTables.GetTableList()[0]
	sourceFields := fieldIDsByName(t, harness, testOwner, testEnterprise, sourceTable.GetId())
	seedRecord(t, harness, sourceTable.GetId(), testOwner, harness.now.Add(-time.Hour), 2, map[string]*commonv1.Value{
		sourceFields["标题"]: textCellValue("公式行"),
	})
	expression, err := base.EncodeStructureConfig(map[string]any{"expression": "1"})
	if err != nil {
		t.Fatalf("encode formula config: %v", err)
	}
	formulaFieldID, err := idgen.New()
	if err != nil {
		t.Fatalf("idgen.New() error = %v", err)
	}
	formulaField := base.StructureField{
		ID:        formulaFieldID,
		TableID:   sourceTable.GetId(),
		Name:      "合计",
		Type:      "FIELD_TYPE_FORMULA",
		Config:    expression,
		Position:  3,
		CreatedAt: harness.now.Add(-time.Hour).UnixMilli(),
	}
	if err := harness.structure.CreateField(ctx, formulaField, nil); err != nil {
		t.Fatalf("seed formula field: %v", err)
	}
	seededRecords := queryRecords(t, harness, testOwner, testEnterprise, sourceBaseID, sourceTable.GetId())
	if len(seededRecords) != 1 {
		t.Fatalf("seeded records = %d, want 1", len(seededRecords))
	}
	sourceFormulaValue := numberCellValue(42)
	calcTx, err := harness.calcRepository.BeginTx(ctx)
	if err != nil {
		t.Fatalf("calc BeginTx() error = %v", err)
	}
	if err := harness.calcRepository.SaveMaterialized(
		ctx,
		calcTx,
		base.CalcMaterializedRecord{
			RecordID:  seededRecords[0].GetId(),
			FieldID:   formulaFieldID,
			ValueJSON: mustCalcValueJSON(t, sourceFormulaValue),
			Status:    "IDLE",
		},
	); err != nil {
		_ = calcTx.Rollback()
		t.Fatalf("seed formula materialization: %v", err)
	}
	if err := calcTx.Commit(); err != nil {
		t.Fatalf("commit seeded formula materialization: %v", err)
	}

	response, err := harness.client.DuplicateBase(ctx, &workspacev1.DuplicateBaseRequest{
		BaseId: sourceBaseID,
	})
	if err != nil {
		t.Fatalf("DuplicateBase() error = %v", err)
	}
	copyBaseID := response.GetBase().GetId()
	copyTables, err := harness.structureClient.ListTables(
		ctx,
		&structurev1.ListTablesRequest{BaseId: copyBaseID},
	)
	if err != nil {
		t.Fatalf("copy ListTables() error = %v", err)
	}
	copiedRecords := queryRecords(t, harness, testOwner, testEnterprise, copyBaseID, copyTables.GetTableList()[0].GetId())
	if len(copiedRecords) != 1 {
		t.Fatalf("copied records = %d, want 1", len(copiedRecords))
	}
	copyFields := fieldIDsByName(t, harness, testOwner, testEnterprise, copyTables.GetTableList()[0].GetId())
	if got := copiedRecords[0].GetCellsMap()[copyFields["合计"]]; !proto.Equal(got, sourceFormulaValue) {
		t.Fatalf("copied formula cell = %v, want %v", got, sourceFormulaValue)
	}
}

func TestDuplicateBaseLeavesCommentsAndHistoryBehind(t *testing.T) {
	harness := newWorkspaceFullHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	created, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "协作清单t03",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	sourceBaseID := created.GetBase().GetId()
	sourceTables, err := harness.structureClient.ListTables(
		ctx,
		&structurev1.ListTablesRequest{BaseId: sourceBaseID},
	)
	if err != nil {
		t.Fatalf("source ListTables() error = %v", err)
	}
	sourceTable := sourceTables.GetTableList()[0]
	sourceFields := fieldIDsByName(t, harness, testOwner, testEnterprise, sourceTable.GetId())
	sourceRecord := seedRecord(t, harness, sourceTable.GetId(), testOwner, harness.now.Add(-time.Hour), 2, map[string]*commonv1.Value{
		sourceFields["标题"]: textCellValue("有评论行"),
	})
	commentID, err := idgen.New()
	if err != nil {
		t.Fatalf("idgen.New() error = %v", err)
	}
	commentTx, err := harness.commentRepository.BeginTx(ctx)
	if err != nil {
		t.Fatalf("comment BeginTx() error = %v", err)
	}
	if err := harness.commentRepository.CreateTx(ctx, commentTx, base.Comment{
		ID:        commentID,
		BaseID:    sourceBaseID,
		RecordID:  sourceRecord.ID,
		Content:   "源记录评论",
		Author:    testMember,
		Status:    "OPEN",
		CreatedAt: harness.now.Add(-time.Hour).UnixMilli(),
	}); err != nil {
		_ = commentTx.Rollback()
		t.Fatalf("seed comment: %v", err)
	}
	if err := commentTx.Commit(); err != nil {
		t.Fatalf("commit seeded comment: %v", err)
	}
	historyID, err := idgen.New()
	if err != nil {
		t.Fatalf("idgen.New() error = %v", err)
	}
	historyTx, err := harness.recordsStore.BeginTx(ctx)
	if err != nil {
		t.Fatalf("records BeginTx() error = %v", err)
	}
	if err := harness.recordsStore.AppendHistoryTx(ctx, historyTx, base.RecordHistory{
		ID:        historyID,
		RecordID:  sourceRecord.ID,
		FieldID:   sourceFields["标题"],
		After:     textCellValue("有评论行"),
		Operator:  testOwner,
		CreatedAt: harness.now.Add(-time.Hour).UnixMilli(),
	}); err != nil {
		_ = historyTx.Rollback()
		t.Fatalf("seed history: %v", err)
	}
	if err := historyTx.Commit(); err != nil {
		t.Fatalf("commit seeded history: %v", err)
	}

	response, err := harness.client.DuplicateBase(ctx, &workspacev1.DuplicateBaseRequest{
		BaseId: sourceBaseID,
	})
	if err != nil {
		t.Fatalf("DuplicateBase() error = %v", err)
	}
	copyBaseID := response.GetBase().GetId()
	copyTables, err := harness.structureClient.ListTables(
		ctx,
		&structurev1.ListTablesRequest{BaseId: copyBaseID},
	)
	if err != nil {
		t.Fatalf("copy ListTables() error = %v", err)
	}
	copyTable := copyTables.GetTableList()[0]
	copyRecords := queryRecords(t, harness, testOwner, testEnterprise, copyBaseID, copyTable.GetId())
	if len(copyRecords) != 1 {
		t.Fatalf("copied records = %d, want 1", len(copyRecords))
	}
	if commentCounts := queryUnsolvedCommentCounts(t, harness, testOwner, testEnterprise, copyBaseID, copyTable.GetId()); len(commentCounts) != 0 {
		t.Fatalf("copied base comment counts = %v, want empty", commentCounts)
	}
	sourceCounts := queryUnsolvedCommentCounts(t, harness, testOwner, testEnterprise, sourceBaseID, sourceTable.GetId())
	if sourceCounts[sourceRecord.ID] != 1 {
		t.Fatalf("source comment counts = %v, want 1 for %s", sourceCounts, sourceRecord.ID)
	}
	copyHistory, err := harness.recordsClient.GetHistory(
		queryContext(t, testOwner, testEnterprise),
		&recordsv1.GetHistoryRequest{RecordId: copyRecords[0].GetId()},
	)
	if err != nil {
		t.Fatalf("copy GetHistory() error = %v", err)
	}
	if len(copyHistory.GetHistoryList()) != 0 {
		t.Fatalf("copied record history = %d nodes, want 0", len(copyHistory.GetHistoryList()))
	}
}

func TestDuplicateBaseRollsBackWhenRecordCopyFails(t *testing.T) {
	harness := newWorkspaceFullHarnessWithDuplicator(
		t,
		&fakeRecordsDuplicator{err: errors.New("records copy unavailable")},
	)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	created, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "回滚清单t03",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	if _, err := harness.client.DuplicateBase(ctx, &workspacev1.DuplicateBaseRequest{
		BaseId: created.GetBase().GetId(),
	}); err == nil {
		t.Fatal("DuplicateBase() error = nil, want records copy failure")
	}
	home, err := harness.client.GetHome(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetHome() error = %v", err)
	}
	for _, item := range home.GetHome().GetCreatedList() {
		if strings.Contains(item.GetName(), "副本") {
			t.Fatalf("home list keeps half-copied base %q", item.GetName())
		}
	}
}
