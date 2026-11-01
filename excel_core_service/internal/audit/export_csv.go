// audit/export_csv.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package audit

import (
	"bytes"
	"encoding/csv"
	"strconv"

	"google.golang.org/grpc"

	"excel-libs/idgen"
	auditv1 "excel/api/audit/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// ExportCSV streams enterprise audit rows as CSV.
func (service *Service) ExportCSV(
	request *auditv1.ExportCSVRequest,
	stream grpc.ServerStreamingServer[auditv1.ExportCSVResponse],
) error {
	principal, err := service.requireAuditor(stream.Context())
	if err != nil {
		return err
	}
	if request == nil {
		return rpcError(deal.CodeInvalidArgument)
	}
	fromTime, toTime, err := service.timeWindow(request.GetFromMs(), request.GetToMs())
	if err != nil {
		return rpcError(deal.CodeInvalidArgument)
	}
	exportEventID, err := idgen.New()
	if err != nil {
		return rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.Append(stream.Context(), base.AuditEventRecord{
		EventID:      exportEventID,
		EnterpriseID: principal.GetEnterpriseId(),
		Action:       auditv1.AuditAction_AUDIT_ACTION_DATA_EXPORT.String(),
		Operator:     principal.GetUserId(),
		Target:       "audit_csv",
		Detail:       []byte(`{"format":"csv","scope":"enterprise"}`),
		OccurredAt:   service.now().UTC(),
	}); err != nil {
		return rpcError(deal.CodeServiceUnavailable)
	}

	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{
		"id",
		"event_id",
		"action",
		"operator",
		"target",
		"base_id",
		"table_id",
		"detail",
		"occurred_at",
	}); err != nil {
		return rpcError(deal.CodeServiceUnavailable)
	}
	writer.Flush()
	if err := stream.Send(&auditv1.ExportCSVResponse{Chunk: output.Bytes()}); err != nil {
		return err
	}

	var cursor int64
	for {
		records, err := service.repository.List(stream.Context(), base.AuditQuery{
			EnterpriseID: principal.GetEnterpriseId(),
			Operator:     request.GetOperator(),
			From:         fromTime,
			To:           toTime,
			Cursor:       cursor,
			Limit:        100,
		})
		if err != nil {
			return rpcError(deal.CodeServiceUnavailable)
		}
		if len(records) == 0 {
			return nil
		}
		output.Reset()
		writer = csv.NewWriter(&output)
		for _, record := range records {
			if err := writer.Write([]string{
				strconv.FormatInt(record.ID, 10),
				record.EventID,
				record.Action,
				record.Operator,
				record.Target,
				record.BaseID,
				record.TableID,
				string(record.Detail),
				record.OccurredAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
			}); err != nil {
				return rpcError(deal.CodeServiceUnavailable)
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return rpcError(deal.CodeServiceUnavailable)
		}
		if err := stream.Send(&auditv1.ExportCSVResponse{Chunk: output.Bytes()}); err != nil {
			return err
		}
		cursor = records[len(records)-1].ID
		if len(records) < 100 {
			return nil
		}
	}
}
