package structure_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/internal/structure"
)

const (
	testMySQLDSN   = "excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true"
	testEnterprise = "01J00000000000000000000001"
	testOwner      = "01J00000000000000000000002"
	testBaseID     = "01J00000000000000000000003"
	testMember     = "01J00000000000000000000004"
)

type structureHarness struct {
	client structurev1.StructureServiceClient
	db     *sql.DB
	roles  *fakeRoleProvider
	impact *fakeImpactCounter
	now    time.Time
}

type fakeRoleProvider struct {
	roles map[string]iamv1.MemberRole
}

func (provider *fakeRoleProvider) Role(
	_ context.Context,
	enterpriseID, userID string,
) (iamv1.MemberRole, error) {
	if enterpriseID != testEnterprise {
		return iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, nil
	}
	role, ok := provider.roles[userID]
	if !ok {
		return iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, nil
	}
	return role, nil
}

type fakeImpactCounter struct {
	records            []string
	conversionAffected int
	conversionCleared  int
}

func (counter *fakeImpactCounter) AffectedRecords(
	_ context.Context,
	_, _ string,
) (int, error) {
	return len(counter.records), nil
}

func (counter *fakeImpactCounter) FieldConversionImpact(
	_ context.Context,
	_, _, _ string,
) (int, int, error) {
	return counter.conversionAffected, counter.conversionCleared, nil
}

func newStructureHarness(t *testing.T) *structureHarness {
	t.Helper()
	db, err := base.OpenDB(testMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetStructureData(t, db)

	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	roles := &fakeRoleProvider{roles: map[string]iamv1.MemberRole{
		testOwner:  iamv1.MemberRole_MEMBER_ROLE_OWNER,
		testMember: iamv1.MemberRole_MEMBER_ROLE_MEMBER,
	}}
	impact := &fakeImpactCounter{}
	repository := base.NewStructureRepository(db)
	service := structure.NewService(repository, roles, impact, nil, func() time.Time { return now })

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	structure.Register(server, service)
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

	harness := &structureHarness{
		client: structurev1.NewStructureServiceClient(conn),
		db:     db,
		roles:  roles,
		impact: impact,
		now:    now,
	}
	t.Cleanup(func() { resetStructureData(t, db) })
	return harness
}

func resetStructureData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_structure_field_options",
		"DELETE FROM excel_structure_views",
		"DELETE FROM excel_structure_fields",
		"DELETE FROM excel_structure_tables",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset structure data: %v", err)
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
		Internal:     true,
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

func configMap(t *testing.T, values map[string]any) *structpb.Struct {
	t.Helper()
	config, err := structpb.NewStruct(values)
	if err != nil {
		t.Fatalf("structpb.NewStruct(%v) error = %v", values, err)
	}
	return config
}

func fieldByName(t *testing.T, fields []*structurev1.Field, name string) *structurev1.Field {
	t.Helper()
	for _, field := range fields {
		if strings.TrimSpace(field.GetName()) == name {
			return field
		}
	}
	t.Fatalf("field %q not found in %v", name, fieldNames(fields))
	return nil
}

func fieldNames(fields []*structurev1.Field) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.GetName())
	}
	return names
}

func createTable(t *testing.T, harness *structureHarness, name string) *structurev1.Table {
	t.Helper()
	response, err := harness.client.CreateTable(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.CreateTableRequest{BaseId: testBaseID, Name: name},
	)
	if err != nil {
		t.Fatalf("CreateTable(%q) error = %v", name, err)
	}
	return response.GetTable()
}
