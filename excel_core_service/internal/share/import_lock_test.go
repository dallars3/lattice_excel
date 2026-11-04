// share/import_lock_test.go：导入中的 Base 全员不可访问（ticket 16）。
package share

import (
	"context"
	"testing"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
)

func TestCheckAccessDeniesImportingBaseForEveryone(t *testing.T) {
	harness := newShareTestHarness(t)
	ctx := context.Background()
	const (
		baseID       = "01J00000000000000000003301"
		enterpriseID = "01J00000000000000000003302"
		ownerID      = "01J00000000000000000003303"
		collaborator = "01J00000000000000000003304"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.importLock[baseID] = true
	harness.baseOwner[baseID] = ownerID
	harness.memberRole[enterpriseID+"/"+ownerID] = iamv1.MemberRole_MEMBER_ROLE_OWNER
	seedShareCollaborator(t, harness, baseID, collaborator, "manager", true)

	request := &sharev1.CheckAccessRequest{
		Principal: &commonv1.Principal{
			UserId:       ownerID,
			EnterpriseId: enterpriseID,
			Internal:     true,
		},
		BaseId: baseID,
		Action: sharev1.Action_ACTION_VIEW,
	}
	denied, err := harness.client.CheckAccess(ctx, request)
	if err != nil {
		t.Fatalf("CheckAccess(owner on importing Base) error = %v", err)
	}
	if denied.GetAllow() {
		t.Fatal("CheckAccess(owner on importing Base) allow = true, want false")
	}

	externalRequest := &sharev1.CheckAccessRequest{
		Principal: &commonv1.Principal{UserId: collaborator, Internal: false},
		BaseId:    baseID,
		Action:    sharev1.Action_ACTION_VIEW,
	}
	externalDenied, err := harness.client.CheckAccess(ctx, externalRequest)
	if err != nil {
		t.Fatalf("CheckAccess(collaborator on importing Base) error = %v", err)
	}
	if externalDenied.GetAllow() {
		t.Fatal("CheckAccess(collaborator on importing Base) allow = true, want false")
	}

	// 解锁后恢复访问。
	harness.importLock[baseID] = false
	allowed, err := harness.client.CheckAccess(ctx, request)
	if err != nil {
		t.Fatalf("CheckAccess(owner after unlock) error = %v", err)
	}
	if !allowed.GetAllow() {
		t.Fatal("CheckAccess(owner after unlock) allow = false, want true")
	}
}
