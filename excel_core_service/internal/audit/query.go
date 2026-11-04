// audit/query.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package audit

import (
	"context"

	auditv1 "excel/api/audit/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// Query returns audit rows for the caller's enterprise.
func (service *Service) Query(
	ctx context.Context,
	request *auditv1.QueryRequest,
) (*auditv1.QueryResponse, error) {
	principal, err := service.requireAuditor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	fromTime, toTime, err := service.timeWindow(request.GetFromMs(), request.GetToMs())
	if err != nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	cursor, err := parseAuditCursor(request.GetCursor())
	if err != nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	limit := int(request.GetLimit())
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	records, err := service.repository.List(ctx, base.AuditQuery{
		EnterpriseID: principal.GetEnterpriseId(),
		Operator:     request.GetOperator(),
		From:         fromTime,
		To:           toTime,
		Cursor:       cursor,
		Limit:        limit + 1,
	})
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	response := &auditv1.QueryResponse{
		AuditList: make([]*auditv1.AuditEvent, 0, len(records)),
		HasMore:   hasMore,
	}
	for _, record := range records {
		response.AuditList = append(response.AuditList, protoAuditEvent(record))
	}
	if hasMore && len(records) > 0 {
		response.NextCursor = auditCursor(records[len(records)-1].ID)
	}
	return response, nil
}
