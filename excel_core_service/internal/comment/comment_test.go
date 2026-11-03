package comment_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"excel-libs/eventbus"
	commentv1 "excel/api/comment/v1"
	commonv1 "excel/api/common/v1"
	"excel_core_service/base"
	corebus "excel_core_service/bus"
	"excel_core_service/internal/comment"
)

const commentTestMySQLDSN = "excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true"

type commentHarness struct {
	client   commentv1.CommentServiceClient
	db       *sql.DB
	events   *eventbus.MemoryBus
	forward  *corebus.Forwarder
	baseID   string
	tableID  string
	recordID string
	userA    string
	userB    string
}

func TestCommentThreadLifecycleMentionResolveAndDeleteOwnership(t *testing.T) {
	harness := newCommentHarness(t)
	ctxA := commentPrincipal(t, harness.userA)
	ctxB := commentPrincipal(t, harness.userB)

	createdEvents := subscribeCommentEvents(t, harness.events)
	root, err := harness.client.CreateComment(ctxA, &commentv1.CreateCommentRequest{
		RecordId:    harness.recordID,
		Content:     "请 @B 看一下",
		MentionList: []string{harness.userB},
	})
	if err != nil {
		t.Fatalf("CreateComment() error = %v", err)
	}
	rootComment := root.GetComment()
	if rootComment.GetParentId() != "" ||
		rootComment.GetStatus() != commentv1.CommentStatus_COMMENT_STATUS_OPEN ||
		rootComment.GetAuthor() != harness.userA ||
		rootComment.GetContent() != "请 @B 看一下" ||
		len(rootComment.GetMentionList()) != 1 ||
		rootComment.GetMentionList()[0] != harness.userB {
		t.Fatalf("created root comment = %+v", rootComment)
	}

	if _, err := harness.forward.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce(root) error = %v", err)
	}
	created := receiveCommentEvent(t, createdEvents["comment.created"])
	mentioned := receiveCommentEvent(t, createdEvents["comment.mentioned"])
	assertEventPayload(t, created, map[string]any{
		"base_id":    harness.baseID,
		"record_id":  harness.recordID,
		"comment_id": rootComment.GetId(),
		"author":     harness.userA,
	})
	assertEventPayload(t, mentioned, map[string]any{
		"base_id":                harness.baseID,
		"record_id":              harness.recordID,
		"comment_id":             rootComment.GetId(),
		"mentioned_user_id_list": []any{harness.userB},
		"author":                 harness.userA,
	})

	reply, err := harness.client.ReplyComment(ctxB, &commentv1.ReplyCommentRequest{
		CommentId: rootComment.GetId(),
		Content:   "已处理",
	})
	if err != nil {
		t.Fatalf("ReplyComment() error = %v", err)
	}
	if reply.GetComment().GetParentId() != rootComment.GetId() ||
		reply.GetComment().GetRecordId() != harness.recordID ||
		reply.GetComment().GetAuthor() != harness.userB {
		t.Fatalf("reply comment = %+v", reply.GetComment())
	}
	if _, err := harness.forward.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce(reply) error = %v", err)
	}
	replyEvent := receiveCommentEvent(t, createdEvents["comment.created"])
	assertEventPayload(t, replyEvent, map[string]any{
		"base_id":          harness.baseID,
		"record_id":        harness.recordID,
		"comment_id":       reply.GetComment().GetId(),
		"author":           harness.userB,
		"parent_author_id": harness.userA,
	})

	listed, err := harness.client.ListComments(ctxA, &commentv1.ListCommentsRequest{
		RecordId: harness.recordID,
		Limit:    100,
	})
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(listed.GetCommentList()) != 2 || listed.GetUnsolvedCount() != 1 {
		t.Fatalf("listed comments = %+v unsolved=%d", listed.GetCommentList(), listed.GetUnsolvedCount())
	}

	if _, err := harness.client.ResolveComment(ctxA, &commentv1.ResolveCommentRequest{
		CommentId: rootComment.GetId(),
	}); err != nil {
		t.Fatalf("ResolveComment() error = %v", err)
	}
	listed, err = harness.client.ListComments(ctxA, &commentv1.ListCommentsRequest{
		RecordId: harness.recordID,
	})
	if err != nil {
		t.Fatalf("ListComments(resolved) error = %v", err)
	}
	if listed.GetUnsolvedCount() != 0 {
		t.Fatalf("unsolved_count after resolve = %d, want 0", listed.GetUnsolvedCount())
	}

	reopened, err := harness.client.ReopenComment(ctxA, &commentv1.ReopenCommentRequest{
		CommentId: rootComment.GetId(),
	})
	if err != nil {
		t.Fatalf("ReopenComment() error = %v", err)
	}
	if reopened.GetComment().GetStatus() != commentv1.CommentStatus_COMMENT_STATUS_OPEN {
		t.Fatalf("reopened status = %s", reopened.GetComment().GetStatus())
	}

	if _, err := harness.client.DeleteComment(ctxA, &commentv1.DeleteCommentRequest{
		CommentId: reply.GetComment().GetId(),
	}); err == nil {
		t.Fatal("DeleteComment(other author) succeeded, want permission error")
	}
	if _, err := harness.client.DeleteComment(ctxB, &commentv1.DeleteCommentRequest{
		CommentId: reply.GetComment().GetId(),
	}); err != nil {
		t.Fatalf("DeleteComment(own) error = %v", err)
	}
	if _, err := harness.client.DeleteComment(ctxA, &commentv1.DeleteCommentRequest{
		CommentId: rootComment.GetId(),
	}); err != nil {
		t.Fatalf("DeleteComment(root) error = %v", err)
	}
	listed, err = harness.client.ListComments(ctxA, &commentv1.ListCommentsRequest{
		RecordId: harness.recordID,
	})
	if err != nil {
		t.Fatalf("ListComments(deleted) error = %v", err)
	}
	if len(listed.GetCommentList()) != 0 || listed.GetUnsolvedCount() != 0 {
		t.Fatalf("comments after delete = %+v unsolved=%d", listed.GetCommentList(), listed.GetUnsolvedCount())
	}
}

func newCommentHarness(t *testing.T) *commentHarness {
	t.Helper()
	db, err := base.OpenDB(commentTestMySQLDSN)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("MigrateUp() error = %v", err)
	}

	prefix := fmt.Sprintf("%026d", time.Now().UnixNano())
	harness := &commentHarness{
		db:       db,
		events:   eventbus.NewMemoryBus(),
		baseID:   prefix[:25] + "1",
		tableID:  prefix[:25] + "2",
		recordID: prefix[:25] + "3",
		userA:    prefix[:25] + "4",
		userB:    prefix[:25] + "5",
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM excel_comment_comments WHERE record_id = ?`, harness.recordID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM excel_bus_outbox WHERE JSON_UNQUOTE(JSON_EXTRACT(payload, '$.record_id')) = ?`, harness.recordID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM excel_records WHERE id = ?`, harness.recordID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM excel_structure_tables WHERE id = ?`, harness.tableID)
	})
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO excel_structure_tables
			(id, enterprise_id, base_id, name, position, created_at)
		VALUES (?, ?, ?, 'comment-test', 0, ?)
	`, harness.tableID, harness.baseID, harness.baseID, time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed table: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO excel_records
			(id, table_id, cells_json, created_by, created_at, version)
		VALUES (?, ?, JSON_OBJECT(), ?, ?, 1)
	`, harness.recordID, harness.tableID, harness.userA, time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed record: %v", err)
	}

	store := base.NewBusStore(db)
	outbox := corebus.NewOutbox(store)
	service := comment.NewService(base.NewCommentRepository(db), outbox, time.Now)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	comment.Register(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///comment-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	harness.client = commentv1.NewCommentServiceClient(connection)
	harness.forward = corebus.NewForwarder(store, harness.events, corebus.ForwarderConfig{})
	return harness
}

func commentPrincipal(t *testing.T, userID string) context.Context {
	t.Helper()
	payload, err := proto.Marshal(&commonv1.Principal{UserId: userID, Internal: true})
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
	)
}

func subscribeCommentEvents(
	t *testing.T,
	bus *eventbus.MemoryBus,
) map[string]<-chan eventbus.Delivery {
	t.Helper()
	result := make(map[string]<-chan eventbus.Delivery)
	for _, topic := range []string{"comment.created", "comment.mentioned"} {
		subscription, err := bus.Subscribe(context.Background(), eventbus.SubscribeOptions{
			Topic:     topic,
			Mode:      eventbus.DeliveryModeReliable,
			QueueSize: 8,
		})
		if err != nil {
			t.Fatalf("Subscribe(%s) error = %v", topic, err)
		}
		t.Cleanup(func() { _ = subscription.Close() })
		memory, ok := subscription.(interface {
			Receive(context.Context) (eventbus.Delivery, error)
		})
		if !ok {
			t.Fatalf("subscription %T does not expose Receive", subscription)
		}
		deliveries := make(chan eventbus.Delivery, 8)
		go func() {
			for {
				delivery, receiveErr := memory.Receive(context.Background())
				if receiveErr != nil {
					close(deliveries)
					return
				}
				deliveries <- delivery
			}
		}()
		result[topic] = deliveries
	}
	return result
}

func receiveCommentEvent(t *testing.T, deliveries <-chan eventbus.Delivery) eventbus.Event {
	t.Helper()
	select {
	case delivery, ok := <-deliveries:
		if !ok {
			t.Fatal("event subscription closed")
		}
		return delivery.Event
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for comment event")
		return eventbus.Event{}
	}
}

func assertEventPayload(t *testing.T, event eventbus.Event, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(event.Payload, &got); err != nil {
		t.Fatalf("decode %s payload: %v", event.Header.Type, err)
	}
	for key, wantValue := range want {
		if fmt.Sprint(got[key]) != fmt.Sprint(wantValue) {
			t.Fatalf("%s payload[%s] = %#v, want %#v; payload=%s",
				event.Header.Type, key, got[key], wantValue, event.Payload)
		}
	}
}
