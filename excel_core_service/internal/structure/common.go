// structure/common.go：模块内可复用逻辑归口。
package structure

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc"
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

// ReadAccess 是结构域只读接口的 Base 级授权 seam。生产实现由 share.CheckAccess
// 适配（share_read_access.go）；测试注入 fake。无企业归属的外部账号在解析出
// Base 后必须经此校验，避免凭 table_id/view_id 读取其它 Base 的结构。
type ReadAccess interface {
	AllowRead(ctx context.Context, principal *commonv1.Principal, baseID string) error
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
	readAccess    ReadAccess
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
	readAccess ReadAccess,
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
		readAccess:    readAccess,
		schema:        NewSchemaProvider(repository),
		now:           now,
	}
}

// Register installs the structure service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *Service) {
	structurev1.RegisterStructureServiceServer(registrar, service)
}

// principalFromContext extracts the principal and enforces enterprise context
// for structure APIs（结构操作必须挂靠企业）。
func principalFromContext(ctx context.Context) (*commonv1.Principal, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if principal.GetEnterpriseId() == "" {
		return nil, deal.RPCError(deal.CodeUnauthenticated)
	}
	return principal, nil
}

// readPrincipal resolves the caller of a read-only structure API. 无企业归属的
// 外部账号（分享链接访客/外部协作者）不要求企业成员角色：Base 级授权已由网关
// CheckAccess 完成，租户边界改为数据表/视图所属 Base 的企业（与记录域现有
// 外部协作者口径一致）；有企业归属的账号仍要求有效成员角色。
func (service *Service) readPrincipal(ctx context.Context) (*commonv1.Principal, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if principal.GetEnterpriseId() == "" {
		return principal, nil
	}
	principal, _, err = service.memberPrincipal(ctx)
	return principal, err
}

// authorizeRead 对无企业归属的外部账号执行 Base 级授权。内部成员已在
// readPrincipal 通过企业成员校验，不需要重复调用；seam 未装配时 fail-closed。
func (service *Service) authorizeRead(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
) error {
	if principal.GetEnterpriseId() != "" {
		return nil
	}
	if service == nil || service.readAccess == nil {
		return deal.RPCError(deal.CodePermissionDenied)
	}
	return service.readAccess.AllowRead(ctx, principal, baseID)
}

// tablesInBaseForRead 列出 Base 内数据表：内部成员按企业租户过滤；外部账号
// 不带企业过滤，租户边界由 Base 级授权承担。
func (service *Service) tablesInBaseForRead(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
) ([]base.StructureTable, error) {
	if principal.GetEnterpriseId() == "" {
		return service.repository.TablesByBaseCollaborator(ctx, baseID)
	}
	return service.repository.TablesByBase(ctx, principal.GetEnterpriseId(), baseID)
}

// tableForReadTenancy 解析只读接口的数据表行：内部成员按企业租户断言；外部
// 账号改用不带企业过滤的协作者查询，租户标识取表行自身 enterprise_id。
// 结构写路径继续使用 memberPrincipal + TableByID，不放宽。
func (service *Service) tableForReadTenancy(
	ctx context.Context,
	principal *commonv1.Principal,
	tableID string,
) (base.StructureTable, error) {
	if principal.GetEnterpriseId() == "" {
		return service.repository.TableByIDCollaborator(ctx, tableID)
	}
	return service.repository.TableByID(ctx, principal.GetEnterpriseId(), tableID)
}

func (service *Service) memberPrincipal(
	ctx context.Context,
) (*commonv1.Principal, iamv1.MemberRole, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, err
	}
	if service == nil || service.roles == nil {
		return nil, iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, deal.RPCError(deal.CodePermissionDenied)
	}
	role, err := service.roles.Role(ctx, principal.GetEnterpriseId(), principal.GetUserId())
	if err != nil {
		return nil, iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, err
	}
	if role == iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED {
		return nil, role, deal.RPCError(deal.CodePermissionDenied)
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
		return nil, role, deal.RPCError(deal.CodePermissionDenied)
	}
	return principal, role, nil
}

func mapStructureError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, base.ErrStructureNotFound) {
		return deal.RPCError(deal.CodeDeletedResource)
	}
	var graphErr *calc.DependencyCycleError
	if errors.As(err, &graphErr) {
		return deal.RPCError(deal.CodeInvalidArgument)
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
		Id:          field.ID,
		TableId:     field.TableID,
		Name:        field.Name,
		Type:        fieldType,
		ConfigMap:   configMap,
		IsPrimary:   field.IsPrimary,
		Position:    int32(field.Position),
		Description: field.Description,
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
