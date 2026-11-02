// structure/common.go：模块内可复用逻辑归口。
package structure

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
	"excel_core_service/internal/calc"
)

// RoleProvider is the structure module's read-only effective-member-role seam.
type RoleProvider interface {
	Role(ctx context.Context, enterpriseID, userID string) (iamv1.MemberRole, error)
}

// RecordsImpactCounter is implemented by the records module in production and
// by a fake at the structure service seam.
type RecordsImpactCounter interface {
	AffectedRecords(ctx context.Context, tableID, fieldID string) (int, error)
}

// FieldConversionImpactCounter is the optional records-module adapter for
// conversion preview counts. When absent, structure falls back to affected
// records and reports no additional cleared values.
type FieldConversionImpactCounter interface {
	FieldConversionImpact(
		ctx context.Context,
		tableID, fieldID, targetType string,
	) (affected, cleared int, err error)
}

// TableRecordsDuplicator is the optional records-module adapter for copying
// table data together with table structure.
type TableRecordsDuplicator interface {
	DuplicateTableRecords(ctx context.Context, sourceTableID, targetTableID string) error
}

// TableRecordsDeleter is the optional records-module adapter for deleting the
// data owned by a dropped table.
type TableRecordsDeleter interface {
	DeleteTableRecords(ctx context.Context, tableID string) error
}

// CalcValidator validates one complete proposed formula/relation/lookup graph.
type CalcValidator interface {
	ValidateDependencyGraph(ctx context.Context, fields []calc.FieldDefinition) error
}

type defaultCalcValidator struct{}

func (defaultCalcValidator) ValidateDependencyGraph(
	_ context.Context,
	fields []calc.FieldDefinition,
) error {
	return calc.ValidateDependencyGraph(fields)
}

// Service implements StructureService.
type Service struct {
	structurev1.UnimplementedStructureServiceServer

	repository    *base.StructureRepository
	roles         RoleProvider
	impact        RecordsImpactCounter
	calcValidator CalcValidator
	schema        *SchemaProvider
	now           func() time.Time
}

// NewService constructs the structure RPC service with explicit external seams.
func NewService(
	repository *base.StructureRepository,
	roles RoleProvider,
	impact RecordsImpactCounter,
	calcValidator CalcValidator,
	now func() time.Time,
) *Service {
	if now == nil {
		now = time.Now
	}
	if calcValidator == nil {
		calcValidator = defaultCalcValidator{}
	}
	return &Service{
		repository:    repository,
		roles:         roles,
		impact:        impact,
		calcValidator: calcValidator,
		schema:        NewSchemaProvider(repository),
		now:           now,
	}
}

// Register installs the structure service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *Service) {
	structurev1.RegisterStructureServiceServer(registrar, service)
}

func principalFromContext(ctx context.Context) (*commonv1.Principal, error) {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	values := incoming.Get("x-principal")
	if len(values) == 0 {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	payload, err := base64.StdEncoding.DecodeString(values[0])
	if err != nil {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	var principal commonv1.Principal
	if err := proto.Unmarshal(payload, &principal); err != nil ||
		principal.GetUserId() == "" ||
		principal.GetEnterpriseId() == "" {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	return &principal, nil
}

func (service *Service) memberPrincipal(
	ctx context.Context,
) (*commonv1.Principal, iamv1.MemberRole, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, err
	}
	if service == nil || service.roles == nil {
		return nil, iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, rpcError(deal.CodePermissionDenied)
	}
	role, err := service.roles.Role(ctx, principal.GetEnterpriseId(), principal.GetUserId())
	if err != nil {
		return nil, iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, err
	}
	if role == iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED {
		return nil, role, rpcError(deal.CodePermissionDenied)
	}
	return principal, role, nil
}

func (service *Service) managerPrincipal(
	ctx context.Context,
) (*commonv1.Principal, iamv1.MemberRole, error) {
	principal, role, err := service.memberPrincipal(ctx)
	if err != nil {
		return nil, role, err
	}
	if role != iamv1.MemberRole_MEMBER_ROLE_OWNER &&
		role != iamv1.MemberRole_MEMBER_ROLE_MANAGER {
		return nil, role, rpcError(deal.CodePermissionDenied)
	}
	return principal, role, nil
}

func rpcError(code int) error {
	spec, ok := deal.LookupError(code)
	if !ok {
		spec = deal.ErrorSpec{Message: "服务暂不可用", GRPCCode: codes.Unavailable}
	}
	withDetails, err := status.New(spec.GRPCCode, spec.Message).WithDetails(&errdetails.ErrorInfo{
		Reason: strconv.Itoa(code),
	})
	if err != nil {
		return status.Error(spec.GRPCCode, spec.Message)
	}
	return withDetails.Err()
}

func mapStructureError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, base.ErrStructureNotFound) {
		return rpcError(deal.CodeDeletedResource)
	}
	var graphErr *calc.DependencyCycleError
	if errors.As(err, &graphErr) {
		return rpcError(deal.CodeInvalidArgument)
	}
	return err
}

func tableToProto(table base.StructureTable) *structurev1.Table {
	return &structurev1.Table{
		Id:         table.ID,
		BaseId:     table.BaseID,
		Name:       table.Name,
		FieldCount: int32(table.FieldCount),
		ViewCount:  int32(table.ViewCount),
		CreatedAt:  table.CreatedAt,
	}
}

func fieldToProto(field base.StructureField) (*structurev1.Field, error) {
	fieldType := structurev1.FieldType(structurev1.FieldType_value[field.Type])
	config, err := base.DecodeStructureConfig(field.Config)
	if err != nil {
		return nil, err
	}
	configMap, err := structpb.NewStruct(config)
	if err != nil {
		return nil, err
	}
	return &structurev1.Field{
		Id:        field.ID,
		TableId:   field.TableID,
		Name:      field.Name,
		Type:      fieldType,
		ConfigMap: configMap,
		IsPrimary: field.IsPrimary,
		Position:  int32(field.Position),
	}, nil
}

func viewToProto(view base.StructureView) *structurev1.View {
	return &structurev1.View{
		Id:        view.ID,
		TableId:   view.TableID,
		Name:      view.Name,
		Type:      view.Type,
		CreatedAt: view.CreatedAt,
	}
}

func fieldTypeString(fieldType structurev1.FieldType) (string, bool) {
	if fieldType <= structurev1.FieldType_FIELD_TYPE_UNSPECIFIED ||
		fieldType > structurev1.FieldType_FIELD_TYPE_LOOKUP {
		return "", false
	}
	return fieldType.String(), true
}
