package records_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/internal/records"
)

const (
	recordsTestMySQLDSN = "excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true"
	recordsTestBaseID   = "01J00000000000000000001001"
	recordsTestTableID  = "01J00000000000000000001002"
	recordsTestViewID   = "01J00000000000000000001003"
	recordsTestUserID   = "01J00000000000000000001004"
	recordsTestOtherID  = "01J00000000000000000001005"
)

type queryStructureAccessor struct {
	table  *structurev1.Table
	fields []*structurev1.Field
	view   *structurev1.View
	config *structurev1.ViewConfig
}

func (accessor *queryStructureAccessor) Table(
	context.Context,
	string,
) (*structurev1.Table, error) {
	if accessor.table == nil {
		return nil, fmt.Errorf("table not found")
	}
	return accessor.table, nil
}

func (accessor *queryStructureAccessor) Fields(
	context.Context,
	string,
) ([]*structurev1.Field, error) {
	return accessor.fields, nil
}

func (accessor *queryStructureAccessor) View(
	context.Context,
	string,
) (*structurev1.View, *structurev1.ViewConfig, error) {
	if accessor.view == nil {
		return nil, nil, fmt.Errorf("view not found")
	}
	return accessor.view, accessor.config, nil
}

type queryScopeProvider struct {
	scope string
}

func (provider queryScopeProvider) RowScope(
	context.Context,
	*commonv1.Principal,
	string,
) (string, error) {
	return provider.scope, nil
}

type queryFormulaProvider struct {
	values map[string]*commonv1.Value
}

func (provider queryFormulaProvider) Value(
	_ context.Context,
	recordID, fieldID string,
) (*commonv1.Value, bool, error) {
	value, ok := provider.values[recordID+"\x00"+fieldID]
	return value, ok, nil
}

type queryHarness struct {
	client  recordsv1.RecordsServiceClient
	db      *sql.DB
	now     time.Time
	access  records.SchemaOrViewAccessor
	scopes  records.RowScopeProvider
	formula records.FormulaValueProvider
}

func newQueryHarness(
	t *testing.T,
	access records.SchemaOrViewAccessor,
	scopes records.RowScopeProvider,
	formulas records.FormulaValueProvider,
) *queryHarness {
	t.Helper()
	db, err := base.OpenDB(recordsTestMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetRecordsData(t, db)
	t.Cleanup(func() { resetRecordsData(t, db) })

	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	service := records.NewService(
		base.NewRecordsRepository(db),
		access,
		scopes,
		formulas,
		func() time.Time { return now },
		base.NewCommentRepository(db),
	)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	records.Register(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///records-bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	return &queryHarness{
		client:  recordsv1.NewRecordsServiceClient(connection),
		db:      db,
		now:     now,
		access:  access,
		scopes:  scopes,
		formula: formulas,
	}
}

func TestQueryViewAppliesAndFiltersSortsEmptyLastGroupsAndCursor(t *testing.T) {
	fields := []*structurev1.Field{
		{Id: "fld_title", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_TEXT, IsPrimary: true},
		{Id: "fld_group", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_SELECT},
		{Id: "fld_score", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		{Id: "fld_filter", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		{Id: "fld_hidden", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_TEXT},
	}
	access := &queryStructureAccessor{
		table: &structurev1.Table{
			Id:     recordsTestTableID,
			BaseId: recordsTestBaseID,
		},
		fields: fields,
		view: &structurev1.View{
			Id:      recordsTestViewID,
			TableId: recordsTestTableID,
			Type:    "table",
		},
		config: &structurev1.ViewConfig{
			HiddenFieldList: []string{"fld_hidden"},
			FilterList: []*structurev1.ViewFilter{
				{
					FieldId: "fld_filter",
					Op:      structurev1.ViewFilterOp_VIEW_FILTER_OP_EQ,
					Value:   intCell(1),
				},
				{
					FieldId: "fld_group",
					Op:      structurev1.ViewFilterOp_VIEW_FILTER_OP_IN,
					Value:   listCell(stringCell("A"), stringCell("B")),
				},
			},
			SortList: []*structurev1.ViewSort{{
				FieldId: "fld_score",
				Dir:     structurev1.SortDir_SORT_DIR_ASC,
			}},
			GroupByList: []*structurev1.ViewGroup{{
				FieldId: "fld_group",
			}},
		},
	}
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		queryFormulaProvider{},
	)

	seedRecord(t, harness.db, "rec_a20", recordsTestTableID, recordsTestUserID, 1, map[string]*commonv1.Value{
		"fld_title":  stringCell("A20"),
		"fld_group":  stringCell("A"),
		"fld_score":  intCell(20),
		"fld_filter": intCell(1),
		"fld_hidden": stringCell("secret-a20"),
	})
	seedRecord(t, harness.db, "rec_b10", recordsTestTableID, recordsTestUserID, 2, map[string]*commonv1.Value{
		"fld_title":  stringCell("B10"),
		"fld_group":  stringCell("B"),
		"fld_score":  intCell(10),
		"fld_filter": intCell(1),
	})
	seedRecord(t, harness.db, "rec_a00", recordsTestTableID, recordsTestUserID, 3, map[string]*commonv1.Value{
		"fld_title":  stringCell("A-empty"),
		"fld_group":  stringCell("A"),
		"fld_filter": intCell(1),
	})
	seedRecord(t, harness.db, "rec_b30", recordsTestTableID, recordsTestUserID, 4, map[string]*commonv1.Value{
		"fld_title":  stringCell("B30"),
		"fld_group":  stringCell("B"),
		"fld_score":  intCell(30),
		"fld_filter": intCell(1),
	})
	seedRecord(t, harness.db, "rec_a05", recordsTestTableID, recordsTestUserID, 5, map[string]*commonv1.Value{
		"fld_title":  stringCell("A05"),
		"fld_group":  stringCell("A"),
		"fld_score":  intCell(5),
		"fld_filter": intCell(1),
	})
	seedRecord(t, harness.db, "rec_c01", recordsTestTableID, recordsTestUserID, 6, map[string]*commonv1.Value{
		"fld_title": stringCell("C01"),
		"fld_group": stringCell("C"),
		"fld_score": intCell(1),
	})
	seedRecord(t, harness.db, "rec_b-1", recordsTestTableID, recordsTestUserID, 7, map[string]*commonv1.Value{
		"fld_title": stringCell("B-negative"),
		"fld_group": stringCell("B"),
		"fld_score": intCell(-1),
	})

	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL")
	var got []string
	var cursor string
	for page := 0; page < 3; page++ {
		response, err := harness.client.QueryView(auth, &recordsv1.QueryViewRequest{
			BaseId:  recordsTestBaseID,
			TableId: recordsTestTableID,
			ViewId:  recordsTestViewID,
			Cursor:  cursor,
			Limit:   2,
		})
		if err != nil {
			t.Fatalf("QueryView(page %d) error = %v", page, err)
		}
		for _, record := range response.GetRecordList() {
			got = append(got, record.GetId())
			if _, ok := record.GetCellsMap()["fld_hidden"]; ok {
				t.Fatalf("hidden field leaked in record %s", record.GetId())
			}
		}
		if !response.GetHasMore() {
			if response.GetNextCursor() != "" {
				t.Fatalf("last page next_cursor = %q, want empty", response.GetNextCursor())
			}
			break
		}
		if response.GetNextCursor() == "" {
			t.Fatalf("page %d has_more without next_cursor", page)
		}
		cursor = response.GetNextCursor()
	}

	want := []string{"rec_a05", "rec_a20", "rec_a00", "rec_b10", "rec_b30"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("QueryView order = %v, want %v", got, want)
	}
}

func TestQueryViewDescendingSortKeepsEmptyLastAndGroupsDatesByGranularity(t *testing.T) {
	access := &queryStructureAccessor{
		table: &structurev1.Table{Id: recordsTestTableID, BaseId: recordsTestBaseID},
		fields: []*structurev1.Field{
			{Id: "fld_title", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_TEXT, IsPrimary: true},
			{Id: "fld_event_date", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_DATE},
			{Id: "fld_score", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		},
		view: &structurev1.View{Id: recordsTestViewID, TableId: recordsTestTableID, Type: "table"},
		config: &structurev1.ViewConfig{
			SortList: []*structurev1.ViewSort{{
				FieldId: "fld_score",
				Dir:     structurev1.SortDir_SORT_DIR_DESC,
			}},
			GroupByList: []*structurev1.ViewGroup{{
				FieldId:     "fld_event_date",
				Granularity: structurev1.Granularity_GRANULARITY_MONTH,
			}},
		},
	}
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		queryFormulaProvider{},
	)
	octoberEarly := time.Date(2026, 10, 2, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)).UnixMilli()
	octoberLate := time.Date(2026, 10, 28, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)).UnixMilli()
	november := time.Date(2026, 11, 1, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)).UnixMilli()
	seedRecord(t, harness.db, "rec_oct_early", recordsTestTableID, recordsTestUserID, 1, map[string]*commonv1.Value{
		"fld_title":      stringCell("October empty score"),
		"fld_event_date": dateCell(octoberEarly),
	})
	seedRecord(t, harness.db, "rec_oct_late", recordsTestTableID, recordsTestUserID, 2, map[string]*commonv1.Value{
		"fld_title":      stringCell("October score five"),
		"fld_event_date": dateCell(octoberLate),
		"fld_score":      intCell(5),
	})
	seedRecord(t, harness.db, "rec_november", recordsTestTableID, recordsTestUserID, 3, map[string]*commonv1.Value{
		"fld_title":      stringCell("November"),
		"fld_event_date": dateCell(november),
		"fld_score":      intCell(2),
	})
	seedRecord(t, harness.db, "rec_no_date", recordsTestTableID, recordsTestUserID, 4, map[string]*commonv1.Value{
		"fld_title": stringCell("No date"),
		"fld_score": intCell(1),
	})

	response, err := harness.client.QueryView(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.QueryViewRequest{
			BaseId:  recordsTestBaseID,
			TableId: recordsTestTableID,
			ViewId:  recordsTestViewID,
			Limit:   100,
		},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	want := []string{"rec_oct_late", "rec_oct_early", "rec_november", "rec_no_date"}
	if got := recordIDs(response.GetRecordList()); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("QueryView order = %v, want %v", got, want)
	}
}

func TestQueryViewReturnsUnsolvedCommentCountMap(t *testing.T) {
	fields := []*structurev1.Field{
		{Id: "fld_title", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_TEXT, IsPrimary: true},
	}
	access := &queryStructureAccessor{
		table:  &structurev1.Table{Id: recordsTestTableID, BaseId: recordsTestBaseID},
		fields: fields,
		view:   &structurev1.View{Id: recordsTestViewID, TableId: recordsTestTableID, Type: "table"},
		config: &structurev1.ViewConfig{},
	}
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		queryFormulaProvider{},
	)

	seedRecord(t, harness.db, "rec_a20", recordsTestTableID, recordsTestUserID, 1, map[string]*commonv1.Value{
		"fld_title": stringCell("A20"),
	})
	seedRecord(t, harness.db, "rec_b10", recordsTestTableID, recordsTestUserID, 2, map[string]*commonv1.Value{
		"fld_title": stringCell("B10"),
	})
	seedRecord(t, harness.db, "rec_c30", recordsTestTableID, recordsTestUserID, 3, map[string]*commonv1.Value{
		"fld_title": stringCell("C30"),
	})

	replyParent := fmt.Sprintf("%026d", int64(7))
	seedComment(t, harness.db, "rec_a20", "", "OPEN", 1)
	seedComment(t, harness.db, "rec_a20", "", "OPEN", 2)
	seedComment(t, harness.db, "rec_a20", "", "RESOLVED", 3)
	seedComment(t, harness.db, "rec_a20", replyParent, "OPEN", 4) // 回复不计入
	seedComment(t, harness.db, "rec_b10", "", "RESOLVED", 5)

	response, err := harness.client.QueryView(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.QueryViewRequest{
			BaseId:  recordsTestBaseID,
			TableId: recordsTestTableID,
			ViewId:  recordsTestViewID,
			Limit:   100,
		},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	if got := recordIDs(response.GetRecordList()); fmt.Sprint(got) != fmt.Sprint([]string{"rec_a20", "rec_b10", "rec_c30"}) {
		t.Fatalf("QueryView order = %v", got)
	}
	counts := response.GetUnsolvedCommentCountMap()
	if len(counts) != 1 {
		t.Fatalf("unsolved_comment_count_map = %v, want exactly rec_a20", counts)
	}
	if counts["rec_a20"] != 2 {
		t.Fatalf("unsolved_comment_count_map[rec_a20] = %d, want 2", counts["rec_a20"])
	}
	if _, ok := counts["rec_b10"]; ok {
		t.Fatalf("resolved-only record rec_b10 must not appear in unsolved map")
	}
	if _, ok := counts["rec_c30"]; ok {
		t.Fatalf("comment-less record rec_c30 must not appear in unsolved map")
	}
}

func seedComment(
	t *testing.T,
	db *sql.DB,
	recordID, parentID, status string,
	seed int64,
) {
	t.Helper()
	var parent any
	if parentID != "" {
		parent = parentID
	}
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO excel_comment_comments
			(id, record_id, parent_id, content, author, status, mentions, created_at)
		VALUES (?, ?, ?, ?, ?, ?, '[]', ?)
	`, fmt.Sprintf("%026d", seed), recordID, parent, "content", recordsTestUserID, status, seed)
	if err != nil {
		t.Fatalf("seed comment %s: %v", recordID, err)
	}
}

func seedRecord(
	t *testing.T,
	db *sql.DB,
	id, tableID, createdBy string,
	version int64,
	cells map[string]*commonv1.Value,
) {
	t.Helper()
	payload, err := base.EncodeRecordCells(cells)
	if err != nil {
		t.Fatalf("base.EncodeRecordCells() error = %v", err)
	}
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO excel_records (
			id, table_id, cells_json, created_by, created_at, version, soft_deleted_at
		) VALUES (?, ?, ?, ?, ?, ?, NULL)
	`, id, tableID, payload, createdBy, int64(version), version)
	if err != nil {
		t.Fatalf("seed record %s: %v", id, err)
	}
}

func resetRecordsData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_records_histories",
		"DELETE FROM excel_records",
		"DELETE FROM excel_comment_comments",
		"DELETE FROM excel_bus_outbox",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset records data: %v", err)
		}
	}
}

func recordsAuthenticatedContext(t *testing.T, userID, rowScope string) context.Context {
	t.Helper()
	payload, err := proto.Marshal(&commonv1.Principal{
		UserId:       userID,
		EnterpriseId: "01J00000000000000000001006",
		Internal:     true,
	})
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
		"x-row-scope",
		rowScope,
	)
}

func requireRPCErrorCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want business code %d", want)
	}
	grpcStatus, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	for _, detail := range grpcStatus.Details() {
		if detailWithReason, ok := detail.(interface{ GetReason() string }); ok {
			if detailWithReason.GetReason() == fmt.Sprint(want) {
				return
			}
		}
	}
	if grpcStatus.Code() == codes.NotFound && want == 4002 {
		return
	}
	t.Fatalf("error = %v, details = %v, want business code %d", err, grpcStatus.Details(), want)
}

func stringCell(value string) *commonv1.Value {
	return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: value}}
}

func intCell(value int64) *commonv1.Value {
	return &commonv1.Value{Kind: &commonv1.Value_IntValue{IntValue: value}}
}

func dateCell(value int64) *commonv1.Value {
	return &commonv1.Value{Kind: &commonv1.Value_DateValueMs{DateValueMs: value}}
}

func listCell(values ...*commonv1.Value) *commonv1.Value {
	return &commonv1.Value{
		Kind: &commonv1.Value_ListValue{
			ListValue: &commonv1.ValueList{ValueList: values},
		},
	}
}

func TestQueryViewGroupSortDegradesWhenGroupFieldMissing(t *testing.T) {
	access := &queryStructureAccessor{
		table: &structurev1.Table{Id: recordsTestTableID, BaseId: recordsTestBaseID},
		fields: []*structurev1.Field{
			{Id: "fld_title", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_TEXT, IsPrimary: true},
			{Id: "fld_score", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		},
		view: &structurev1.View{Id: recordsTestViewID, TableId: recordsTestTableID, Type: "table"},
		config: &structurev1.ViewConfig{
			SortList: []*structurev1.ViewSort{{
				FieldId: "fld_score",
				Dir:     structurev1.SortDir_SORT_DIR_ASC,
			}},
			GroupByList: []*structurev1.ViewGroup{{
				FieldId: "fld_ghost_group",
			}},
		},
	}
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		queryFormulaProvider{},
	)
	seedRecord(t, harness.db, "rec_ghost_high", recordsTestTableID, recordsTestUserID, 1, map[string]*commonv1.Value{
		"fld_title":       stringCell("Ghost high"),
		"fld_ghost_group": stringCell("A"),
		"fld_score":       intCell(2),
	})
	seedRecord(t, harness.db, "rec_ghost_low", recordsTestTableID, recordsTestUserID, 2, map[string]*commonv1.Value{
		"fld_title":       stringCell("Ghost low"),
		"fld_ghost_group": stringCell("Z"),
		"fld_score":       intCell(1),
	})

	response, err := harness.client.QueryView(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.QueryViewRequest{
			BaseId:  recordsTestBaseID,
			TableId: recordsTestTableID,
			ViewId:  recordsTestViewID,
			Limit:   100,
		},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	// 分组字段缺失 → 忽略分组，退化为按既有排序键（score 升序）。
	want := []string{"rec_ghost_low", "rec_ghost_high"}
	if got := recordIDs(response.GetRecordList()); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("QueryView order = %v, want %v", got, want)
	}
}

func TestQueryViewEmptyGroupConfigSortsBySortKeysOnly(t *testing.T) {
	access := &queryStructureAccessor{
		table: &structurev1.Table{Id: recordsTestTableID, BaseId: recordsTestBaseID},
		fields: []*structurev1.Field{
			{Id: "fld_title", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_TEXT, IsPrimary: true},
			{Id: "fld_score", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		},
		view: &structurev1.View{Id: recordsTestViewID, TableId: recordsTestTableID, Type: "table"},
		config: &structurev1.ViewConfig{
			SortList: []*structurev1.ViewSort{{
				FieldId: "fld_score",
				Dir:     structurev1.SortDir_SORT_DIR_DESC,
			}},
		},
	}
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		queryFormulaProvider{},
	)
	seedRecord(t, harness.db, "rec_s1", recordsTestTableID, recordsTestUserID, 1, map[string]*commonv1.Value{
		"fld_title": stringCell("S1"),
		"fld_score": intCell(1),
	})
	seedRecord(t, harness.db, "rec_s9", recordsTestTableID, recordsTestUserID, 2, map[string]*commonv1.Value{
		"fld_title": stringCell("S9"),
		"fld_score": intCell(9),
	})

	response, err := harness.client.QueryView(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.QueryViewRequest{
			BaseId:  recordsTestBaseID,
			TableId: recordsTestTableID,
			ViewId:  recordsTestViewID,
			Limit:   100,
		},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	want := []string{"rec_s9", "rec_s1"}
	if got := recordIDs(response.GetRecordList()); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("QueryView order = %v, want %v", got, want)
	}
}

func TestQueryViewEmptyTableWithGroupConfigReturnsEmpty(t *testing.T) {
	access := &queryStructureAccessor{
		table: &structurev1.Table{Id: recordsTestTableID, BaseId: recordsTestBaseID},
		fields: []*structurev1.Field{
			{Id: "fld_title", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_TEXT, IsPrimary: true},
			{Id: "fld_group", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_SELECT},
		},
		view: &structurev1.View{Id: recordsTestViewID, TableId: recordsTestTableID, Type: "table"},
		config: &structurev1.ViewConfig{
			GroupByList: []*structurev1.ViewGroup{{
				FieldId: "fld_group",
			}},
		},
	}
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		queryFormulaProvider{},
	)

	response, err := harness.client.QueryView(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.QueryViewRequest{
			BaseId:  recordsTestBaseID,
			TableId: recordsTestTableID,
			ViewId:  recordsTestViewID,
			Limit:   100,
		},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	if got := response.GetRecordList(); len(got) != 0 {
		t.Fatalf("QueryView record count = %d, want 0", len(got))
	}
	if response.GetHasMore() {
		t.Fatalf("QueryView has_more = true, want false")
	}
	if response.GetNextCursor() != "" {
		t.Fatalf("QueryView next_cursor = %q, want empty", response.GetNextCursor())
	}
}

func TestQueryViewGroupCellMissingSortsEmptyLast(t *testing.T) {
	access := &queryStructureAccessor{
		table: &structurev1.Table{Id: recordsTestTableID, BaseId: recordsTestBaseID},
		fields: []*structurev1.Field{
			{Id: "fld_title", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_TEXT, IsPrimary: true},
			{Id: "fld_group", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_SELECT},
			{Id: "fld_score", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		},
		view: &structurev1.View{Id: recordsTestViewID, TableId: recordsTestTableID, Type: "table"},
		config: &structurev1.ViewConfig{
			SortList: []*structurev1.ViewSort{{
				FieldId: "fld_score",
				Dir:     structurev1.SortDir_SORT_DIR_ASC,
			}},
			GroupByList: []*structurev1.ViewGroup{{
				FieldId: "fld_group",
			}},
		},
	}
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		queryFormulaProvider{},
	)
	seedRecord(t, harness.db, "rec_na20", recordsTestTableID, recordsTestUserID, 1, map[string]*commonv1.Value{
		"fld_title": stringCell("A20"),
		"fld_group": stringCell("A"),
		"fld_score": intCell(20),
	})
	seedRecord(t, harness.db, "rec_na5", recordsTestTableID, recordsTestUserID, 2, map[string]*commonv1.Value{
		"fld_title": stringCell("A5"),
		"fld_group": stringCell("A"),
		"fld_score": intCell(5),
	})
	seedRecord(t, harness.db, "rec_nnil1", recordsTestTableID, recordsTestUserID, 3, map[string]*commonv1.Value{
		"fld_title": stringCell("nil group low"),
		"fld_score": intCell(1),
	})
	seedRecord(t, harness.db, "rec_nnil50", recordsTestTableID, recordsTestUserID, 4, map[string]*commonv1.Value{
		"fld_title": stringCell("nil group high"),
		"fld_score": intCell(50),
	})

	response, err := harness.client.QueryView(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.QueryViewRequest{
			BaseId:  recordsTestBaseID,
			TableId: recordsTestTableID,
			ViewId:  recordsTestViewID,
			Limit:   100,
		},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	want := []string{"rec_na5", "rec_na20", "rec_nnil1", "rec_nnil50"}
	if got := recordIDs(response.GetRecordList()); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("QueryView order = %v, want %v", got, want)
	}
}

// 排序字段从表结构中删除后，结构里不再有该字段：排序与游标均不得因解引用 nil
// 字段而 panic，且该排序键应被忽略、落到后续排序键（此处为 fld_score 升序，
// 同分再按记录 ID 兜底）。分页游标必须照常推进并最终为空。
func TestQueryViewSortSkipsDeletedSortFieldAndFallsThrough(t *testing.T) {
	access := &queryStructureAccessor{
		table: &structurev1.Table{Id: recordsTestTableID, BaseId: recordsTestBaseID},
		fields: []*structurev1.Field{
			{Id: "fld_title", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_TEXT, IsPrimary: true},
			{Id: "fld_score", TableId: recordsTestTableID, Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		},
		view: &structurev1.View{Id: recordsTestViewID, TableId: recordsTestTableID, Type: "table"},
		config: &structurev1.ViewConfig{
			SortList: []*structurev1.ViewSort{
				{FieldId: "fld_ghost_sort", Dir: structurev1.SortDir_SORT_DIR_ASC},
				{FieldId: "fld_score", Dir: structurev1.SortDir_SORT_DIR_ASC},
			},
		},
	}
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		queryFormulaProvider{},
	)
	// fld_ghost_sort 已删除（结构不含该字段），但记录仍可能残留该列单元格；
	// 该排序键必须被跳过，最终按 fld_score 升序、同分按记录 ID。
	seedRecord(t, harness.db, "rec_s5", recordsTestTableID, recordsTestUserID, 1, map[string]*commonv1.Value{
		"fld_title":      stringCell("score five"),
		"fld_ghost_sort": stringCell("Z"),
		"fld_score":      intCell(5),
	})
	seedRecord(t, harness.db, "rec_s1", recordsTestTableID, recordsTestUserID, 2, map[string]*commonv1.Value{
		"fld_title":      stringCell("score one"),
		"fld_ghost_sort": stringCell("A"),
		"fld_score":      intCell(1),
	})
	seedRecord(t, harness.db, "rec_s3b", recordsTestTableID, recordsTestUserID, 3, map[string]*commonv1.Value{
		"fld_title":      stringCell("score three b"),
		"fld_ghost_sort": stringCell("M"),
		"fld_score":      intCell(3),
	})
	seedRecord(t, harness.db, "rec_s3a", recordsTestTableID, recordsTestUserID, 4, map[string]*commonv1.Value{
		"fld_title": stringCell("score three a"),
		"fld_score": intCell(3),
	})

	var got []string
	var cursor string
	for page := 0; page < 3; page++ {
		response, err := harness.client.QueryView(
			recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
			&recordsv1.QueryViewRequest{
				BaseId:  recordsTestBaseID,
				TableId: recordsTestTableID,
				ViewId:  recordsTestViewID,
				Cursor:  cursor,
				Limit:   2,
			},
		)
		if err != nil {
			t.Fatalf("QueryView(page %d) error = %v", page, err)
		}
		for _, record := range response.GetRecordList() {
			got = append(got, record.GetId())
		}
		if !response.GetHasMore() {
			if response.GetNextCursor() != "" {
				t.Fatalf("last page next_cursor = %q, want empty", response.GetNextCursor())
			}
			break
		}
		if response.GetNextCursor() == "" {
			t.Fatalf("page %d has_more without next_cursor", page)
		}
		cursor = response.GetNextCursor()
	}

	want := []string{"rec_s1", "rec_s3a", "rec_s3b", "rec_s5"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("QueryView order = %v, want %v", got, want)
	}
}
