package records_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
)

func TestUpdateCellDispatchesCrossTableCalcJob(t *testing.T) {
	access := writeStructureAccessor(&structurev1.Field{
		Id:        "fld_num",
		TableId:   recordsTestTableID,
		Name:      "Num",
		Type:      structurev1.FieldType_FIELD_TYPE_NUMBER,
		IsPrimary: true,
	})
	access.crossTableRefs = true
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		nil,
	)
	seedRecord(
		t,
		harness.db,
		"rec_calc_job",
		recordsTestTableID,
		recordsTestUserID,
		1,
		map[string]*commonv1.Value{"fld_num": intCell(1)},
	)
	subscription, err := harness.memoryBus.Subscribe(
		context.Background(),
		eventbus.SubscribeOptions{
			Topic: "calc.job",
			Mode:  eventbus.DeliveryModePubSub,
		},
	)
	if err != nil {
		t.Fatalf("subscribe calc.job: %v", err)
	}
	t.Cleanup(func() { _ = subscription.Close() })

	_, err = harness.client.UpdateCell(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.UpdateCellRequest{
			RecordId:      "rec_calc_job",
			FieldId:       "fld_num",
			Value:         intCell(2),
			ExpectVersion: 1,
		},
	)
	if err != nil {
		t.Fatalf("UpdateCell() error = %v", err)
	}
	forwarded, err := harness.forwarder.ForwardOnce(context.Background())
	if err != nil || forwarded != 2 {
		t.Fatalf("ForwardOnce() = %d, %v, want changed and calc.job events", forwarded, err)
	}
	receiveCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	delivery, err := subscription.Receive(receiveCtx)
	if err != nil {
		t.Fatalf("receive calc.job: %v", err)
	}
	var payload struct {
		JobID       string   `json:"job_id"`
		TableID     string   `json:"table_id"`
		FieldIDList []string `json:"field_id_list"`
		Trigger     struct {
			RecordID string `json:"record_id"`
			FieldID  string `json:"field_id"`
		} `json:"trigger"`
	}
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode calc.job payload: %v", err)
	}
	if delivery.Event.Header.Type != "calc.job" ||
		len(payload.JobID) != 26 ||
		payload.TableID != recordsTestTableID ||
		len(payload.FieldIDList) != 1 ||
		payload.FieldIDList[0] != "fld_num" ||
		payload.Trigger.RecordID != "rec_calc_job" ||
		payload.Trigger.FieldID != "fld_num" {
		t.Fatalf("calc.job event = header:%+v payload:%+v", delivery.Event.Header, payload)
	}
}
