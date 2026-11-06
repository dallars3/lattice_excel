// comment/permission_matrix_test.go：评论层级与评论/删除权限矩阵契约（工单 10，
// 缺陷 fullflow #19）。锁定 spec §4 决策：ReplyComment 仅允许一级父评论（对二级
// 回复再回复 1001）；filler 仅能评论自己创建的记录；viewer 不可评论；评论作者或
// Base 可管理者可删除评论；越权统一 3001。夹具 ID 一律以 01JT10 前缀命名并按
// 前缀清理，不触碰其它夹具数据。
package comment_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	commentv1 "excel/api/comment/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	corebus "excel_core_service/bus"
	"excel_core_service/deal"
	"excel_core_service/internal/comment"
	"excel_core_service/internal/iam"
	"excel_core_service/internal/share"
	"excel_core_service/internal/workspace"
)

// TestReplyToSecondLevelReplyRejectedAndListStaysTwoLevel 锁定评论两级结构：
// 对二级回复再回复返回 1001，且列表保持「顶层线程 + 其二级回复」的稳定形状。
func TestReplyToSecondLevelReplyRejectedAndListStaysTwoLevel(t *testing.T) {
	harness := newCommentHarness(t)
	ctxA := commentPrincipal(t, harness.userA)
	ctxB := commentPrincipal(t, harness.userB)

	root, err := harness.client.CreateComment(ctxA, &commentv1.CreateCommentRequest{
		RecordId: harness.recordID,
		Content:  "顶层评论",
	})
	if err != nil {
		t.Fatalf("CreateComment(root) error = %v", err)
	}
	reply, err := harness.client.ReplyComment(ctxB, &commentv1.ReplyCommentRequest{
		CommentId: root.GetComment().GetId(),
		Content:   "二级回复",
	})
	if err != nil {
		t.Fatalf("ReplyComment(second level) error = %v", err)
	}

	third, err := harness.client.ReplyComment(ctxA, &commentv1.ReplyCommentRequest{
		CommentId: reply.GetComment().GetId(),
		Content:   "三级回复",
	})
	requireCommentCode(t, err, deal.CodeInvalidArgument)
	if third != nil {
		t.Fatalf("ReplyComment(third level) returned response %+v", third)
	}

	listed, err := harness.client.ListComments(ctxA, &commentv1.ListCommentsRequest{
		RecordId: harness.recordID,
		Limit:    100,
	})
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	list := listed.GetCommentList()
	if len(list) != 2 {
		t.Fatalf("comment list size = %d, want 2 (one thread + one reply)", len(list))
	}
	if list[0].GetId() != root.GetComment().GetId() || list[0].GetParentId() != "" {
		t.Fatalf("list[0] = %+v, want top-level root thread", list[0])
	}
	if list[1].GetId() != reply.GetComment().GetId() ||
		list[1].GetParentId() != root.GetComment().GetId() {
		t.Fatalf("list[1] = %+v, want second-level reply of root", list[1])
	}
	if listed.GetUnsolvedCount() != 1 {
		t.Fatalf("unsolved_count = %d, want 1", listed.GetUnsolvedCount())
	}
}

// TestCommentWritePermissionMatrix 锁定评论写权限矩阵（spec §4）：filler 仅能
// 评论自己创建的记录；viewer 不可评论；editor/manager 可评 Base 内记录；越权
// 统一 3001。
func TestCommentWritePermissionMatrix(t *testing.T) {
	harness := newPermissionMatrixHarness(t)
	ownerCtx := t10InternalPrincipal(t, harness.ownerUserID)
	fillerCtx := t10ExternalPrincipal(t, harness.fillerUserID)
	editorCtx := t10ExternalPrincipal(t, harness.editorUserID)
	viewerCtx := t10ExternalPrincipal(t, harness.viewerUserID)

	// filler 评论自己创建的记录 → 成功。
	own, err := harness.client.CreateComment(fillerCtx, &commentv1.CreateCommentRequest{
		RecordId: harness.fillerRecordID,
		Content:  "filler 评论自己的行",
	})
	if err != nil {
		t.Fatalf("CreateComment(filler own record) error = %v", err)
	}
	if own.GetComment().GetAuthor() != harness.fillerUserID ||
		own.GetComment().GetRecordId() != harness.fillerRecordID {
		t.Fatalf("filler own comment = %+v", own.GetComment())
	}

	// filler 评论他人记录 → 3001。
	_, err = harness.client.CreateComment(fillerCtx, &commentv1.CreateCommentRequest{
		RecordId: harness.ownerRecordID,
		Content:  "filler 评论他人行",
	})
	requireCommentCode(t, err, deal.CodePermissionDenied)

	// filler 在他人记录的线程上回复 → 3001。
	root, err := harness.client.CreateComment(ownerCtx, &commentv1.CreateCommentRequest{
		RecordId: harness.ownerRecordID,
		Content:  "owner 顶层评论",
	})
	if err != nil {
		t.Fatalf("CreateComment(owner thread) error = %v", err)
	}
	_, err = harness.client.ReplyComment(fillerCtx, &commentv1.ReplyCommentRequest{
		CommentId: root.GetComment().GetId(),
		Content:   "filler 越权回复",
	})
	requireCommentCode(t, err, deal.CodePermissionDenied)

	// viewer 不可评论 → 3001（无论记录归属）。
	_, err = harness.client.CreateComment(viewerCtx, &commentv1.CreateCommentRequest{
		RecordId: harness.ownerRecordID,
		Content:  "viewer 越权评论",
	})
	requireCommentCode(t, err, deal.CodePermissionDenied)
	_, err = harness.client.CreateComment(viewerCtx, &commentv1.CreateCommentRequest{
		RecordId: harness.fillerRecordID,
		Content:  "viewer 越权评论",
	})
	requireCommentCode(t, err, deal.CodePermissionDenied)

	// editor 可评论 Base 内他人记录（既有行为保持）。
	if _, err := harness.client.CreateComment(editorCtx, &commentv1.CreateCommentRequest{
		RecordId: harness.ownerRecordID,
		Content:  "editor 评论他人行",
	}); err != nil {
		t.Fatalf("CreateComment(editor other record) error = %v", err)
	}

	// owner（内部成员兼 Base 所有者=可管理）可评论任意记录。
	if _, err := harness.client.CreateComment(ownerCtx, &commentv1.CreateCommentRequest{
		RecordId: harness.fillerRecordID,
		Content:  "owner 评论 filler 行",
	}); err != nil {
		t.Fatalf("CreateComment(owner filler record) error = %v", err)
	}
}

// TestDeleteCommentAllowsAuthorAndBaseManager 锁定删除权限（spec §4）：评论作者
// 或 Base 可管理者可删除评论；非作者且非管理者删除返回 3001。
func TestDeleteCommentAllowsAuthorAndBaseManager(t *testing.T) {
	harness := newPermissionMatrixHarness(t)
	ownerCtx := t10InternalPrincipal(t, harness.ownerUserID)
	fillerCtx := t10ExternalPrincipal(t, harness.fillerUserID)
	editorCtx := t10ExternalPrincipal(t, harness.editorUserID)
	viewerCtx := t10ExternalPrincipal(t, harness.viewerUserID)

	fillerComment, err := harness.client.CreateComment(fillerCtx, &commentv1.CreateCommentRequest{
		RecordId: harness.fillerRecordID,
		Content:  "filler 自己的评论",
	})
	if err != nil {
		t.Fatalf("CreateComment(filler) error = %v", err)
	}
	editorComment, err := harness.client.CreateComment(editorCtx, &commentv1.CreateCommentRequest{
		RecordId: harness.ownerRecordID,
		Content:  "editor 待治理评论",
	})
	if err != nil {
		t.Fatalf("CreateComment(editor) error = %v", err)
	}

	// 非作者且非管理者删除 → 3001。
	_, err = harness.client.DeleteComment(editorCtx, &commentv1.DeleteCommentRequest{
		CommentId: fillerComment.GetComment().GetId(),
	})
	requireCommentCode(t, err, deal.CodePermissionDenied)
	_, err = harness.client.DeleteComment(viewerCtx, &commentv1.DeleteCommentRequest{
		CommentId: editorComment.GetComment().GetId(),
	})
	requireCommentCode(t, err, deal.CodePermissionDenied)

	// Base 可管理者删除他人评论 → 成功。
	if _, err := harness.client.DeleteComment(ownerCtx, &commentv1.DeleteCommentRequest{
		CommentId: editorComment.GetComment().GetId(),
	}); err != nil {
		t.Fatalf("DeleteComment(manager) error = %v", err)
	}

	// 作者自删 → 成功；删除后列表收口。
	if _, err := harness.client.DeleteComment(fillerCtx, &commentv1.DeleteCommentRequest{
		CommentId: fillerComment.GetComment().GetId(),
	}); err != nil {
		t.Fatalf("DeleteComment(author) error = %v", err)
	}
	listed, err := harness.client.ListComments(fillerCtx, &commentv1.ListCommentsRequest{
		RecordId: harness.fillerRecordID,
	})
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(listed.GetCommentList()) != 0 || listed.GetUnsolvedCount() != 0 {
		t.Fatalf("comments after delete = %+v unsolved=%d", listed.GetCommentList(), listed.GetUnsolvedCount())
	}
}

// requireCommentCode 断言 gRPC 错误携带的业务码（errdetails.ErrorInfo.Reason）。
func requireCommentCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want business code %d", want)
	}
	grpcStatus, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	for _, detail := range grpcStatus.Details() {
		if info, ok := detail.(interface{ GetReason() string }); ok {
			if info.GetReason() == fmt.Sprint(want) {
				return
			}
		}
	}
	t.Fatalf("error = %v (grpc %s), want business code %d", err, grpcStatus.Code(), want)
}

const (
	// t10CommentEnterpriseID 是 Base 所属企业（数据租户）。
	t10CommentEnterpriseID = "01JT10ENTA0000000000000000"
	// t10CommentBaseID / t10CommentTableID 是评论权限矩阵的 Base 与数据表。
	t10CommentBaseID  = "01JT10BASE1000000000000000"
	t10CommentTableID = "01JT10TAB10000000000000000"
	// t10OwnerRecordID 由 Base 所有者创建；t10FillerRecordID 由 filler 创建。
	t10OwnerRecordID  = "01JT10REC10000000000000000"
	t10FillerRecordID = "01JT10REC20000000000000000"
	// t10OwnerUserID 是内部企业成员兼 Base 所有者（resolveDecision 判为可管理）。
	t10OwnerUserID = "01JT10OWN10000000000000000"
	// t10FillerUserID / t10ViewerUserID / t10EditorUserID 是外部协作者条目。
	t10FillerUserID = "01JT10FIL10000000000000000"
	t10ViewerUserID = "01JT10VIE10000000000000000"
	t10EditorUserID = "01JT10EDT10000000000000000"
)

type permissionMatrixHarness struct {
	client         commentv1.CommentServiceClient
	db             *sql.DB
	ownerRecordID  string
	fillerRecordID string
	ownerUserID    string
	fillerUserID   string
	viewerUserID   string
	editorUserID   string
}

// newPermissionMatrixHarness 按 cmd/main.go 生产装配接线（真实 share 角色裁决
// seam），夹具只含 01JT10 前缀行，清理按前缀精确删除。
func newPermissionMatrixHarness(t *testing.T) *permissionMatrixHarness {
	t.Helper()
	db, err := base.OpenDB(commentTestMySQLDSN)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("MigrateUp() error = %v", err)
	}
	resetPermissionMatrixData(t, db)
	t.Cleanup(func() { resetPermissionMatrixData(t, db) })

	now := time.Now().UnixMilli()
	seedPermissionMatrixFixtures(t, db, now)

	workspaceRepository := base.NewWorkspaceRepository(db)
	shareService, err := share.NewService(share.ServiceDeps{
		Repository:           base.NewShareRepository(db),
		Outbox:               corebus.NewOutbox(base.NewBusStore(db)),
		Bases:                workspace.NewShareBaseResolver(workspaceRepository),
		ImportLock:           workspace.NewShareBaseResolver(workspaceRepository),
		BaseOwner:            workspace.NewShareBaseOwnerProvider(workspaceRepository),
		Members:              iam.NewMemberRoleProvider(base.NewIAMRepository(db)),
		PermissionConfigPath: writeT10PermissionPolicy(t),
	})
	if err != nil {
		t.Fatalf("share.NewService() error = %v", err)
	}
	service := comment.NewService(
		base.NewCommentRepository(db),
		corebus.NewOutbox(base.NewBusStore(db)),
		time.Now,
		comment.NewShareRoleAccess(shareService),
	)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	comment.Register(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///comment-permission-bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	return &permissionMatrixHarness{
		client:         commentv1.NewCommentServiceClient(connection),
		db:             db,
		ownerRecordID:  t10OwnerRecordID,
		fillerRecordID: t10FillerRecordID,
		ownerUserID:    t10OwnerUserID,
		fillerUserID:   t10FillerUserID,
		viewerUserID:   t10ViewerUserID,
		editorUserID:   t10EditorUserID,
	}
}

// resetPermissionMatrixData 只清理 01JT10 前缀夹具，不影响并行测试数据。
func resetPermissionMatrixData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_comment_comments WHERE record_id LIKE '01JT10%'",
		"DELETE FROM excel_bus_outbox WHERE base_id LIKE '01JT10%'",
		"DELETE FROM excel_records WHERE id LIKE '01JT10%' OR table_id LIKE '01JT10%'",
		"DELETE FROM excel_structure_tables WHERE base_id LIKE '01JT10%'",
		"DELETE FROM excel_share_collaborators WHERE base_id LIKE '01JT10%' OR user_id LIKE '01JT10%'",
		"DELETE FROM excel_share_links WHERE base_id LIKE '01JT10%'",
		"DELETE FROM excel_workspace_stars WHERE base_id LIKE '01JT10%'",
		"DELETE FROM excel_workspace_recent WHERE base_id LIKE '01JT10%'",
		"DELETE FROM excel_workspace_bases WHERE id LIKE '01JT10%'",
		"DELETE FROM excel_iam_members WHERE enterprise_id LIKE '01JT10%' OR user_id LIKE '01JT10%'",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset permission matrix data with %q: %v", statement, err)
		}
	}
}

func seedPermissionMatrixFixtures(t *testing.T, db *sql.DB, now int64) {
	t.Helper()
	fixtures := []struct {
		query string
		args  []any
	}{
		{
			"INSERT INTO excel_workspace_bases " +
				"(id, enterprise_id, owner_id, name, last_modified_at, last_modifier, created_at) " +
				"VALUES (?, ?, ?, ?, ?, ?, ?)",
			[]any{t10CommentBaseID, t10CommentEnterpriseID, t10OwnerUserID, "评论权限矩阵 Base", now, t10OwnerUserID, now},
		},
		{
			// 所有者是活跃企业成员（resolveDecision 内部成员路径的前提）。
			"INSERT INTO excel_iam_members " +
				"(enterprise_id, user_id, role, status, joined_at) VALUES (?, ?, 'owner', 'active', ?)",
			[]any{t10CommentEnterpriseID, t10OwnerUserID, now},
		},
		{
			"INSERT INTO excel_structure_tables " +
				"(id, enterprise_id, base_id, name, position, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{t10CommentTableID, t10CommentEnterpriseID, t10CommentBaseID, "评论表", 0, now},
		},
		{
			"INSERT INTO excel_share_collaborators " +
				"(base_id, user_id, role, internal, source, granted_at) VALUES (?, ?, 'filler', 0, 'invite', ?)",
			[]any{t10CommentBaseID, t10FillerUserID, now},
		},
		{
			"INSERT INTO excel_share_collaborators " +
				"(base_id, user_id, role, internal, source, granted_at) VALUES (?, ?, 'viewer', 0, 'invite', ?)",
			[]any{t10CommentBaseID, t10ViewerUserID, now},
		},
		{
			"INSERT INTO excel_share_collaborators " +
				"(base_id, user_id, role, internal, source, granted_at) VALUES (?, ?, 'editor', 0, 'invite', ?)",
			[]any{t10CommentBaseID, t10EditorUserID, now},
		},
		{
			"INSERT INTO excel_records " +
				"(id, table_id, cells_json, created_by, created_at, version) " +
				"VALUES (?, ?, JSON_OBJECT(), ?, ?, 1)",
			[]any{t10OwnerRecordID, t10CommentTableID, t10OwnerUserID, now},
		},
		{
			"INSERT INTO excel_records " +
				"(id, table_id, cells_json, created_by, created_at, version) " +
				"VALUES (?, ?, JSON_OBJECT(), ?, ?, 1)",
			[]any{t10FillerRecordID, t10CommentTableID, t10FillerUserID, now},
		},
	}
	for _, fixture := range fixtures {
		if _, err := db.ExecContext(context.Background(), fixture.query, fixture.args...); err != nil {
			t.Fatalf("seed permission matrix fixture %q: %v", fixture.query, err)
		}
	}
}

// t10InternalPrincipal 是内部成员会话（企业归属与 Base 企业一致）。
func t10InternalPrincipal(t *testing.T, userID string) context.Context {
	t.Helper()
	return t10PrincipalContext(t, &commonv1.Principal{
		UserId:       userID,
		EnterpriseId: t10CommentEnterpriseID,
		Internal:     true,
	})
}

// t10ExternalPrincipal 是外部协作者会话（无企业归属，凭协作者条目获得角色）。
func t10ExternalPrincipal(t *testing.T, userID string) context.Context {
	t.Helper()
	return t10PrincipalContext(t, &commonv1.Principal{UserId: userID})
}

func t10PrincipalContext(t *testing.T, principal *commonv1.Principal) context.Context {
	t.Helper()
	payload, err := proto.Marshal(principal)
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
	)
}

// stubBaseAccess 以固定角色驱动既有评论夹具（不经过 share 裁决）。
type stubBaseAccess struct {
	role sharev1.Role
}

// Role returns the stubbed role for every Base.
func (access stubBaseAccess) Role(
	context.Context,
	*commonv1.Principal,
	string,
) (sharev1.Role, error) {
	return access.role, nil
}

// writeT10PermissionPolicy 写出与 excel_permission_config/permissions.json 一致
// 的临时权限清单（share 裁决的角色→动作矩阵）。
func writeT10PermissionPolicy(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "permissions.json")
	payload := `{
		"schema_version": 1,
		"roles": {
			"manager": {"name": "可管理", "actions": ["view", "create_record", "edit_all", "delete_record", "manage_view", "manage_structure", "comment", "export", "manage_share", "copy_base", "import"]},
			"editor": {"name": "可编辑", "actions": ["view", "create_record", "edit_all", "delete_record", "manage_view", "comment", "export"]},
			"filler": {"name": "可填写", "actions": ["view_own", "create_record", "edit_own", "comment"]},
			"viewer": {"name": "可查看", "actions": ["view", "comment"]}
		},
		"external": {"export_allowed": false}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write permission policy: %v", err)
	}
	return path
}
