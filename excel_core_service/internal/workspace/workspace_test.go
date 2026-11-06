package workspace_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net"
	"testing"
	"time"

	"excel-libs/eventbus"
	"excel-libs/idgen"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	recordsv1 "excel/api/records/v1"
	sharev1 "excel/api/share/v1"
	structurev1 "excel/api/structure/v1"
	workspacev1 "excel/api/workspace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"excel_core_service/base"
	corebus "excel_core_service/bus"
	"excel_core_service/internal/comment"
	"excel_core_service/internal/records"
	"excel_core_service/internal/share"
	"excel_core_service/internal/structure"
	"excel_core_service/internal/workspace"
)

const (
	testMySQLDSN   = "excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true"
	testEnterprise = "01J00000000000000000000101"
	testOwner      = "01J00000000000000000000102"
	testManager    = "01J00000000000000000000103"
	testMember     = "01J00000000000000000000104"
)

type workspaceHarness struct {
	client            workspacev1.WorkspaceServiceClient
	structureClient   structurev1.StructureServiceClient
	recordsClient     recordsv1.RecordsServiceClient
	db                *sql.DB
	repository        *base.WorkspaceRepository
	structure         *base.StructureRepository
	recordsStore      *base.RecordsRepository
	calcRepository    *base.CalcRepository
	commentRepository *base.CommentRepository
	roles             *fakeRoleProvider
	invitations       *fakeInvitationProvider
	records           *fakeRecordsDuplicator
	outbox            *fakeOutbox
	events            *eventbus.MemoryBus
	forwarder         *corebus.Forwarder
	auditEvents       eventbus.Subscription
	now               time.Time
	workspaceServer   *workspace.Service
}

type fakeRoleProvider struct {
	roles map[string]iamv1.MemberRole
	err   error
}

// fixedRoleAccess 以固定角色驱动 records 读回夹具（不经过 share 裁决）。
type fixedRoleAccess struct {
	role sharev1.Role
}

// Role returns the stubbed role for every Base.
func (access fixedRoleAccess) Role(
	context.Context,
	*commonv1.Principal,
	string,
) (sharev1.Role, error) {
	return access.role, nil
}

func (provider *fakeRoleProvider) Role(
	_ context.Context,
	enterpriseID, userID string,
) (iamv1.MemberRole, error) {
	if provider.err != nil {
		return iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, provider.err
	}
	if enterpriseID != testEnterprise {
		return iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, nil
	}
	return provider.roles[userID], nil
}

type fakeInvitationProvider struct {
	invitations []*iamv1.Invitation
	err         error
}

func (provider *fakeInvitationProvider) ListPendingInvitations(
	_ context.Context,
) ([]*iamv1.Invitation, error) {
	return provider.invitations, provider.err
}

type fakeRecordsDuplicator struct {
	calls []map[string]string
	err   error
}

func (duplicator *fakeRecordsDuplicator) DuplicateBaseRows(
	_ context.Context,
	tableIDs map[string]string,
) error {
	duplicator.calls = append(duplicator.calls, tableIDs)
	return duplicator.err
}

type fakeRecordsDeleter struct {
	baseIDs  []string
	tableIDs []string
	err      error
}

func (deleter *fakeRecordsDeleter) DeleteBaseRows(
	_ context.Context,
	baseID string,
	tableIDList []string,
) error {
	deleter.baseIDs = append(deleter.baseIDs, baseID)
	deleter.tableIDs = append(deleter.tableIDs, tableIDList...)
	return deleter.err
}

type testLinksCleaner struct {
	repository *base.ShareRepository
}

func (cleaner testLinksCleaner) DeleteAllForBase(
	ctx context.Context,
	baseID string,
) error {
	return share.DeleteAllForBase(ctx, cleaner.repository, baseID)
}

type fakeOutbox struct {
	writer *corebus.Outbox
	events []eventbus.Event
	err    error
}

func (outbox *fakeOutbox) Enqueue(
	ctx context.Context,
	tx *sql.Tx,
	event eventbus.Event,
) error {
	if outbox.err != nil {
		return outbox.err
	}
	if outbox.writer != nil {
		if err := outbox.writer.Enqueue(ctx, tx, event); err != nil {
			return err
		}
	}
	outbox.events = append(outbox.events, event.Clone())
	return nil
}

func newWorkspaceHarness(t *testing.T) *workspaceHarness {
	return newWorkspaceHarnessWith(t, nil, nil)
}

// newWorkspaceHarnessWith 构造 bufconn 测试服；dependencies 透传给
// workspace.NewService 的可变依赖（如 profile provider，见 modifier_name_test.go）。
func newWorkspaceHarnessWith(
	t *testing.T,
	recordDeleter workspace.RecordsDeleter,
	comments workspace.CommentsDeleter,
	dependencies ...any,
) *workspaceHarness {
	t.Helper()
	db, err := base.OpenDB(testMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetWorkspaceData(t, db)
	t.Cleanup(func() { resetWorkspaceData(t, db) })

	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	repository := base.NewWorkspaceRepository(db)
	structureRepository := base.NewStructureRepository(db)
	roles := &fakeRoleProvider{roles: map[string]iamv1.MemberRole{
		testOwner:   iamv1.MemberRole_MEMBER_ROLE_OWNER,
		testManager: iamv1.MemberRole_MEMBER_ROLE_MANAGER,
		testMember:  iamv1.MemberRole_MEMBER_ROLE_MEMBER,
	}}
	invitations := &fakeInvitationProvider{}
	duplicator := &fakeRecordsDuplicator{}
	if recordDeleter == nil {
		recordDeleter = records.NewBaseRowsDeleter(base.NewRecordsRepository(db))
	}
	if comments == nil {
		comments = comment.NewBaseCommentsDeleter(base.NewCommentRepository(db))
	}
	links := testLinksCleaner{repository: base.NewShareRepository(db)}
	outbox := &fakeOutbox{writer: corebus.NewOutbox(base.NewBusStore(db))}
	events := eventbus.NewMemoryBus()
	forwarder := corebus.NewForwarder(base.NewBusStore(db), events, corebus.ForwarderConfig{})
	auditEvents, err := events.Subscribe(
		context.Background(),
		eventbus.SubscribeOptions{Topic: "audit.append"},
	)
	if err != nil {
		t.Fatalf("Subscribe(audit.append) error = %v", err)
	}
	t.Cleanup(func() { _ = auditEvents.Close() })
	// 结构复制器为固定依赖；追加依赖经 dependencies 透传（NewService 可变参数）。
	dependencies = append([]any{
		workspace.NewStructureDuplicator(structureRepository, func() time.Time { return now }),
	}, dependencies...)
	service := workspace.NewService(
		repository,
		roles,
		invitations,
		duplicator,
		recordDeleter,
		comments,
		workspace.NewStructureBootstrap(structureRepository),
		workspace.NewStructureCleanup(structureRepository),
		links,
		outbox,
		func() time.Time { return now },
		dependencies...,
	)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	workspace.Register(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &workspaceHarness{
		client:          workspacev1.NewWorkspaceServiceClient(conn),
		db:              db,
		repository:      repository,
		structure:       structureRepository,
		roles:           roles,
		invitations:     invitations,
		records:         duplicator,
		outbox:          outbox,
		events:          events,
		forwarder:       forwarder,
		auditEvents:     auditEvents,
		now:             now,
		workspaceServer: service,
	}
}

// rowScopeAllProvider 满足 records.RowScopeProvider：包级测试统一 ALL 行权。
type rowScopeAllProvider struct{}

func (rowScopeAllProvider) RowScope(
	context.Context,
	*commonv1.Principal,
	string,
) (string, error) {
	return "ROW_SCOPE_ALL", nil
}

// newWorkspaceFullHarness 在同一 bufconn 上注册真实 structure/records 服务并
// 装配真实记录行复制器，记录断言一律经 records RPC 读回（Testing Decisions：
// 只断言部署后外部行为）。recordsDuplicator 传 nil 用真实适配器，注入 fake
// 用于失败回滚路径。
func newWorkspaceFullHarness(t *testing.T) *workspaceHarness {
	return newWorkspaceFullHarnessWithDuplicator(t, nil)
}

func newWorkspaceFullHarnessWithDuplicator(
	t *testing.T,
	recordsDuplicator workspace.RecordsDuplicator,
) *workspaceHarness {
	t.Helper()
	db, err := base.OpenDB(testMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetWorkspaceData(t, db)
	t.Cleanup(func() { resetWorkspaceData(t, db) })

	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	repository := base.NewWorkspaceRepository(db)
	structureRepository := base.NewStructureRepository(db)
	recordsRepository := base.NewRecordsRepository(db)
	calcRepository := base.NewCalcRepository(db)
	roles := &fakeRoleProvider{roles: map[string]iamv1.MemberRole{
		testOwner:   iamv1.MemberRole_MEMBER_ROLE_OWNER,
		testManager: iamv1.MemberRole_MEMBER_ROLE_MANAGER,
		testMember:  iamv1.MemberRole_MEMBER_ROLE_MEMBER,
	}}
	invitations := &fakeInvitationProvider{}
	duplicator := &fakeRecordsDuplicator{}
	if recordsDuplicator == nil {
		recordsDuplicator = records.NewTableRowsDuplicator(
			recordsRepository,
			structureRepository,
			calcRepository,
			func() time.Time { return now },
		)
	}
	links := testLinksCleaner{repository: base.NewShareRepository(db)}
	outbox := &fakeOutbox{writer: corebus.NewOutbox(base.NewBusStore(db))}

	structureService := structure.NewService(
		structureRepository,
		roles,
		nil, nil,
		func() time.Time { return now },
		nil,
	)
	commentRepository := base.NewCommentRepository(db)
	recordsService := records.NewService(
		recordsRepository,
		records.NewStructureViewAccessor(structureRepository, nil),
		rowScopeAllProvider{},
		// 夹具按 editor 角色驱动（历史可见性矩阵由 records 包锁定；本包只读回
		// 复制语义：副本记录历史为空）。
		fixedRoleAccess{role: sharev1.Role_ROLE_EDITOR},
		records.NewCalcFormulaValueProvider(calcRepository),
		func() time.Time { return now },
		commentRepository,
	)

	service := workspace.NewService(
		repository,
		roles,
		invitations,
		recordsDuplicator,
		records.NewBaseRowsDeleter(recordsRepository),
		comment.NewBaseCommentsDeleter(commentRepository),
		workspace.NewStructureBootstrap(structureRepository),
		workspace.NewStructureCleanup(structureRepository),
		links,
		outbox,
		func() time.Time { return now },
		workspace.NewStructureDuplicator(structureRepository, func() time.Time { return now }),
	)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	structure.Register(server, structureService)
	records.Register(server, recordsService)
	workspace.Register(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &workspaceHarness{
		client:            workspacev1.NewWorkspaceServiceClient(conn),
		structureClient:   structurev1.NewStructureServiceClient(conn),
		recordsClient:     recordsv1.NewRecordsServiceClient(conn),
		db:                db,
		repository:        repository,
		structure:         structureRepository,
		recordsStore:      recordsRepository,
		calcRepository:    calcRepository,
		commentRepository: commentRepository,
		roles:             roles,
		invitations:     invitations,
		records:         duplicator,
		outbox:          outbox,
		now:             now,
		workspaceServer: service,
	}
}

// queryContext 携带操作者身份与 records 读链要求的 x-row-scope 元数据。
func queryContext(
	t *testing.T,
	userID, enterpriseID string,
) context.Context {
	t.Helper()
	return metadata.AppendToOutgoingContext(
		authenticatedContext(t, userID, enterpriseID),
		"x-row-scope",
		"ROW_SCOPE_ALL",
	)
}

// seedRecord 直接经 records 仓储落一条源记录（夹具准备，断言仍走 RPC 读回）。
func seedRecord(
	t *testing.T,
	harness *workspaceHarness,
	tableID, createdBy string,
	createdAt time.Time,
	version int64,
	cells map[string]*commonv1.Value,
) base.Record {
	t.Helper()
	recordID, err := idgen.New()
	if err != nil {
		t.Fatalf("idgen.New() error = %v", err)
	}
	tx, err := harness.recordsStore.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("records BeginTx() error = %v", err)
	}
	record := base.Record{
		ID:        recordID,
		TableID:   tableID,
		Cells:     cells,
		CreatedBy: createdBy,
		CreatedAt: createdAt.UnixMilli(),
		Version:   version,
	}
	if err := harness.recordsStore.CreateRecordTx(context.Background(), tx, record); err != nil {
		_ = tx.Rollback()
		t.Fatalf("seed record: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seeded record: %v", err)
	}
	return record
}

func resetWorkspaceData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_comment_comments",
		"DELETE FROM excel_records_histories",
		"DELETE FROM excel_records_oplog",
		"DELETE FROM excel_records",
		"DELETE FROM excel_calc_materialized",
		"DELETE FROM excel_calc_jobs",
		"DELETE FROM excel_workspace_stars",
		"DELETE FROM excel_workspace_recent",
		"DELETE FROM excel_workspace_bases",
		"DELETE FROM excel_structure_field_options",
		"DELETE FROM excel_structure_views",
		"DELETE FROM excel_structure_fields",
		"DELETE FROM excel_structure_tables",
		"DELETE FROM excel_share_collaborators",
		"DELETE FROM excel_share_links",
		"DELETE FROM excel_bus_outbox",
		"DELETE FROM excel_audit_events",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset workspace data: %v", err)
		}
	}
}

func authenticatedContext(
	t *testing.T,
	userID, enterpriseID string,
) context.Context {
	t.Helper()
	payload, err := proto.Marshal(&commonv1.Principal{
		UserId:       userID,
		EnterpriseId: enterpriseID,
		Internal:     enterpriseID != "",
	})
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
	)
}

func requireErrorCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want business code %d", want)
	}
	grpcStatus, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	for _, detail := range grpcStatus.Details() {
		if info, ok := detail.(interface{ GetReason() string }); ok {
			if info.GetReason() == fmt.Sprint(want) {
				return
			}
		}
	}
	t.Fatalf("error = %v, details = %v, want business code %d", err, grpcStatus.Details(), want)
}
