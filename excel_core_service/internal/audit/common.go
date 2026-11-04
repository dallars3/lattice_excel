// audit/common.go：模块内可复用逻辑归口。
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	auditv1 "excel/api/audit/v1"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

const (
	// AuditRetention is the default lookback for queries and CSV exports.
	AuditRetention = 180 * 24 * time.Hour
	// AuditConsumerGroup is the durable EventBus consumer group for audit.
	AuditConsumerGroup = "core-audit"
)

// RoleProvider is the read-only IAM membership seam.
type RoleProvider interface {
	Role(ctx context.Context, enterpriseID, userID string) (iamv1.MemberRole, error)
}

// Service implements the action-level audit RPC surface and event consumers.
type Service struct {
	auditv1.UnimplementedAuditServiceServer

	repository *base.AuditRepository
	roles      RoleProvider
	now        func() time.Time
}

// NewService constructs the audit service with its persistence and role seams.
func NewService(repository *base.AuditRepository, roles RoleProvider) *Service {
	return &Service{
		repository: repository,
		roles:      roles,
		now:        time.Now,
	}
}

// Register installs the audit service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *Service) {
	auditv1.RegisterAuditServiceServer(registrar, service)
}

func (service *Service) requireAuditor(ctx context.Context) (*commonv1.Principal, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil || principal.GetEnterpriseId() == "" {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if service == nil || service.repository == nil || service.roles == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	role, err := service.roles.Role(
		ctx,
		principal.GetEnterpriseId(),
		principal.GetUserId(),
	)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if role != iamv1.MemberRole_MEMBER_ROLE_OWNER &&
		role != iamv1.MemberRole_MEMBER_ROLE_MANAGER {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	return principal, nil
}

func (service *Service) appendRequest(
	ctx context.Context,
	request *auditv1.AppendRequest,
) error {
	if service.repository == nil || request == nil {
		return deal.RPCError(deal.CodeInvalidArgument)
	}
	if request.GetEventId() == "" ||
		request.GetAction() == auditv1.AuditAction_AUDIT_ACTION_UNSPECIFIED {
		return deal.RPCError(deal.CodeInvalidArgument)
	}
	return service.appendAudit(
		ctx,
		request,
		detailEnterpriseID(request.GetDetailMap()),
		service.now().UTC(),
	)
}

func (service *Service) appendAudit(
	ctx context.Context,
	request *auditv1.AppendRequest,
	enterpriseID string,
	occurredAt time.Time,
) error {
	if service.repository == nil || request == nil {
		return deal.RPCError(deal.CodeInvalidArgument)
	}
	if request.GetEventId() == "" ||
		enterpriseID == "" ||
		request.GetAction() == auditv1.AuditAction_AUDIT_ACTION_UNSPECIFIED {
		return deal.RPCError(deal.CodeInvalidArgument)
	}
	if len(request.GetEventId()) > 64 ||
		len(enterpriseID) > 26 ||
		len(request.GetOperator()) > 128 ||
		len(request.GetTarget()) > 256 ||
		len(request.GetBaseId()) > 26 ||
		len(request.GetTableId()) > 26 {
		return deal.RPCError(deal.CodeInvalidArgument)
	}
	encodedDetail, err := marshalDetail(request.GetDetailMap())
	if err != nil {
		return deal.RPCError(deal.CodeInvalidArgument)
	}
	record := base.AuditEventRecord{
		EventID:      request.GetEventId(),
		EnterpriseID: enterpriseID,
		Action:       request.GetAction().String(),
		Operator:     request.GetOperator(),
		Target:       request.GetTarget(),
		BaseID:       request.GetBaseId(),
		TableID:      request.GetTableId(),
		Detail:       encodedDetail,
		OccurredAt:   occurredAt.UTC(),
	}
	err = service.repository.Append(ctx, record)
	if err == nil || errors.Is(err, base.ErrAuditEventExists) {
		return nil
	}
	return deal.RPCError(deal.CodeServiceUnavailable)
}

func marshalDetail(detail *structpb.Struct) ([]byte, error) {
	if detail == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(detail.AsMap())
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func decodeDetail(detail []byte) *structpb.Struct {
	if len(detail) == 0 {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal(detail, &value); err != nil {
		return nil
	}
	result, _ := structpb.NewStruct(value)
	return result
}

func detailEnterpriseID(detail *structpb.Struct) string {
	if detail == nil {
		return ""
	}
	return detail.GetFields()["enterprise_id"].GetStringValue()
}

func auditCursor(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func parseAuditCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil || value <= 0 {
		return 0, errors.New("invalid audit cursor")
	}
	return value, nil
}

func protoAuditEvent(record base.AuditEventRecord) *auditv1.AuditEvent {
	return &auditv1.AuditEvent{
		Id:        strconv.FormatInt(record.ID, 10),
		Action:    auditAction(record.Action),
		Operator:  record.Operator,
		Target:    record.Target,
		BaseId:    record.BaseID,
		TableId:   record.TableID,
		DetailMap: decodeDetail(record.Detail),
		CreatedAt: record.OccurredAt.UTC().UnixMilli(),
	}
}

func auditAction(value string) auditv1.AuditAction {
	number, ok := auditv1.AuditAction_value[value]
	if !ok {
		return auditv1.AuditAction_AUDIT_ACTION_UNSPECIFIED
	}
	return auditv1.AuditAction(number)
}

func (service *Service) timeWindow(
	fromMS, toMS int64,
) (time.Time, time.Time, error) {
	now := service.now().UTC()
	toTime := now
	if toMS > 0 {
		toTime = time.UnixMilli(toMS).UTC()
	}
	fromTime := toTime.Add(-AuditRetention)
	if fromMS > 0 {
		fromTime = time.UnixMilli(fromMS).UTC()
	}
	if fromTime.After(toTime) {
		return time.Time{}, time.Time{}, errors.New("from_ms is after to_ms")
	}
	return fromTime, toTime, nil
}
