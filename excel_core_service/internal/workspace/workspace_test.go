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
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	workspacev1 "excel/api/workspace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"excel_core_service/base"
	corebus "excel_core_service/bus"
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
	client          workspacev1.WorkspaceServiceClient
	db              *sql.DB
	repository      *base.WorkspaceRepository
	structure       *base.StructureRepository
	roles           *fakeRoleProvider
	invitations     *fakeInvitationProvider
	records         *fakeRecordsDuplicator
	recordDeleter   *fakeRecordsDeleter
	comments        *fakeCommentsDeleter
	links           *fakeLinksCleaner
	outbox          *fakeOutbox
	events          *eventbus.MemoryBus
	forwarder       *corebus.Forwarder
	auditEvents     eventbus.Subscription
	now             time.Time
	workspaceServer *workspace.Service
}

type fakeRoleProvider struct {
	roles map[string]iamv1.MemberRole
	err   error
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
	calls []string
	err   error
}

func (duplicator *fakeRecordsDuplicator) DuplicateTableRows(
	_ context.Context,
	sourceTableID, targetTableID string,
) error {
	duplicator.calls = append(duplicator.calls, sourceTableID+"->"+targetTableID)
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

type fakeCommentsDeleter struct {
	baseIDs []string
	err     error
}

func (deleter *fakeCommentsDeleter) DeleteBaseComments(
	_ context.Context,
	baseID string,
) error {
	deleter.baseIDs = append(deleter.baseIDs, baseID)
	return deleter.err
}

type fakeLinksCleaner struct {
	baseIDs []string
	err     error
}

func (cleaner *fakeLinksCleaner) DeleteAllForBase(
	_ context.Context,
	baseID string,
) error {
	cleaner.baseIDs = append(cleaner.baseIDs, baseID)
	return cleaner.err
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
	records := &fakeRecordsDuplicator{}
	recordDeleter := &fakeRecordsDeleter{}
	comments := &fakeCommentsDeleter{}
	links := &fakeLinksCleaner{}
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
	service := workspace.NewService(
		repository,
		roles,
		invitations,
		records,
		recordDeleter,
		comments,
		workspace.NewStructureBootstrap(structureRepository),
		workspace.NewStructureCleanup(structureRepository),
		links,
		outbox,
		func() time.Time { return now },
		workspace.NewStructureDuplicator(structureRepository, func() time.Time { return now }),
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
		records:         records,
		recordDeleter:   recordDeleter,
		comments:        comments,
		links:           links,
		outbox:          outbox,
		events:          events,
		forwarder:       forwarder,
		auditEvents:     auditEvents,
		now:             now,
		workspaceServer: service,
	}
}

func resetWorkspaceData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
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
