// gateway/audit_routes.go：H-87 ~ H-88 企业操作审计路由。
package gateway

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/grpc/metadata"

	auditv1 "excel/api/audit/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type auditRoutes struct {
	client rpc.AuditRPC
}

// RegisterAuditRoutes binds audit query and CSV export behind SessionRequired.
func RegisterAuditRoutes(pipeline *Pipeline, client rpc.AuditRPC) {
	if pipeline == nil {
		panic("audit routes pipeline is required")
	}
	routes := &auditRoutes{client: client}
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/enterprise/audits",
		http.HandlerFunc(routes.query),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/enterprise/audits/export.csv",
		http.HandlerFunc(routes.exportCSV),
	)
}

func (routes *auditRoutes) query(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	fromMs, ok := auditMillisQuery(request, "from")
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	toMs, ok := auditMillisQuery(request, "to")
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.Query(
		workerRequestContext(request, ""),
		&auditv1.QueryRequest{
			Cursor:   queryString(request, "cursor", ""),
			Limit:    queryLimit(request, 20, 100),
			Operator: queryString(request, "operator", ""),
			FromMs:   fromMs,
			ToMs:     toMs,
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeProtoResult(response, result)
}

func (routes *auditRoutes) exportCSV(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	fromMs, ok := auditMillisQuery(request, "from")
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	toMs, ok := auditMillisQuery(request, "to")
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	stream, err := routes.client.ExportCSV(
		workerRequestContext(request, ""),
		&auditv1.ExportCSVRequest{
			Operator: queryString(request, "operator", ""),
			FromMs:   fromMs,
			ToMs:     toMs,
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	header, err := stream.Header()
	if err != nil {
		return
	}
	if firstHeader(header, "x-file-name") == "" {
		if header == nil {
			header = metadata.MD{}
		}
		header.Set("x-file-mime", "text/csv")
	}
	prepareDownloadHeaders(response, func() (metadata.MD, error) { return header, nil })
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			return
		}
		_, _ = response.Write(chunk.GetChunk())
	}
}

// auditMillisQuery parses a millisecond timestamp query value. An absent value
// returns the zero value without error; a malformed value is rejected.
func auditMillisQuery(request *http.Request, key string) (int64, bool) {
	raw := strings.TrimSpace(request.URL.Query().Get(key))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
