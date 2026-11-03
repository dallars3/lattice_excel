package records_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"excel-libs/eventbus"
	calcv1 "excel/api/calc/v1"
	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

type writeEvaluator struct {
	request *calcv1.EvaluateRequest
}

func (evaluator *writeEvaluator) Evaluate(
	_ context.Context,
	request *calcv1.EvaluateRequest,
) (*calcv1.EvaluateResponse, error) {
	evaluator.request = request
	return &calcv1.EvaluateResponse{
		ResultList: []*calcv1.EvalResult{{
			Value: intCell(14),
		}},
	}, nil
}

type failingWriteEvaluator struct{}

func (failingWriteEvaluator) Evaluate(
	context.Context,
	*calcv1.EvaluateRequest,
) (*calcv1.EvaluateResponse, error) {
	return nil, errors.New("formula engine unavailable")
}

func TestUpdateCellEvaluatesSameRowFormulaMaterializesAndPublishesChanged(t *testing.T) {
	formulaConfig, err := structpb.NewStruct(map[string]any{
		"expression": "{Score}*2",
	})
	if err != nil {
		t.Fatalf("formula config: %v", err)
	}
	access := writeStructureAccessor(
		&structurev1.Field{
			Id:        "fld_score",
			TableId:   recordsTestTableID,
			Name:      "Score",
			Type:      structurev1.FieldType_FIELD_TYPE_NUMBER,
			IsPrimary: true,
		},
		&structurev1.Field{
			Id:        "fld_double",
			TableId:   recordsTestTableID,
			Name:      "Double",
			Type:      structurev1.FieldType_FIELD_TYPE_FORMULA,
			ConfigMap: formulaConfig,
		},
	)
	evaluator := &writeEvaluator{}
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		evaluator,
	)
	seedRecord(
		t,
		harness.db,
		"rec_formula_write",
		recordsTestTableID,
		recordsTestUserID,
		3,
		map[string]*commonv1.Value{"fld_score": intCell(2)},
	)
	subscription, err := harness.memoryBus.Subscribe(
		context.Background(),
		eventbus.SubscribeOptions{
			Topic: "record.changed",
			Mode:  eventbus.DeliveryModePubSub,
		},
	)
	if err != nil {
		t.Fatalf("subscribe record.changed: %v", err)
	}
	t.Cleanup(func() { _ = subscription.Close() })

	response, err := harness.client.UpdateCell(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.UpdateCellRequest{
			RecordId:      "rec_formula_write",
			FieldId:       "fld_score",
			Value:         intCell(7),
			ExpectVersion: 3,
		},
	)
	if err != nil {
		t.Fatalf("UpdateCell() error = %v", err)
	}
	if response.GetValue().GetIntValue() != 7 || response.GetVersion() != 4 {
		t.Fatalf("UpdateCell response = %v, want value 7 version 4", response)
	}

	record, err := base.NewRecordsRepository(harness.db).RecordByID(
		context.Background(),
		"rec_formula_write",
	)
	if err != nil {
		t.Fatalf("RecordByID() error = %v", err)
	}
	if record.Version != 4 ||
		record.Cells["fld_score"].GetIntValue() != 7 ||
		record.Cells["fld_double"].GetIntValue() != 14 {
		t.Fatalf("record after UpdateCell = version:%d cells:%v", record.Version, record.Cells)
	}
	if evaluator.request == nil ||
		len(evaluator.request.GetExpressionList()) != 1 ||
		evaluator.request.GetExpressionList()[0] != "{Score}*2" ||
		evaluator.request.GetRowContextMap().GetFields()["Score"].GetNumberValue() != 7 {
		t.Fatalf("Evaluate request = %v", evaluator.request)
	}
	if len(harness.computedWriter.writes) != 1 ||
		harness.computedWriter.writes[0].FieldID != "fld_double" ||
		harness.computedWriter.writes[0].Value.GetIntValue() != 14 {
		t.Fatalf("computed writes = %+v", harness.computedWriter.writes)
	}

	forwarded, err := harness.forwarder.ForwardOnce(context.Background())
	if err != nil || forwarded != 1 {
		t.Fatalf("ForwardOnce() = %d, %v, want one event", forwarded, err)
	}
	receiveCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	delivery, err := subscription.Receive(receiveCtx)
	if err != nil {
		t.Fatalf("receive record.changed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode record.changed payload: %v", err)
	}
	if delivery.Event.Header.Type != "record.changed" ||
		delivery.Event.Header.BaseID != recordsTestBaseID ||
		payload["record_id"] != "rec_formula_write" ||
		payload["field_id"] != "fld_score" ||
		payload["version"] != float64(4) ||
		payload["operator"] != recordsTestUserID ||
		payload["table_id"] != recordsTestTableID {
		t.Fatalf("record.changed event = header:%+v payload:%v", delivery.Event.Header, payload)
	}
}

func TestUpdateCellFormulaFailureKeepsOriginalRowAndReturnsErrorData(t *testing.T) {
	formulaConfig, err := structpb.NewStruct(map[string]any{
		"expression": "{Score}*2",
	})
	if err != nil {
		t.Fatalf("formula config: %v", err)
	}
	access := writeStructureAccessor(
		&structurev1.Field{
			Id:        "fld_score",
			TableId:   recordsTestTableID,
			Name:      "Score",
			Type:      structurev1.FieldType_FIELD_TYPE_NUMBER,
			IsPrimary: true,
		},
		&structurev1.Field{
			Id:        "fld_double",
			TableId:   recordsTestTableID,
			Name:      "Double",
			Type:      structurev1.FieldType_FIELD_TYPE_FORMULA,
			ConfigMap: formulaConfig,
		},
	)
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		failingWriteEvaluator{},
	)
	seedRecord(
		t,
		harness.db,
		"rec_formula_failure",
		recordsTestTableID,
		recordsTestUserID,
		2,
		map[string]*commonv1.Value{
			"fld_score":  intCell(3),
			"fld_double": intCell(6),
		},
	)
	_, err = harness.client.UpdateCell(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.UpdateCellRequest{
			RecordId:      "rec_formula_failure",
			FieldId:       "fld_score",
			Value:         intCell(7),
			ExpectVersion: 2,
		},
	)
	requireRPCErrorCode(t, err, deal.CodeInvalidArgument)
	data := rpcErrorData(t, err)
	if data["formula_errors"] == nil {
		t.Fatalf("formula error data = %v", data)
	}
	record, err := base.NewRecordsRepository(harness.db).RecordByID(
		context.Background(),
		"rec_formula_failure",
	)
	if err != nil {
		t.Fatalf("RecordByID() error = %v", err)
	}
	if record.Version != 2 ||
		record.Cells["fld_score"].GetIntValue() != 3 ||
		record.Cells["fld_double"].GetIntValue() != 6 {
		t.Fatalf("row after formula failure = %+v", record)
	}
	assertDatabaseCount(t, harness.db, "excel_records_histories", 0)
	assertDatabaseCount(t, harness.db, "excel_bus_outbox", 0)
}

func TestUpdateCellPersistsComputedValueInBusinessTransaction(t *testing.T) {
	formulaConfig, err := structpb.NewStruct(map[string]any{
		"expression": "{Score}*2",
	})
	if err != nil {
		t.Fatalf("formula config: %v", err)
	}
	access := writeStructureAccessor(
		&structurev1.Field{
			Id:        "fld_score",
			TableId:   recordsTestTableID,
			Name:      "Score",
			Type:      structurev1.FieldType_FIELD_TYPE_NUMBER,
			IsPrimary: true,
		},
		&structurev1.Field{
			Id:        "fld_double",
			TableId:   recordsTestTableID,
			Name:      "Double",
			Type:      structurev1.FieldType_FIELD_TYPE_FORMULA,
			ConfigMap: formulaConfig,
		},
	)
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		&writeEvaluator{},
	)
	seedRecord(
		t,
		harness.db,
		"rec_formula_persist",
		recordsTestTableID,
		recordsTestUserID,
		1,
		map[string]*commonv1.Value{"fld_score": intCell(2)},
	)
	_, err = harness.client.UpdateCell(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.UpdateCellRequest{
			RecordId:      "rec_formula_persist",
			FieldId:       "fld_score",
			Value:         intCell(7),
			ExpectVersion: 1,
		},
	)
	if err != nil {
		t.Fatalf("UpdateCell() error = %v", err)
	}
	var valueJSON []byte
	if err := harness.db.QueryRowContext(
		context.Background(),
		`SELECT value_json FROM excel_calc_materialized
		 WHERE record_id = ? AND field_id = ?`,
		"rec_formula_persist",
		"fld_double",
	).Scan(&valueJSON); err != nil {
		t.Fatalf("query materialized formula: %v", err)
	}
	value, err := base.DecodeRecordCell(valueJSON)
	if err != nil {
		t.Fatalf("decode materialized formula: %v", err)
	}
	if value.GetIntValue() != 14 {
		t.Fatalf("materialized formula = %v, want 14", value)
	}
}
