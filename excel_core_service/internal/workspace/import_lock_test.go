// workspace/import_lock_test.go：导入中的 Base 全员不可访问，解锁后恢复（ticket 16）。
package workspace_test

import (
	"context"
	"testing"

	"google.golang.org/protobuf/types/known/emptypb"

	workspacev1 "excel/api/workspace/v1"
)

func TestImportingBaseIsHiddenUntilImportFinished(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ownerCtx := authenticatedContext(t, testOwner, testEnterprise)

	created, err := harness.client.CreateBase(ownerCtx, &workspacev1.CreateBaseRequest{
		Name:          "导入中",
		ImportPending: true,
	})
	if err != nil {
		t.Fatalf("CreateBase(import pending) error = %v", err)
	}
	baseID := created.GetBase().GetId()

	importing, err := harness.repository.Importing(context.Background(), baseID)
	if err != nil {
		t.Fatalf("Importing() error = %v", err)
	}
	if !importing {
		t.Fatal("Importing() = false, want true while import pending")
	}
	normal, err := harness.client.CreateBase(ownerCtx, &workspacev1.CreateBaseRequest{
		Name: "常规",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}

	home, err := harness.client.GetHome(ownerCtx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetHome() error = %v", err)
	}
	for _, item := range home.GetHome().GetCreatedList() {
		if item.GetId() == baseID {
			t.Fatal("importing Base visible in created list, want hidden")
		}
	}
	found := false
	for _, item := range home.GetHome().GetCreatedList() {
		if item.GetId() == normal.GetBase().GetId() {
			found = true
		}
	}
	if !found {
		t.Fatal("normal Base missing from created list, want visible")
	}

	// import.finished 消费者落库解锁（幂等，重复调用无副作用）。
	if err := harness.repository.ClearImportStatus(context.Background(), baseID); err != nil {
		t.Fatalf("ClearImportStatus() error = %v", err)
	}
	if err := harness.repository.ClearImportStatus(context.Background(), baseID); err != nil {
		t.Fatalf("repeated ClearImportStatus() error = %v", err)
	}

	importing, err = harness.repository.Importing(context.Background(), baseID)
	if err != nil {
		t.Fatalf("Importing() after unlock error = %v", err)
	}
	if importing {
		t.Fatal("Importing() = true after unlock, want false")
	}
	home, err = harness.client.GetHome(ownerCtx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetHome() after unlock error = %v", err)
	}
	found = false
	for _, item := range home.GetHome().GetCreatedList() {
		if item.GetId() == baseID {
			found = true
		}
	}
	if !found {
		t.Fatal("unlocked Base still hidden in created list, want visible")
	}
}
