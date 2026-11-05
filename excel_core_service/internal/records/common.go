// records/common.go：记录读域共享接口、身份解析与 protobuf 投影。
package records

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

const (
	rowScopeAll = "ROW_SCOPE_ALL"
	rowScopeOwn = "ROW_SCOPE_OWN"
)

// SchemaOrViewAccessor is the records module's read-only structure seam.
type SchemaOrViewAccessor interface {
	Table(ctx context.Context, tableID string) (*structurev1.Table, error)
	Fields(ctx context.Context, tableID string) ([]*structurev1.Field, error)
	View(
		ctx context.Context,
		viewID string,
	) (*structurev1.View, *structurev1.ViewConfig, error)
}

// RowScopeProvider resolves target-row scope for relation and lookup reads.
type RowScopeProvider interface {
	RowScope(
		ctx context.Context,
		principal *commonv1.Principal,
		baseID string,
	) (string, error)
}

// FormulaValueProvider supplies materialized formula values. A missing
// materialization is represented by ok=false and renders as an empty cell.
type FormulaValueProvider interface {
	Value(
		ctx context.Context,
		recordID, fieldID string,
	) (*commonv1.Value, bool, error)
}

// CommentCountProvider resolves unsolved top-level comment counts per record.
type CommentCountProvider interface {
	UnsolvedCounts(ctx context.Context, recordIDs []string) (map[string]int32, error)
}

// Service implements the RecordsService read and write surface.
type Service struct {
	recordsv1.UnimplementedRecordsServiceServer

	repository *base.RecordsRepository
	structure  SchemaOrViewAccessor
	scopes     RowScopeProvider
	formulas   FormulaValueProvider
	comments   CommentCountProvider
	outbox     EventOutbox
	bases      BaseContextProvider
	evaluator  FormulaEvaluator
	computed   ComputedWriter
	now        func() time.Time
	location   *time.Location
}

// NewService constructs the records service with explicit external seams.
func NewService(
	repository *base.RecordsRepository,
	structureView SchemaOrViewAccessor,
	scopes RowScopeProvider,
	formulas FormulaValueProvider,
	now func() time.Time,
	comments CommentCountProvider,
	options ...WriterOptions,
) *Service {
	if now == nil {
		now = time.Now
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	service := &Service{
		repository: repository,
		structure:  structureView,
		scopes:     scopes,
		formulas:   formulas,
		comments:   comments,
		now:        now,
		location:   location,
	}
	for _, option := range options {
		service.outbox = option.Outbox
		service.bases = option.Bases
		service.evaluator = option.Evaluator
		service.computed = option.ComputedWriter
	}
	return service
}

// Register installs the records service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *Service) {
	recordsv1.RegisterRecordsServiceServer(registrar, service)
}

func rowScopeFromContext(ctx context.Context) (string, error) {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", missingRowScopeError()
	}
	values := incoming.Get("x-row-scope")
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return "", missingRowScopeError()
	}
	return normalizeRowScope(values[0])
}

func missingRowScopeError() error {
	return status.Error(codes.InvalidArgument, "x-row-scope metadata is required")
}

func normalizeRowScope(value string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	switch normalized {
	case rowScopeAll, rowScopeOwn:
		return normalized, nil
	default:
		return "", status.Errorf(codes.InvalidArgument, "unsupported x-row-scope %q", value)
	}
}

func mapRecordError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, base.ErrRecordNotFound) ||
		errors.Is(err, base.ErrStructureNotFound) {
		return deal.RPCError(deal.CodeDeletedResource)
	}
	return err
}

func (service *Service) tableForBase(
	ctx context.Context,
	baseID, tableID string,
) (*structurev1.Table, error) {
	if service == nil || service.structure == nil {
		return nil, errors.New("records structure accessor is not configured")
	}
	table, err := service.structure.Table(ctx, tableID)
	if err != nil {
		return nil, mapRecordError(err)
	}
	if table == nil ||
		table.GetId() != tableID ||
		table.GetBaseId() != baseID {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	return table, nil
}

func fieldByID(fields []*structurev1.Field) map[string]*structurev1.Field {
	result := make(map[string]*structurev1.Field, len(fields))
	for _, field := range fields {
		if field != nil {
			result[field.GetId()] = field
		}
	}
	return result
}

func recordToProto(record base.Record) *recordsv1.Record {
	cells := make(map[string]*commonv1.Value, len(record.Cells))
	for fieldID, value := range record.Cells {
		if value != nil {
			cells[fieldID] = value
		}
	}
	return &recordsv1.Record{
		Id:        record.ID,
		TableId:   record.TableID,
		CellsMap:  cells,
		CreatedBy: record.CreatedBy,
		CreatedAt: record.CreatedAt,
		Version:   record.Version,
	}
}

func dataError(format string, arguments ...any) error {
	return status.Error(codes.InvalidArgument, fmt.Sprintf(format, arguments...))
}
