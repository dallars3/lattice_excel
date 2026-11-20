package audit_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"excel-libs/eventbus"
	auditv1 "excel/api/audit/v1"
	iamv1 "excel/api/iam/v1"
	"excel-libs/testmysql"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/internal/audit"
	"excel_core_service/internal/iam"
)

type auditEventHarness struct {
	db          *sql.DB
	auditClient auditv1.AuditServiceClient
	iamClient   iamv1.IamServiceClient
	broker      *eventbus.MemoryBus
	forwarder   *bus.Forwarder
	outbox      *bus.Outbox
}

func newAuditEventHarness(t *testing.T) *auditEventHarness {
	t.Helper()

	config, err := base.LoadConfig(filepath.Join("..", "..", "configs", "core_service_config.yaml"))
	if err != nil {
		t.Fatalf("base.LoadConfig() error = %v", err)
	}
	db := testmysql.OpenTestDB(t, testmysql.TestDSN(config.MySQLDSN))
	if err := base.MigrateUpWithRetry(db); err != nil {
		_ = db.Close()
		t.Fatalf("base.MigrateUpWithRetry() error = %v", err)
	}
	cleanupAuditEnterprise(db)

	store := base.NewBusStore(db)
	outbox := bus.NewOutbox(store)
	broker := eventbus.NewMemoryBus()
	auditService := audit.NewService(
		base.NewAuditRepository(db),
		fakeRoleProvider{
			"01J00000000000000000008001/01J00000000000000000008002": iamv1.MemberRole_MEMBER_ROLE_OWNER,
		},
	)
	consumer := bus.NewConsumer(store, broker, bus.ConsumerConfig{
		Retry: eventbus.RetryPolicy{
			Initial:     10 * time.Millisecond,
			Max:         20 * time.Millisecond,
			MaxAttempts: 3,
		},
	})
	if err := audit.Consumers(consumer, auditService); err != nil {
		_ = db.Close()
		t.Fatalf("audit.Consumers() error = %v", err)
	}
	consumerContext, cancelConsumer := context.WithCancel(context.Background())
	if err := consumer.Start(consumerContext); err != nil {
		cancelConsumer()
		_ = db.Close()
		t.Fatalf("consumer.Start() error = %v", err)
	}

	iamService := iam.NewIamService(
		base.NewIAMRepository(db),
		nil,
		nil,
		outbox,
	)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	audit.Register(server, auditService)
	iam.Register(server, iamService)
	go func() {
		_ = server.Serve(listener)
	}()

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		server.Stop()
		_ = listener.Close()
		cancelConsumer()
		_ = db.Close()
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
		stopContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = consumer.Stop(stopContext)
		cancelConsumer()
		cleanupAuditEnterprise(db)
		_ = db.Close()
	})
	return &auditEventHarness{
		db:          db,
		auditClient: auditv1.NewAuditServiceClient(conn),
		iamClient:   iamv1.NewIamServiceClient(conn),
		broker:      broker,
		forwarder:   bus.NewForwarder(store, broker, bus.ForwarderConfig{BatchSize: 100}),
		outbox:      outbox,
	}
}

func seedAuditEnterprise(t *testing.T, harness *auditEventHarness, enterpriseID, ownerID, memberID string) {
	t.Helper()
	now := time.Now().UnixMilli()
	statements := []struct {
		query string
		args  []any
	}{
		{
			query: "INSERT INTO excel_iam_enterprises (id, name, owner_id, member_count, created_at) VALUES (?, ?, ?, ?, ?)",
			args:  []any{enterpriseID, "审计测试企业", ownerID, 2, now},
		},
		{
			query: "INSERT INTO excel_iam_users (id, username, display_name, password_hash, enterprise_id, `internal`, created_at) VALUES (?, ?, ?, ?, ?, 1, ?)",
			args:  []any{ownerID, "AuditOwner", "AuditOwner", "fixture", enterpriseID, now},
		},
		{
			query: "INSERT INTO excel_iam_users (id, username, display_name, password_hash, enterprise_id, `internal`, created_at) VALUES (?, ?, ?, ?, ?, 1, ?)",
			args:  []any{memberID, "AuditMember", "AuditMember", "fixture", enterpriseID, now},
		},
		{
			query: "INSERT INTO excel_iam_members (enterprise_id, user_id, role, status, joined_at) VALUES (?, ?, 'owner', 'active', ?)",
			args:  []any{enterpriseID, ownerID, now},
		},
		{
			query: "INSERT INTO excel_iam_members (enterprise_id, user_id, role, status, joined_at) VALUES (?, ?, 'member', 'active', ?)",
			args:  []any{enterpriseID, memberID, now},
		},
	}
	for _, statement := range statements {
		if _, err := harness.db.ExecContext(context.Background(), statement.query, statement.args...); err != nil {
			t.Fatalf("seed audit enterprise: %v; query = %s", err, statement.query)
		}
	}
}

func enqueueAuditEvents(
	t *testing.T,
	harness *auditEventHarness,
	events []struct {
		eventID string
		topic   string
		payload map[string]any
	},
) {
	t.Helper()
	tx, err := harness.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for index, fixture := range events {
		payload, err := json.Marshal(fixture.payload)
		if err != nil {
			t.Fatalf("marshal %s payload: %v", fixture.topic, err)
		}
		event := eventbus.Event{
			Header: eventbus.EventHeader{
				EventID:    fixture.eventID,
				Type:       fixture.topic,
				Version:    1,
				OccurredAt: time.Now().Add(time.Duration(index) * time.Millisecond).UnixMilli(),
				Producer:   "audit-test",
			},
			Payload: payload,
		}
		if err := harness.outbox.Enqueue(context.Background(), tx, event); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", fixture.topic, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit synthetic audit events: %v", err)
	}
}

func cleanupAuditEnterprise(db *sql.DB) {
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, "DELETE FROM excel_audit_events WHERE enterprise_id LIKE '01J00000000000000000008%'")
	_, _ = db.ExecContext(ctx, "DELETE FROM excel_bus_outbox WHERE JSON_UNQUOTE(JSON_EXTRACT(payload, '$.enterprise_id')) LIKE '01J00000000000000000008%'")
	_, _ = db.ExecContext(ctx, "DELETE FROM excel_iam_members WHERE enterprise_id LIKE '01J00000000000000000008%'")
	_, _ = db.ExecContext(ctx, "DELETE FROM excel_iam_users WHERE enterprise_id LIKE '01J00000000000000000008%'")
	_, _ = db.ExecContext(ctx, "DELETE FROM excel_iam_enterprises WHERE id LIKE '01J00000000000000000008%'")
}

func TestEightEventTopicsAreMappedAndPersistedThroughOutbox(t *testing.T) {
	harness := newAuditEventHarness(t)
	const (
		enterpriseID = "01J00000000000000000008001"
		ownerID      = "01J00000000000000000008002"
		memberID     = "01J00000000000000000008003"
	)
	cleanupAuditEnterprise(harness.db)
	seedAuditEnterprise(t, harness, enterpriseID, ownerID, memberID)
	ownerContext := auditPrincipalContext(t, enterpriseID, ownerID)

	if _, err := harness.iamClient.DisableMember(ownerContext, &iamv1.DisableMemberRequest{
		UserId: memberID,
	}); err != nil {
		t.Fatalf("DisableMember() error = %v", err)
	}
	if _, err := harness.iamClient.EnableMember(ownerContext, &iamv1.EnableMemberRequest{
		UserId: memberID,
	}); err != nil {
		t.Fatalf("EnableMember() error = %v", err)
	}
	if _, err := harness.iamClient.TransferOwner(ownerContext, &iamv1.TransferOwnerRequest{
		SuccessorId: memberID,
		Confirm:     true,
	}); err != nil {
		t.Fatalf("TransferOwner() error = %v", err)
	}

	baseID := "01J00000000000000000008004"
	tableID := "01J00000000000000000008005"
	synthetic := []struct {
		eventID string
		topic   string
		payload map[string]any
	}{
		{
			eventID: uniqueEventID("evt_record_deleted"),
			topic:   "record.deleted",
			payload: map[string]any{
				"enterprise_id": enterpriseID,
				"record_id_list": []string{
					"01J00000000000000000008006",
					"01J00000000000000000008007",
				},
				"operator": ownerID,
				"base_id":  baseID,
				"table_id": tableID,
			},
		},
		{
			eventID: uniqueEventID("evt_link_closed"),
			topic:   "link.closed",
			payload: map[string]any{
				"enterprise_id": enterpriseID,
				"base_id":       baseID,
				"link_id":       baseID,
				"operator":      ownerID,
				"reason":        "force_close",
			},
		},
		{
			eventID: uniqueEventID("evt_export_audited"),
			topic:   "export.audited",
			payload: map[string]any{
				"enterprise_id": enterpriseID,
				"operator":      ownerID,
				"base_id":       baseID,
				"table_id":      tableID,
				"format":        "csv",
			},
		},
		{
			eventID: uniqueEventID("evt_import_finished"),
			topic:   "import.finished",
			payload: map[string]any{
				"enterprise_id": enterpriseID,
				"task_id":       "01J00000000000000000008008",
				"base_id":       baseID,
				"ok_rows":       10,
				"fail_rows":     2,
			},
		},
		{
			eventID: uniqueEventID("evt_audit_append"),
			topic:   "audit.append",
			payload: map[string]any{
				"enterprise_id": enterpriseID,
				"action":        "AUDIT_ACTION_BASE_DELETED",
				"operator":      ownerID,
				"target":        "deleted_base",
				"base_id":       baseID,
				"detail_map": map[string]any{
					"enterprise_id": "01J00000000000000000009999",
					"reason":        "admin",
				},
			},
		},
	}
	enqueueAuditEvents(t, harness, synthetic)
	if _, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce() error = %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	var auditList []*auditv1.AuditEvent
	for time.Now().Before(deadline) {
		response, err := harness.auditClient.Query(
			ownerContext,
			&auditv1.QueryRequest{Limit: 100},
		)
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		auditList = response.GetAuditList()
		if len(auditList) >= 8 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(auditList) != 8 {
		t.Fatalf("audit count = %d, want 8; records = %+v", len(auditList), auditList)
	}

	got := make(map[auditv1.AuditAction]*auditv1.AuditEvent, len(auditList))
	for _, event := range auditList {
		got[event.GetAction()] = event
	}
	want := []auditv1.AuditAction{
		auditv1.AuditAction_AUDIT_ACTION_RECORD_DELETED,
		auditv1.AuditAction_AUDIT_ACTION_LINK_FORCE_CLOSED,
		auditv1.AuditAction_AUDIT_ACTION_DATA_EXPORT,
		auditv1.AuditAction_AUDIT_ACTION_DATA_IMPORT,
		auditv1.AuditAction_AUDIT_ACTION_BASE_DELETED,
		auditv1.AuditAction_AUDIT_ACTION_MEMBER_DISABLED,
		auditv1.AuditAction_AUDIT_ACTION_MEMBER_ENABLED,
		auditv1.AuditAction_AUDIT_ACTION_OWNER_TRANSFERRED,
	}
	for _, action := range want {
		if got[action] == nil {
			t.Fatalf("missing action %s; records = %+v", action, auditList)
		}
	}
	if got[auditv1.AuditAction_AUDIT_ACTION_MEMBER_DISABLED].GetTarget() != memberID ||
		got[auditv1.AuditAction_AUDIT_ACTION_MEMBER_DISABLED].GetOperator() != ownerID {
		t.Fatalf("member.disabled record = %+v", got[auditv1.AuditAction_AUDIT_ACTION_MEMBER_DISABLED])
	}
	if got[auditv1.AuditAction_AUDIT_ACTION_MEMBER_ENABLED].GetTarget() != memberID ||
		got[auditv1.AuditAction_AUDIT_ACTION_MEMBER_ENABLED].GetOperator() != ownerID {
		t.Fatalf("member.enabled record = %+v", got[auditv1.AuditAction_AUDIT_ACTION_MEMBER_ENABLED])
	}
	if got[auditv1.AuditAction_AUDIT_ACTION_OWNER_TRANSFERRED].GetOperator() != ownerID ||
		got[auditv1.AuditAction_AUDIT_ACTION_OWNER_TRANSFERRED].GetTarget() != memberID {
		t.Fatalf("owner transfer record = %+v", got[auditv1.AuditAction_AUDIT_ACTION_OWNER_TRANSFERRED])
	}
	if got[auditv1.AuditAction_AUDIT_ACTION_RECORD_DELETED].
		GetDetailMap().GetFields()["record_id_list"] == nil {
		t.Fatal("record.deleted detail did not preserve record_id_list")
	}
}

// 表删除（core outbox）与导出成功（worker 直发）都必须落审计；同一事件重复
// 投递（worker 直发主题可能被 broker 重投）只允许落一行（缺陷 #23）。
func TestTableDeletedAndDataExportEventsAreAuditedIdempotently(t *testing.T) {
	harness := newAuditEventHarness(t)
	const (
		enterpriseID = "01J00000000000000000008001"
		ownerID      = "01J00000000000000000008002"
		memberID     = "01J00000000000000000008003"
	)
	cleanupAuditEnterprise(harness.db)
	seedAuditEnterprise(t, harness, enterpriseID, ownerID, memberID)
	ownerContext := auditPrincipalContext(t, enterpriseID, ownerID)

	baseID := "01J00000000000000000008004"
	tableID := "01J00000000000000000008005"
	tableDeletedEventID := uniqueEventID("evt_table_deleted")
	exportEventID := uniqueEventID("evt_export_cri")
	exportPayload := map[string]any{
		"enterprise_id":     enterpriseID,
		"operator":          ownerID,
		"base_id":           baseID,
		"table_id":          tableID,
		"view_id":           "01J00000000000000000008006",
		"format":            "xlsx",
		"client_request_id": "t15-export-cri",
	}

	enqueueAuditEvents(t, harness, []struct {
		eventID string
		topic   string
		payload map[string]any
	}{
		{
			eventID: tableDeletedEventID,
			topic:   "table.deleted",
			payload: map[string]any{
				"enterprise_id": enterpriseID,
				"operator":      ownerID,
				"base_id":       baseID,
				"table_id":      tableID,
				"table_name":    "客户表",
			},
		},
		{
			eventID: exportEventID,
			topic:   "export.audited",
			payload: exportPayload,
		},
	})
	if _, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce() error = %v", err)
	}

	// worker 直发的 export.audited 不落 core outbox；模拟 broker 重投同一条事件。
	payloadBytes, err := json.Marshal(exportPayload)
	if err != nil {
		t.Fatalf("marshal export.audited payload: %v", err)
	}
	if err := harness.broker.Publish(context.Background(), eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    exportEventID,
			Type:       "export.audited",
			Version:    1,
			BaseID:     baseID,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "importexport",
		},
		Payload: payloadBytes,
	}); err != nil {
		t.Fatalf("broker.Publish(export.audited replay) error = %v", err)
	}

	// 共享 MySQL 与其它用例并发时有瞬时抖动，消费轮询窗口放宽到 15s。
	deadline := time.Now().Add(15 * time.Second)
	var auditList []*auditv1.AuditEvent
	for time.Now().Before(deadline) {
		response, err := harness.auditClient.Query(
			ownerContext,
			&auditv1.QueryRequest{Limit: 100},
		)
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		auditList = response.GetAuditList()
		if len(auditList) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	byAction := make(map[auditv1.AuditAction][]*auditv1.AuditEvent, 2)
	for _, event := range auditList {
		byAction[event.GetAction()] = append(byAction[event.GetAction()], event)
	}
	tableDeleted := byAction[auditv1.AuditAction_AUDIT_ACTION_TABLE_DELETED]
	if len(tableDeleted) != 1 {
		t.Fatalf("table_deleted audit rows = %d, want 1; records = %+v", len(tableDeleted), auditList)
	}
	if tableDeleted[0].GetOperator() != ownerID ||
		tableDeleted[0].GetBaseId() != baseID ||
		tableDeleted[0].GetTableId() != tableID ||
		tableDeleted[0].GetTarget() != tableID ||
		tableDeleted[0].GetDetailMap().GetFields()["table_name"].GetStringValue() != "客户表" {
		t.Fatalf("table.deleted audit record = %+v", tableDeleted[0])
	}
	dataExport := byAction[auditv1.AuditAction_AUDIT_ACTION_DATA_EXPORT]
	if len(dataExport) != 1 {
		t.Fatalf("data_export audit rows = %d, want 1 after redelivery; records = %+v", len(dataExport), auditList)
	}
	if dataExport[0].GetOperator() != ownerID ||
		dataExport[0].GetDetailMap().GetFields()["client_request_id"].GetStringValue() != "t15-export-cri" {
		t.Fatalf("export.audited audit record = %+v", dataExport[0])
	}
}

// 审计 CSV 必须能导出表删除与数据导出动作（缺陷 #23：CSV 过滤后缺动作）。
func TestExportCSVIncludesTableDeletedAndDataExportActions(t *testing.T) {
	harness := newAuditEventHarness(t)
	const (
		enterpriseID = "01J00000000000000000008001"
		ownerID      = "01J00000000000000000008002"
		memberID     = "01J00000000000000000008003"
	)
	cleanupAuditEnterprise(harness.db)
	seedAuditEnterprise(t, harness, enterpriseID, ownerID, memberID)
	ownerContext := auditPrincipalContext(t, enterpriseID, ownerID)

	enqueueAuditEvents(t, harness, []struct {
		eventID string
		topic   string
		payload map[string]any
	}{
		{
			eventID: uniqueEventID("evt_csv_table_deleted"),
			topic:   "table.deleted",
			payload: map[string]any{
				"enterprise_id": enterpriseID,
				"operator":      ownerID,
				"base_id":       "01J00000000000000000008004",
				"table_id":      "01J00000000000000000008005",
				"table_name":    "客户表",
			},
		},
		{
			eventID: uniqueEventID("evt_csv_export"),
			topic:   "export.audited",
			payload: map[string]any{
				"enterprise_id":     enterpriseID,
				"operator":          ownerID,
				"base_id":           "01J00000000000000000008004",
				"table_id":          "01J00000000000000000008005",
				"format":            "csv",
				"client_request_id": "t15-csv-export-cri",
			},
		},
	})
	if _, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce() error = %v", err)
	}

	// 共享 MySQL 与其它用例并发时有瞬时抖动，消费轮询窗口放宽到 15s。
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		response, err := harness.auditClient.Query(
			ownerContext,
			&auditv1.QueryRequest{Limit: 100},
		)
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if len(response.GetAuditList()) >= 3 { // 两条新事件 + ExportCSV 自身的 data_export
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	stream, err := harness.auditClient.ExportCSV(
		ownerContext,
		&auditv1.ExportCSVRequest{},
	)
	if err != nil {
		t.Fatalf("ExportCSV() error = %v", err)
	}
	exported := collectCSV(t, stream)
	if !strings.Contains(exported, auditv1.AuditAction_AUDIT_ACTION_TABLE_DELETED.String()) {
		t.Fatalf("CSV missing table_deleted action: %q", exported)
	}
	if !strings.Contains(exported, auditv1.AuditAction_AUDIT_ACTION_DATA_EXPORT.String()) {
		t.Fatalf("CSV missing data_export action: %q", exported)
	}
	if !strings.Contains(exported, "t15-csv-export-cri") {
		t.Fatalf("CSV missing export client_request_id detail: %q", exported)
	}
}
