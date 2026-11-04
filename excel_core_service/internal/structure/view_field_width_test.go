package structure_test

import (
	"testing"

	structurev1 "excel/api/structure/v1"
)

// 视图级字段宽度口径：宽度 map 只存显式设置的字段，未出现的字段无宽度条目，
// 前端按自身默认宽度渲染（后端不写死默认值，避免与前端默认漂移）。
func TestViewFieldWidthRoundTripOverwriteAndIsolation(t *testing.T) {
	harness := newStructureHarness(t)
	table := createTable(t, harness, "宽度表")
	primary := createField(t, harness, table.GetId(), "主字段", structurev1.FieldType_FIELD_TYPE_TEXT, nil)
	nameField := createField(t, harness, table.GetId(), "名称", structurev1.FieldType_FIELD_TYPE_TEXT, nil)
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	viewA := createView(t, harness, table.GetId(), "视图A")
	viewB := createView(t, harness, table.GetId(), "视图B")

	// 写入宽度 → 读取一致
	if _, err := harness.client.UpdateViewConfig(ctx, &structurev1.UpdateViewConfigRequest{
		ViewId: viewA.GetId(),
		ViewConfig: &structurev1.ViewConfig{
			FieldWidthMap: map[string]int32{
				primary.GetId():   120,
				nameField.GetId(): 240,
			},
		},
	}); err != nil {
		t.Fatalf("UpdateViewConfig(width) error = %v", err)
	}
	gotA := mustGetViewConfig(t, harness, viewA.GetId())
	if got := gotA.GetFieldWidthMap(); got[primary.GetId()] != 120 || got[nameField.GetId()] != 240 {
		t.Fatalf("view A width map = %v, want primary=120 name=240", got)
	}

	// 再次全量替换可覆盖（name 字段从 map 中移除）
	if _, err := harness.client.UpdateViewConfig(ctx, &structurev1.UpdateViewConfigRequest{
		ViewId: viewA.GetId(),
		ViewConfig: &structurev1.ViewConfig{
			FieldWidthMap: map[string]int32{
				primary.GetId(): 80,
			},
		},
	}); err != nil {
		t.Fatalf("UpdateViewConfig(overwrite) error = %v", err)
	}
	gotA = mustGetViewConfig(t, harness, viewA.GetId())
	if got := gotA.GetFieldWidthMap(); len(got) != 1 || got[primary.GetId()] != 80 {
		t.Fatalf("view A overwritten width map = %v, want primary=80 only", got)
	}

	// 未设宽度的视图 → map 为空（无显式宽度，前端按默认渲染）
	gotB := mustGetViewConfig(t, harness, viewB.GetId())
	if len(gotB.GetFieldWidthMap()) != 0 {
		t.Fatalf("view B default width map = %v, want empty", gotB.GetFieldWidthMap())
	}

	// 向视图 B 写入 → 视图 A 不受影响（视图级隔离）
	if _, err := harness.client.UpdateViewConfig(ctx, &structurev1.UpdateViewConfigRequest{
		ViewId: viewB.GetId(),
		ViewConfig: &structurev1.ViewConfig{
			FieldWidthMap: map[string]int32{nameField.GetId(): 300},
		},
	}); err != nil {
		t.Fatalf("UpdateViewConfig(viewB) error = %v", err)
	}
	gotA = mustGetViewConfig(t, harness, viewA.GetId())
	if got := gotA.GetFieldWidthMap(); len(got) != 1 || got[primary.GetId()] != 80 {
		t.Fatalf("view A width map leaked after view B write = %v, want primary=80 only", got)
	}
}

func TestViewFieldWidthValidation(t *testing.T) {
	harness := newStructureHarness(t)
	table := createTable(t, harness, "宽度校验")
	primary := createField(t, harness, table.GetId(), "主字段", structurev1.FieldType_FIELD_TYPE_TEXT, nil)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	view := createView(t, harness, table.GetId(), "表格")

	for _, width := range []int32{0, -1, 39, 2001} {
		_, err := harness.client.UpdateViewConfig(ctx, &structurev1.UpdateViewConfigRequest{
			ViewId: view.GetId(),
			ViewConfig: &structurev1.ViewConfig{
				FieldWidthMap: map[string]int32{primary.GetId(): width},
			},
		})
		requireErrorCode(t, err, 1001)
	}

	// 未知字段 id 拒绝
	_, err := harness.client.UpdateViewConfig(ctx, &structurev1.UpdateViewConfigRequest{
		ViewId: view.GetId(),
		ViewConfig: &structurev1.ViewConfig{
			FieldWidthMap: map[string]int32{"nonexistent_field": 120},
		},
	})
	requireErrorCode(t, err, 1001)

	// 上下限边界值合法
	for _, width := range []int32{40, 2000} {
		if _, err := harness.client.UpdateViewConfig(ctx, &structurev1.UpdateViewConfigRequest{
			ViewId: view.GetId(),
			ViewConfig: &structurev1.ViewConfig{
				FieldWidthMap: map[string]int32{primary.GetId(): width},
			},
		}); err != nil {
			t.Fatalf("width %d should be valid, got error = %v", width, err)
		}
	}
}

func mustGetViewConfig(
	t *testing.T,
	harness *structureHarness,
	viewID string,
) *structurev1.ViewConfig {
	t.Helper()
	response, err := harness.client.GetViewConfig(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.GetViewConfigRequest{ViewId: viewID},
	)
	if err != nil {
		t.Fatalf("GetViewConfig() error = %v", err)
	}
	return response.GetViewConfig()
}
