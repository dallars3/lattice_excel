package audit_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	auditv1 "excel/api/audit/v1"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	"excel_core_service/base"
	"excel_core_service/internal/audit"
)

type fakeRoleProvider map[string]iamv1.MemberRole

func (provider fakeRoleProvider) Role(
	_ context.Context,
	enterpriseID, userID string,
) (iamv1.MemberRole, error) {
	return provider[enterpriseID+"/"+userID], nil
}

type auditHarness struct {
	db     *sql.DB
	client auditv1.AuditServiceClient
}

func newAuditHarness(t *testing.T, roles fakeRoleProvider) *auditHarness {
	t.Helper()

	config, err := base.LoadConfig(filepath.Join("..", "..", "configs", "core_service_config.yaml"))
	if err != nil {
		t.Fatalf("base.LoadConfig() error = %v", err)
	}
	db, err := base.OpenDB(config.MySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	if err := base.MigrateUp(db); err != nil {
		_ = db.Close()
		t.Fatalf("base.MigrateUp() error = %v", err)
	}

	service := audit.NewService(base.NewAuditRepository(db), roles)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	audit.Register(server, service)
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
		_ = db.Close()
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
		_ = db.Close()
	})
	return &auditHarness{
		db:     db,
		client: auditv1.NewAuditServiceClient(conn),
	}
}

func TestAppendIsIdempotentByEventID(t *testing.T) {
	const (
		enterpriseID = "01J00000000000000000007001"
		ownerID      = "01J00000000000000000007002"
		eventID      = "evt_audit_idempotent"
	)
	harness := newAuditHarness(t, fakeRoleProvider{
		enterpriseID + "/" + ownerID: iamv1.MemberRole_MEMBER_ROLE_OWNER,
	})
	t.Cleanup(func() {
		_, _ = harness.db.ExecContext(
			context.Background(),
			"DELETE FROM excel_audit_events WHERE event_id = ?",
			eventID,
		)
	})
	ctx := auditPrincipalContext(t, enterpriseID, ownerID)
	detail, err := structpb.NewStruct(map[string]any{
		"enterprise_id": enterpriseID,
		"source":        "test",
	})
	if err != nil {
		t.Fatalf("structpb.NewStruct() error = %v", err)
	}
	request := &auditv1.AppendRequest{
		EventId:   eventID,
		Action:    auditv1.AuditAction_AUDIT_ACTION_DATA_EXPORT,
		Operator:  ownerID,
		Target:    "base_exported",
		BaseId:    "01J00000000000000000007003",
		DetailMap: detail,
	}
	if _, err := harness.client.Append(ctx, request); err != nil {
		t.Fatalf("Append(first) error = %v", err)
	}
	if _, err := harness.client.Append(ctx, request); err != nil {
		t.Fatalf("Append(repeat) error = %v, want idempotent success", err)
	}

	response, err := harness.client.Query(ctx, &auditv1.QueryRequest{})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(response.GetAuditList()) != 1 {
		t.Fatalf("audit count = %d, want 1; response = %+v", len(response.GetAuditList()), response)
	}
	got := response.GetAuditList()[0]
	if got.GetAction() != auditv1.AuditAction_AUDIT_ACTION_DATA_EXPORT ||
		got.GetOperator() != ownerID ||
		got.GetTarget() != "base_exported" ||
		got.GetDetailMap().GetFields()["source"].GetStringValue() != "test" {
		t.Fatalf("audit record = %+v", got)
	}
}

func TestQueryAndExportOnlyAllowOwnerOrManager(t *testing.T) {
	const enterpriseID = "01J00000000000000000007101"
	users := []struct {
		name string
		id   string
		role iamv1.MemberRole
	}{
		{name: "owner", id: "01J00000000000000000007102", role: iamv1.MemberRole_MEMBER_ROLE_OWNER},
		{name: "manager", id: "01J00000000000000000007103", role: iamv1.MemberRole_MEMBER_ROLE_MANAGER},
		{name: "member", id: "01J00000000000000000007104", role: iamv1.MemberRole_MEMBER_ROLE_MEMBER},
		{name: "unspecified", id: "01J00000000000000000007105", role: iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED},
	}
	roles := fakeRoleProvider{}
	for _, user := range users {
		roles[enterpriseID+"/"+user.id] = user.role
	}
	harness := newAuditHarness(t, roles)
	cleanupAuditEnterpriseID(t, harness.db, enterpriseID)
	t.Cleanup(func() {
		cleanupAuditEnterpriseID(t, harness.db, enterpriseID)
	})

	for _, user := range users {
		t.Run(user.name, func(t *testing.T) {
			ctx := auditPrincipalContext(t, enterpriseID, user.id)
			response, err := harness.client.Query(ctx, &auditv1.QueryRequest{})
			stream, streamErr := harness.client.ExportCSV(
				ctx,
				&auditv1.ExportCSVRequest{},
			)
			allowed := user.role == iamv1.MemberRole_MEMBER_ROLE_OWNER ||
				user.role == iamv1.MemberRole_MEMBER_ROLE_MANAGER
			if allowed {
				if err != nil {
					t.Fatalf("Query() error = %v", err)
				}
				if response == nil {
					t.Fatal("Query() response = nil")
				}
				if streamErr != nil {
					t.Fatalf("ExportCSV() create error = %v", streamErr)
				}
				exported := collectCSV(t, stream)
				if !strings.HasPrefix(exported, "id,event_id,action,operator,target,base_id,table_id,detail,occurred_at\n") {
					t.Fatalf("CSV header = %q", exported)
				}
				return
			}
			requireAuditStatus(t, err, codes.PermissionDenied, 3001)
			if streamErr != nil {
				requireAuditStatus(t, streamErr, codes.PermissionDenied, 3001)
				return
			}
			_, exportErr := stream.Recv()
			requireAuditStatus(t, exportErr, codes.PermissionDenied, 3001)
		})
	}
}

func TestQueryAndExportDefaultToRecent180Days(t *testing.T) {
	const (
		enterpriseID = "01J00000000000000000007201"
		ownerID      = "01J00000000000000000007202"
		recentID     = "evt_audit_recent_180"
		oldID        = "evt_audit_old_180"
	)
	harness := newAuditHarness(t, fakeRoleProvider{
		enterpriseID + "/" + ownerID: iamv1.MemberRole_MEMBER_ROLE_OWNER,
	})
	cleanupAuditEnterpriseID(t, harness.db, enterpriseID)
	t.Cleanup(func() {
		cleanupAuditEnterpriseID(t, harness.db, enterpriseID)
	})
	repository := base.NewAuditRepository(harness.db)
	for _, record := range []base.AuditEventRecord{
		{
			EventID:      recentID,
			EnterpriseID: enterpriseID,
			Action:       auditv1.AuditAction_AUDIT_ACTION_DATA_EXPORT.String(),
			Operator:     ownerID,
			Target:       "recent",
			OccurredAt:   time.Now().Add(-10 * 24 * time.Hour).UTC(),
		},
		{
			EventID:      oldID,
			EnterpriseID: enterpriseID,
			Action:       auditv1.AuditAction_AUDIT_ACTION_DATA_EXPORT.String(),
			Operator:     ownerID,
			Target:       "old",
			OccurredAt:   time.Now().Add(-181 * 24 * time.Hour).UTC(),
		},
	} {
		if err := repository.Append(context.Background(), record); err != nil {
			t.Fatalf("seed audit event %s: %v", record.EventID, err)
		}
	}

	ctx := auditPrincipalContext(t, enterpriseID, ownerID)
	response, err := harness.client.Query(ctx, &auditv1.QueryRequest{})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(response.GetAuditList()) != 1 ||
		response.GetAuditList()[0].GetTarget() != "recent" {
		t.Fatalf("default window audit_list = %+v, want only recent record", response.GetAuditList())
	}
	stream, err := harness.client.ExportCSV(ctx, &auditv1.ExportCSVRequest{})
	if err != nil {
		t.Fatalf("ExportCSV() error = %v", err)
	}
	exported := collectCSV(t, stream)
	if !strings.Contains(exported, recentID) || strings.Contains(exported, oldID) {
		t.Fatalf("default-window CSV = %q", exported)
	}
}

func TestAuditPersistenceHasNoUpdateOrDeleteStatements(t *testing.T) {
	prohibited := regexp.MustCompile(`(?i)\b(?:UPDATE|DELETE)\b`)
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("filepath.Glob() error = %v", err)
	}
	paths = append(paths, filepath.Join("..", "..", "base", "audit.go"))
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if match := prohibited.Find(contents); match != nil {
			t.Fatalf("%s contains prohibited mutation keyword %q", path, match)
		}
	}
}

func TestExportCSVRecordsItsOwnDataExportAction(t *testing.T) {
	const (
		enterpriseID = "01J00000000000000000007301"
		ownerID      = "01J00000000000000000007302"
	)
	harness := newAuditHarness(t, fakeRoleProvider{
		enterpriseID + "/" + ownerID: iamv1.MemberRole_MEMBER_ROLE_OWNER,
	})
	cleanupAuditEnterpriseID(t, harness.db, enterpriseID)
	t.Cleanup(func() {
		cleanupAuditEnterpriseID(t, harness.db, enterpriseID)
	})
	ctx := auditPrincipalContext(t, enterpriseID, ownerID)
	stream, err := harness.client.ExportCSV(ctx, &auditv1.ExportCSVRequest{})
	if err != nil {
		t.Fatalf("ExportCSV() error = %v", err)
	}
	_ = collectCSV(t, stream)

	response, err := harness.client.Query(ctx, &auditv1.QueryRequest{})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(response.GetAuditList()) != 1 ||
		response.GetAuditList()[0].GetAction() != auditv1.AuditAction_AUDIT_ACTION_DATA_EXPORT ||
		response.GetAuditList()[0].GetTarget() != "audit_csv" {
		t.Fatalf("export action audit_list = %+v", response.GetAuditList())
	}
}

func auditPrincipalContext(t *testing.T, enterpriseID, userID string) context.Context {
	t.Helper()
	payload, err := proto.Marshal(&commonv1.Principal{
		UserId:       userID,
		EnterpriseId: enterpriseID,
		Internal:     true,
	})
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
	))
}

func auditErrorReason(err error) string {
	for _, detail := range status.Convert(err).Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}
	return ""
}

func requireAuditStatus(t *testing.T, err error, wantCode codes.Code, wantReason int) {
	t.Helper()
	if status.Code(err) != wantCode ||
		auditErrorReason(err) != strconv.Itoa(wantReason) {
		t.Fatalf("error = %v, want gRPC %s reason %d", err, wantCode, wantReason)
	}
}

func uniqueEventID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func collectCSV(
	t *testing.T,
	stream auditv1.AuditService_ExportCSVClient,
) string {
	t.Helper()
	var output bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			return output.String()
		}
		if err != nil {
			t.Fatalf("ExportCSV().Recv() error = %v", err)
		}
		output.Write(chunk.GetChunk())
	}
}

func cleanupAuditEnterpriseID(t *testing.T, db *sql.DB, enterpriseID string) {
	t.Helper()
	if _, err := db.ExecContext(
		context.Background(),
		"DELETE FROM excel_audit_events WHERE enterprise_id = ?",
		enterpriseID,
	); err != nil {
		t.Fatalf("cleanup audit events for %s: %v", enterpriseID, err)
	}
}
