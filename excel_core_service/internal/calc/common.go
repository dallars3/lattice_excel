// calc/common.go：模块内可复用逻辑归口。
package calc

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc"

	calcapi "excel/api/calc/v1"
	commonv1 "excel/api/common/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
)

const calcConsumerGroup = "core-calc"

// RelationTarget identifies the target table and row identity field of a
// one-direction relation.
type RelationTarget struct {
	TableID string
	FieldID string
}

// RelationStep is one edge in a relation/lookup chain.
type RelationStep struct {
	TableID string
	FieldID string
}

// LookupSource describes the relation field and target field read by a lookup.
type LookupSource struct {
	RelationFieldID string
	RelationChain   []RelationStep
	TargetFieldID   string
}

// FieldDefinition is the calc-visible projection of a structure field.
type FieldDefinition struct {
	ID          string
	Name        string
	TableID     string
	Type        string
	Formula     string
	FormulaRefs []string
	Relation    *RelationTarget
	Lookup      *LookupSource
}

// SchemaProvider supplies the complete cross-table field graph during save
// validation and recompute planning. The 08 structure module implements it.
type SchemaProvider interface {
	Fields(ctx context.Context) ([]FieldDefinition, error)
}

// CellMaterializer is implemented by 12/records to write authoritative cell
// values. Recompute tests use a fake to observe writes.
type CellMaterializer interface {
	WriteComputed(ctx context.Context, recordID, fieldID string, value *commonv1.Value) error
}

// RowContextReader lets recompute read the source row without calc owning
// records-module SQL. Implementations may provide both seams on one adapter.
type RowContextReader interface {
	ReadRowContext(ctx context.Context, tableID, recordID string) (map[string]*commonv1.Value, error)
}

// RelationRecordFinder resolves source rows through one relation field. The
// fieldID is the relation field in tableID and targetRecordID is the changed
// row in that relation's target table.
type RelationRecordFinder interface {
	RowsReferencing(
		ctx context.Context,
		tableID, fieldID, targetRecordID string,
	) ([]string, error)
}

// Service implements the calc RPC surface and calc.job consumer.
type Service struct {
	calcapi.UnimplementedCalcServiceServer

	repository   *base.CalcRepository
	schema       SchemaProvider
	materializer CellMaterializer
	outbox       *bus.Outbox
	now          func() time.Time
	location     *time.Location
}

// NewService constructs the calc service with injectable clock and persistence
// seams. Nil schema/materializer paths return explicit errors when used.
func NewService(
	repository *base.CalcRepository,
	schema SchemaProvider,
	materializer CellMaterializer,
	outbox *bus.Outbox,
	clock func() time.Time,
) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{
		repository:   repository,
		schema:       schema,
		materializer: materializer,
		outbox:       outbox,
		now:          clock,
		location:     shanghaiLocation(),
	}
}

// Register installs the calc service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *Service) {
	calcapi.RegisterCalcServiceServer(registrar, service)
}

var (
	shanghaiOnce          sync.Once
	shanghaiLocationValue *time.Location
)

func shanghaiLocation() *time.Location {
	shanghaiOnce.Do(func() {
		location, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			location = time.FixedZone("Asia/Shanghai", 8*60*60)
		}
		shanghaiLocationValue = location
	})
	return shanghaiLocationValue
}

// failJob marks a job ERROR after a failed recompute step; the mark itself is
// best-effort——标记失败仅记 ERROR 日志（作业可能停留在 CALC），不静默吞错。
func (service *Service) failJob(ctx context.Context, jobID string) {
	if err := service.repository.FailJob(ctx, jobID); err != nil {
		slog.Error("calc: mark job ERROR failed", "job_id", jobID, "error", err)
	}
}
