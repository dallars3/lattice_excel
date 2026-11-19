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

// CalcJobBackfill marks a trigger-less backfill job（N5 结构域回填种子）。
// AfterRecordID 为上一页最后一条记录 id，空串表示从表头开始。
type CalcJobBackfill struct {
	AfterRecordID string `json:"after_record_id,omitempty"`
}

// CalcJobTrigger is the triggering record cell of a write-chain calc job;
// 回填作业无触发记录，两字段为空串（线上仍携带空 trigger 对象）。
type CalcJobTrigger struct {
	RecordID string `json:"record_id"`
	FieldID  string `json:"field_id"`
}

// CalcJobPayload is the single calc.job wire contract（双轴评审第 2 项：载荷
// 契约单源）。生产者（records 写链、structure 回填种子、calc 回填链）与消费者
// （consumeCalcJob）共用本类型与 NewCalcJobPayload，两端字段一字不差——线上键
// 为 job_id/table_id/field_id_list/trigger/ backfill，任何漂移由
// TestCalcJobPayloadWireShapeIsStable 红灯拦截。
type CalcJobPayload struct {
	JobID       string           `json:"job_id"`
	TableID     string           `json:"table_id"`
	FieldIDList []string         `json:"field_id_list"`
	Trigger     CalcJobTrigger   `json:"trigger"`
	Backfill    *CalcJobBackfill `json:"backfill,omitempty"`
}

// NewCalcJobPayload is the only calc.job payload constructor：fieldIDList 复制
// 入参防调用方复用底层数组；backfill 传 nil 即普通写链作业（线上不带 backfill
// 键），传 &CalcJobBackfill{...} 即回填作业（种子/链式）。
func NewCalcJobPayload(
	jobID, tableID string,
	fieldIDList []string,
	triggerRecordID, triggerFieldID string,
	backfill *CalcJobBackfill,
) CalcJobPayload {
	return CalcJobPayload{
		JobID:       jobID,
		TableID:     tableID,
		FieldIDList: append([]string(nil), fieldIDList...),
		Trigger:     CalcJobTrigger{RecordID: triggerRecordID, FieldID: triggerFieldID},
		Backfill:    backfill,
	}
}

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
//
// 生产实现（records.CalcRecomputeAdapter）的 WriteComputed 是**纯 no-op 写通知**：
// calc 作业对每个计算格已在 processCalcJob 事务内 SaveMaterialized（权威持久化，
// 与 record.changed 广播和作业收尾原子提交），适配器若提前落库会把「物化行可见」
// 提到作业提交之前，与删除字段级联/转出清理产生 resurrect 竞态（N5 票 03）。
// 保留该接口方法而非剥离：calc 单元测试的 fake 依赖它做两件实事——观察写入
// 顺序/值（拓扑序、错误哨兵断言）与把已算值反馈进行上下文供下游公式连算；
// 剥离需要重设计整套重算测试装置，收益不成比例。新实现应同样把它视为通知缝，
// 持久化必须走作业事务。
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

// BackfillRecordPager pages a table's active record ids for trigger-less
// backfill jobs（N5）：结构域新建/复制/转类型公式或引用字段后，作业按页物化
// 全表存量记录。afterRecordID 为上一页最后一条记录的 id（空串=从头开始），
// limit 为本页上限（消费方多取一条探测页边界）。
type BackfillRecordPager interface {
	RecordIDsAfter(
		ctx context.Context,
		tableID, afterRecordID string,
		limit int,
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
