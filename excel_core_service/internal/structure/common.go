// structure/common.go：模块内可复用逻辑归口。
package structure

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"excel-libs/eventbus"
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
// conversion preview counts. targetOptionNames 携带目标选项 id→名 映射
// （文本→单选/多选按名精确匹配）；未装配时回退到受影响记录数。
type FieldConversionImpactCounter interface {
	FieldConversionImpact(
		ctx context.Context,
		tableID, fieldID, targetType string,
		targetOptionNames map[string]string,
	) (affected, cleared int, err error)
}

// FieldConversionApplier migrates stored record values for one confirmed
// conversion（PRD 7.1 规则，同一实现同时服务预览统计，保证预览=确认结果）。
type FieldConversionApplier interface {
	ApplyFieldConversion(
		ctx context.Context,
		tableID, fieldID, targetType string,
		targetOptionNames map[string]string,
	) error
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

// EventOutbox is the transactional event seam implemented by bus.Outbox：
// 表删除的 table.deleted 事件必须与删除同一事务落 outbox（缺陷 #23）。
type EventOutbox interface {
	Enqueue(ctx context.Context, tx *sql.Tx, event eventbus.Event) error
}

// ServiceOptions carries the optional write-side seams of the structure module.
type ServiceOptions struct {
	Outbox EventOutbox
	// CalcRepository 供删除字段级联与转出公式/引用类型在同一写事务内清理
	// 物化列（N5 票 03）。未装配时相关路径 fail-closed 9001。
	CalcRepository *base.CalcRepository
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

	repository     *base.StructureRepository
	roles          RoleProvider
	impact         RecordsImpactCounter
	calcValidator  CalcValidator
	readAccess     ReadAccess
	outbox         EventOutbox
	calcRepository *base.CalcRepository
	schema         *SchemaProvider
	now            func() time.Time
}

// NewService constructs the structure RPC service with explicit external seams.
func NewService(
	repository *base.StructureRepository,
	roles RoleProvider,
	impact RecordsImpactCounter,
	calcValidator CalcValidator,
	now func() time.Time,
	readAccess ReadAccess,
	options ...ServiceOptions,
) *Service {
	if now == nil {
		now = time.Now
	}
	if calcValidator == nil {
		calcValidator = defaultCalcValidator{}
	}
	service := &Service{
		repository:    repository,
		roles:         roles,
		impact:        impact,
		calcValidator: calcValidator,
		readAccess:    readAccess,
		schema:        NewSchemaProvider(repository),
		now:           now,
	}
	for _, option := range options {
		service.outbox = option.Outbox
		service.calcRepository = option.CalcRepository
	}
	return service
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

// authorizeRead 对经协作者路径解析出 Base 的调用方执行 Base 级授权（fail-closed）：
//   - 无企业归属的外部账号（分享链接访客/外部协作者）一律走该 seam；
//   - 有企业归属但数据行经协作者回退解析的账号（其它企业的 Base 协作者，与
//     记录域缺陷 #13 同口径，ADR 0004）同样必须过该 seam——表级/视图级路由
//     不经网关 BaseAuth 兜底。
//
// 内部成员（按自身企业命中的快路径）已在 readPrincipal 通过企业成员校验，
// 不需要重复调用；seam 未装配时 fail-closed（3001）。
func (service *Service) authorizeRead(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
	collaboratorPath bool,
) error {
	if !collaboratorPath {
		return nil
	}
	if service == nil || service.readAccess == nil {
		return deal.RPCError(deal.CodePermissionDenied)
	}
	return service.readAccess.AllowRead(ctx, principal, baseID)
}

// tablesInBaseForRead 列出 Base 内数据表，租户口径 = 数据表所属 Base 的企业
// （spec §4，ADR 0004）：内部成员按企业租户过滤（快路径，行为不变）；外部账号
// 不带企业过滤，租户边界由 Base 级授权承担。有企业归属的调用方按自身企业查空
// 时可能是不归属本企业的外部 Base 协作者：以无过滤查询确认 Base 归属，命中即
// 按外部路径放行（返回 collaboratorPath=true，调用方须过 authorizeRead）；
// 无过滤查询仍为空则维持空列表（Base 不存在或确实无表，行为与修复前一致）。
func (service *Service) tablesInBaseForRead(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
) ([]base.StructureTable, bool, error) {
	if principal.GetEnterpriseId() == "" {
		tables, err := service.repository.TablesByBaseCollaborator(ctx, baseID)
		return tables, true, err
	}
	tables, err := service.repository.TablesByBase(ctx, principal.GetEnterpriseId(), baseID)
	if err != nil || len(tables) > 0 {
		return tables, false, err
	}
	fallback, fallbackErr := service.repository.TablesByBaseCollaborator(ctx, baseID)
	if fallbackErr != nil || len(fallback) == 0 {
		return tables, false, fallbackErr
	}
	return fallback, true, nil
}

// tableForReadTenancy 解析只读接口的数据表行，租户口径 = 数据表所属 Base 的企业
// （spec §4，ADR 0004；与记录域 tableForPrincipalTenancy 同口径）：
//   - 无企业归属的外部账号：不带企业过滤的协作者查询（既有路径）；
//   - 有企业归属的账号：先按调用方企业断言（内部成员快路径，行为不变）；查无
//     此行时调用方可能是不归属本企业的外部 Base 协作者（对测企业账号被加进
//     沙箱 Base 协作，WS 握手探针 TC-090-03..07），改用不带企业过滤的协作者
//     查询放行，租户标识取表行自身 enterprise_id。
//
// 返回值 collaboratorPath 表示数据行不是经调用方自身企业解析得到的（外部路径），
// 调用方必须把它交给 authorizeRead 做 Base 级裁决（fail-closed）。结构写路径
// 继续使用 memberPrincipal + TableByID，不放宽。
func (service *Service) tableForReadTenancy(
	ctx context.Context,
	principal *commonv1.Principal,
	tableID string,
) (base.StructureTable, bool, error) {
	if principal.GetEnterpriseId() == "" {
		table, err := service.repository.TableByIDCollaborator(ctx, tableID)
		return table, true, err
	}
	table, err := service.repository.TableByID(ctx, principal.GetEnterpriseId(), tableID)
	if err == nil {
		return table, false, nil
	}
	if !errors.Is(err, base.ErrStructureNotFound) {
		return base.StructureTable{}, false, err
	}
	table, err = service.repository.TableByIDCollaborator(ctx, tableID)
	return table, true, err
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

// isComputedField 报告字段类型是否属于计算字段族（公式/关联/引用，双轴评审
// 第 3 项单一谓词）：三者的配置/表达式共同构成 calc 依赖图，凡字段写路径
// 需要按「拟议全量字段图」校验依赖（CreateField/DuplicateField/UpdateFieldConfig/
// ConvertField）或按「是否离开计算族」决定物化列清理（ConvertField 转出、
// DeleteField 级联）时一律经本谓词判断，不得散写 FIELD_TYPE_* 字符串比较。
func isComputedField(fieldType string) bool {
	switch fieldType {
	case "FIELD_TYPE_FORMULA", "FIELD_TYPE_RELATION", "FIELD_TYPE_LOOKUP":
		return true
	default:
		return false
	}
}

// needsCalcBackfill 报告字段类型是否需要回填作业：公式字段依赖
// excel_calc_materialized 权威投影（缺失即前端「计算中…」）；引用字段读路径
// 实时物化，入队作业保持三类计算字段回填口径一致（作业对引用字段为空转成功）。
// 关联字段的值是用户数据（链接 id），无物化列，不入队。
func needsCalcBackfill(fieldType string) bool {
	return fieldType == "FIELD_TYPE_FORMULA" || fieldType == "FIELD_TYPE_LOOKUP"
}
