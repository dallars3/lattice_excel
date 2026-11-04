package records_test

import (
	"context"
	"database/sql"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/internal/records"
)

type writeBaseContextProvider struct {
	baseID       string
	enterpriseID string
	tableIDs     []string
}

func (provider *writeBaseContextProvider) ContextByTable(
	_ context.Context,
	tableID string,
) (string, string, error) {
	provider.tableIDs = append(provider.tableIDs, tableID)
	return provider.baseID, provider.enterpriseID, nil
}

type observedComputedWrite struct {
	RecordID string
	FieldID  string
	Value    *commonv1.Value
}

type writeComputedWriter struct {
	mu       sync.Mutex
	writes   []observedComputedWrite
	delegate records.ComputedWriter
}

func (writer *writeComputedWriter) WriteComputed(
	ctx context.Context,
	recordID, fieldID string,
	value *commonv1.Value,
) error {
	writer.mu.Lock()
	writer.writes = append(writer.writes, observedComputedWrite{
		RecordID: recordID,
		FieldID:  fieldID,
		Value:    value,
	})
	delegate := writer.delegate
	writer.mu.Unlock()
	if delegate != nil {
		return delegate.WriteComputed(ctx, recordID, fieldID, value)
	}
	return nil
}

type writeHarness struct {
	client         recordsv1.RecordsServiceClient
	db             *sql.DB
	now            time.Time
	memoryBus      *eventbus.MemoryBus
	forwarder      *bus.Forwarder
	computedWriter *writeComputedWriter
	baseContext    *writeBaseContextProvider
}

func newWriteHarness(
	t *testing.T,
	access records.SchemaOrViewAccessor,
	scopes records.RowScopeProvider,
	evaluator records.FormulaEvaluator,
	computedWriters ...records.ComputedWriter,
) *writeHarness {
	t.Helper()
	db, err := base.OpenDB(recordsTestMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetWriteData(t, db)
	t.Cleanup(func() { resetWriteData(t, db) })

	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	memoryBus := eventbus.NewMemoryBus()
	busStore := base.NewBusStore(db)
	computedWriter := &writeComputedWriter{
		delegate: records.NewCalcComputedWriter(base.NewCalcRepository(db)),
	}
	var writer records.ComputedWriter = computedWriter
	if len(computedWriters) > 0 && computedWriters[0] != nil {
		writer = computedWriters[0]
	}
	baseContext := &writeBaseContextProvider{
		baseID:       recordsTestBaseID,
		enterpriseID: "01J00000000000000000001006",
	}
	service := records.NewService(
		base.NewRecordsRepository(db),
		access,
		scopes,
		nil,
		func() time.Time { return now },
		nil,
		records.WriterOptions{
			Outbox:         bus.NewOutbox(busStore),
			Bases:          baseContext,
			Evaluator:      evaluator,
			ComputedWriter: writer,
		},
	)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	records.Register(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///records-write-bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	return &writeHarness{
		client:         recordsv1.NewRecordsServiceClient(connection),
		db:             db,
		now:            now,
		memoryBus:      memoryBus,
		forwarder:      bus.NewForwarder(busStore, memoryBus, bus.ForwarderConfig{}),
		computedWriter: computedWriter,
		baseContext:    baseContext,
	}
}

func writeStructureAccessor(
	fields ...*structurev1.Field,
) *writeAccessor {
	return &writeAccessor{
		table: &structurev1.Table{
			Id:     recordsTestTableID,
			BaseId: recordsTestBaseID,
		},
		fields: fields,
	}
}

type writeAccessor struct {
	table          *structurev1.Table
	fields         []*structurev1.Field
	crossTableRefs bool
}

func (accessor *writeAccessor) Table(
	context.Context,
	string,
) (*structurev1.Table, error) {
	return accessor.table, nil
}

func (accessor *writeAccessor) Fields(
	context.Context,
	string,
) ([]*structurev1.Field, error) {
	return accessor.fields, nil
}

func (accessor *writeAccessor) View(
	context.Context,
	string,
) (*structurev1.View, *structurev1.ViewConfig, error) {
	return nil, nil, nil
}

func (accessor *writeAccessor) Field(
	_ context.Context,
	fieldID string,
) (*structurev1.Field, error) {
	for _, field := range accessor.fields {
		if field.GetId() == fieldID {
			return field, nil
		}
	}
	return nil, base.ErrStructureNotFound
}

func (accessor *writeAccessor) HasCrossTableDependents(
	context.Context,
	string,
	string,
	string,
) (bool, error) {
	return accessor.crossTableRefs, nil
}

func resetWriteData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_records_oplog",
		"DELETE FROM excel_records_histories",
		"DELETE FROM excel_records",
		"DELETE FROM excel_bus_outbox",
		"DELETE FROM excel_calc_materialized",
		"DELETE FROM excel_calc_jobs",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset write data with %q: %v", statement, err)
		}
	}
}
