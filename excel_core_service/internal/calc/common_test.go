package calc

import (
	"encoding/json"
	"testing"
)

// 双轴评审第 2 项回归：calc.job 载荷契约单源——生产者（records 写链、structure
// 回填种子、calc 回填链）与消费者共用 calc.NewCalcJobPayload/CalcJobPayload，
// 线上字段一字不差（job_id/table_id/field_id_list/trigger{record_id,field_id}/
// backfill{after_record_id}）。本用例钉死线上形状，任何契约漂移在此红灯。
func TestCalcJobPayloadWireShapeIsStable(t *testing.T) {
	t.Run("写链作业（带触发记录、无 backfill 键）", func(t *testing.T) {
		payload := NewCalcJobPayload("job-w", "table-w", []string{"field-a"}, "record-1", "field-a", nil)
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal write-chain payload: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal write-chain payload: %v", err)
		}
		if decoded["job_id"] != "job-w" || decoded["table_id"] != "table-w" {
			t.Fatalf("write-chain payload identity = %v", decoded)
		}
		fieldList, ok := decoded["field_id_list"].([]any)
		if !ok || len(fieldList) != 1 || fieldList[0] != "field-a" {
			t.Fatalf("write-chain payload field_id_list = %v", decoded["field_id_list"])
		}
		trigger, ok := decoded["trigger"].(map[string]any)
		if !ok || trigger["record_id"] != "record-1" || trigger["field_id"] != "field-a" {
			t.Fatalf("write-chain payload trigger = %v", decoded["trigger"])
		}
		if _, exists := decoded["backfill"]; exists {
			t.Fatalf("write-chain payload must not carry backfill key: %v", decoded)
		}
	})
	t.Run("回填种子作业（无触发记录、backfill 空对象）", func(t *testing.T) {
		payload := NewCalcJobPayload(
			"job-b",
			"table-b",
			[]string{"field-b"},
			"",
			"",
			&CalcJobBackfill{AfterRecordID: ""},
		)
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal backfill payload: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal backfill payload: %v", err)
		}
		if trigger, ok := decoded["trigger"].(map[string]any); !ok ||
			trigger["record_id"] != "" || trigger["field_id"] != "" {
			t.Fatalf("backfill payload trigger = %v, want empty trigger object", decoded["trigger"])
		}
		backfill, ok := decoded["backfill"].(map[string]any)
		if !ok {
			t.Fatalf("backfill payload missing backfill marker: %v", decoded)
		}
		if len(backfill) != 0 {
			t.Fatalf("backfill marker = %v, want empty object（空游标不携带键）", backfill)
		}
	})
	t.Run("回填链作业（游标推进）", func(t *testing.T) {
		payload := NewCalcJobPayload(
			"job-c",
			"table-b",
			[]string{"field-b"},
			"",
			"",
			&CalcJobBackfill{AfterRecordID: "record-prev"},
		)
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal chain payload: %v", err)
		}
		var decoded struct {
			JobID       string           `json:"job_id"`
			TableID     string           `json:"table_id"`
			FieldIDList []string         `json:"field_id_list"`
			Trigger     CalcJobTrigger   `json:"trigger"`
			Backfill    *CalcJobBackfill `json:"backfill"`
		}
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal chain payload: %v", err)
		}
		if decoded.JobID != "job-c" || decoded.TableID != "table-b" {
			t.Fatalf("chain payload identity = %+v", decoded)
		}
		if decoded.Backfill == nil || decoded.Backfill.AfterRecordID != "record-prev" {
			t.Fatalf("chain payload backfill = %+v, want cursor record-prev", decoded.Backfill)
		}
	})
}
