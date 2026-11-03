package notify_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	notifyv1 "excel/api/notify/v1"
	"excel_worker_service/base"
	workerbus "excel_worker_service/bus"
	"excel_worker_service/internal/notify"
	workerredis "excel_worker_service/redis"
)

const workerNotifyTestRedisAddr = "127.0.0.1:6379"

type notifyHarness struct {
	db        *sql.DB
	counters  *workerredis.NotifyCounters
	events    *eventbus.MemoryBus
	consumer  *workerbus.Consumer
	service   *notify.Service
	client    notifyv1.NotifyServiceClient
	prefix    string
	userIDs   []string
	usernames map[string]string
	eventIDs  []string
}

func TestNotifyRPCsPaginateRetainAndMarkReadIdempotently(t *testing.T) {
	harness := newNotifyHarness(t)
	ctx := notifyPrincipal(t, harness.userIDs[0])
	userID := harness.userIDs[0]
	now := time.Now().UTC().UnixMilli()
	baseID := harness.prefix[:25] + "b"
	rows := []base.Notification{
		{
			ID:         harness.prefix[:24] + "01",
			UserID:     userID,
			Type:       "comment.mentioned",
			BaseID:     baseID,
			RecordID:   harness.prefix[:24] + "r1",
			CommentID:  harness.prefix[:24] + "c1",
			PayloadMap: []byte(`{"record_id":"record-1","comment_id":"comment-1"}`),
			CreatedAt:  now - 1000,
		},
		{
			ID:         harness.prefix[:24] + "02",
			UserID:     userID,
			Type:       "share.changed",
			BaseID:     baseID,
			PayloadMap: []byte(`{"change_type":"updated"}`),
			CreatedAt:  now,
		},
		{
			ID:         harness.prefix[:24] + "03",
			UserID:     userID,
			Type:       "comment.created",
			BaseID:     baseID,
			PayloadMap: []byte(`{"expired":true}`),
			CreatedAt:  now - (91 * 24 * time.Hour).Milliseconds(),
		},
	}
	for _, row := range rows {
		if err := base.NewNotifyRepository(harness.db).Create(context.Background(), row); err != nil {
			t.Fatalf("create notification %s: %v", row.ID, err)
		}
	}
	if err := harness.counters.Increment(context.Background(), userID); err != nil {
		t.Fatalf("increment first unread: %v", err)
	}
	if err := harness.counters.Increment(context.Background(), userID); err != nil {
		t.Fatalf("increment second unread: %v", err)
	}

	count, err := harness.client.GetUnreadCount(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if count.GetCount() != 2 {
		t.Fatalf("unread count = %d, want 2 retained rows", count.GetCount())
	}

	firstPage, err := harness.client.ListNotifications(ctx, &notifyv1.ListNotificationsRequest{
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListNotifications(first) error = %v", err)
	}
	if len(firstPage.GetNotificationList()) != 1 ||
		firstPage.GetNotificationList()[0].GetId() != rows[1].ID ||
		!firstPage.GetHasMore() ||
		firstPage.GetNextCursor() == "" {
		t.Fatalf("first notification page = %+v", firstPage)
	}
	if firstPage.GetNotificationList()[0].GetPayloadMap().GetFields()["change_type"].GetStringValue() != "updated" {
		t.Fatalf("first notification payload = %+v", firstPage.GetNotificationList()[0].GetPayloadMap())
	}
	secondPage, err := harness.client.ListNotifications(ctx, &notifyv1.ListNotificationsRequest{
		Cursor: firstPage.GetNextCursor(),
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("ListNotifications(second) error = %v", err)
	}
	if len(secondPage.GetNotificationList()) != 1 ||
		secondPage.GetNotificationList()[0].GetId() != rows[0].ID ||
		secondPage.GetHasMore() {
		t.Fatalf("second notification page = %+v", secondPage)
	}

	if _, err := harness.client.MarkRead(ctx, &notifyv1.MarkReadRequest{
		NotificationId: rows[1].ID,
	}); err != nil {
		t.Fatalf("MarkRead(first) error = %v", err)
	}
	if _, err := harness.client.MarkRead(ctx, &notifyv1.MarkReadRequest{
		NotificationId: rows[1].ID,
	}); err != nil {
		t.Fatalf("MarkRead(replay) error = %v", err)
	}
	count, err = harness.client.GetUnreadCount(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetUnreadCount(after mark) error = %v", err)
	}
	if count.GetCount() != 1 {
		t.Fatalf("unread count after repeated MarkRead = %d, want 1", count.GetCount())
	}

	if _, err := harness.client.MarkAllRead(ctx, &emptypb.Empty{}); err != nil {
		t.Fatalf("MarkAllRead(first) error = %v", err)
	}
	if _, err := harness.client.MarkAllRead(ctx, &emptypb.Empty{}); err != nil {
		t.Fatalf("MarkAllRead(replay) error = %v", err)
	}
	count, err = harness.client.GetUnreadCount(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetUnreadCount(after mark-all) error = %v", err)
	}
	if count.GetCount() != 0 {
		t.Fatalf("unread count after repeated MarkAllRead = %d, want 0", count.GetCount())
	}
}

func TestNotifyConsumerMapsTenEventsToRecipientsAndPublishesRedDots(t *testing.T) {
	harness := newNotifyHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	redDots, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic:     "notify.created",
		Mode:      eventbus.DeliveryModePubSub,
		QueueSize: 32,
	})
	if err != nil {
		t.Fatalf("subscribe notify.created: %v", err)
	}
	t.Cleanup(func() { _ = redDots.Close() })

	user := func(suffix int) string {
		return harness.prefix[:25] + string(rune('A'+suffix))
	}
	parentAuthor := user(0)
	mentionA := user(1)
	mentionB := user(2)
	inviteUser := user(3)
	memberUser := user(4)
	oldOwner := user(5)
	newOwner := user(6)
	linkCreator := user(7)
	shareUser := user(8)
	importOwner := user(9)
	harness.seedUser(t, inviteUser, harness.usernames["invite"])

	events := []eventbus.Event{
		notifyTestEvent(harness, "comment.created", "", map[string]any{
			"base_id":          harness.prefix[:25] + "b",
			"record_id":        harness.prefix[:25] + "r",
			"comment_id":       harness.prefix[:25] + "c",
			"author":           user(10),
			"parent_author_id": parentAuthor,
		}),
		notifyTestEvent(harness, "comment.mentioned", "", map[string]any{
			"base_id":                harness.prefix[:25] + "b",
			"record_id":              harness.prefix[:25] + "r",
			"comment_id":             harness.prefix[:25] + "d",
			"mentioned_user_id_list": []string{mentionA, mentionB},
			"author":                 user(10),
		}),
		notifyTestEvent(harness, "invite.created", "", map[string]any{
			"enterprise_id": harness.prefix[:25] + "e",
			"username":      harness.usernames["invite"],
			"invite_id":     harness.prefix[:25] + "i",
		}),
		notifyTestEvent(harness, "invite.canceled", "", map[string]any{
			"enterprise_id": harness.prefix[:25] + "e",
			"username":      harness.usernames["invite"],
			"invite_id":     harness.prefix[:25] + "i",
		}),
		notifyTestEvent(harness, "member.disabled", "", map[string]any{
			"enterprise_id": harness.prefix[:25] + "e",
			"user_id":       memberUser,
			"operator":      user(10),
		}),
		notifyTestEvent(harness, "member.enabled", "", map[string]any{
			"enterprise_id": harness.prefix[:25] + "e",
			"user_id":       memberUser,
			"operator":      user(10),
		}),
		notifyTestEvent(harness, "enterprise.owner_transferred", "", map[string]any{
			"enterprise_id": harness.prefix[:25] + "e",
			"from_user_id":  oldOwner,
			"to_user_id":    newOwner,
		}),
		notifyTestEvent(harness, "link.closed", harness.prefix[:25]+"b", map[string]any{
			"base_id":    harness.prefix[:25] + "b",
			"link_id":    harness.prefix[:25] + "l",
			"creator_id": linkCreator,
			"reason":     "manager_force_close",
		}),
		notifyTestEvent(harness, "share.changed", harness.prefix[:25]+"b", map[string]any{
			"base_id":     harness.prefix[:25] + "b",
			"change_type": "removed",
			"user_id":     shareUser,
			"role":        "viewer",
		}),
		notifyTestEvent(harness, "import.finished", harness.prefix[:25]+"b", map[string]any{
			"task_id":    harness.prefix[:25] + "t",
			"base_id":    harness.prefix[:25] + "b",
			"ok_rows":    10,
			"fail_rows":  1,
			"creator_id": importOwner,
		}),
	}
	for _, event := range events {
		if err := harness.events.Publish(ctx, event); err != nil {
			t.Fatalf("publish %s: %v", event.Header.Type, err)
		}
	}

	waitForNotificationCount(t, ctx, harness.db, harness.userIDs, 12)
	rows := queryNotifications(t, harness.db, harness.userIDs)
	wantRecipients := map[string]map[string]int{
		"comment.created":              {parentAuthor: 1},
		"comment.mentioned":            {mentionA: 1, mentionB: 1},
		"invite.created":               {inviteUser: 1},
		"invite.canceled":              {inviteUser: 1},
		"member.disabled":              {memberUser: 1},
		"member.enabled":               {memberUser: 1},
		"enterprise.owner_transferred": {oldOwner: 1, newOwner: 1},
		"link.closed":                  {linkCreator: 1},
		"share.changed":                {shareUser: 1},
		"import.finished":              {importOwner: 1},
	}
	gotRecipients := make(map[string]map[string]int)
	for _, row := range rows {
		if gotRecipients[row.Type] == nil {
			gotRecipients[row.Type] = make(map[string]int)
		}
		gotRecipients[row.Type][row.UserID]++
		if row.Read {
			t.Fatalf("new notification %s is already read", row.ID)
		}
	}
	if fmt.Sprint(gotRecipients) != fmt.Sprint(wantRecipients) {
		t.Fatalf("notification recipients = %#v, want %#v", gotRecipients, wantRecipients)
	}

	gotCounts := make(map[string]int)
	for _, userID := range harness.userIDs {
		count, found, err := harness.counters.Lookup(ctx, userID)
		if err != nil {
			t.Fatalf("counter Lookup(%s): %v", userID, err)
		}
		if found {
			gotCounts[userID] = int(count)
		}
	}
	wantCounts := map[string]int{
		parentAuthor: 1,
		mentionA:     1,
		mentionB:     1,
		inviteUser:   2,
		memberUser:   2,
		oldOwner:     1,
		newOwner:     1,
		linkCreator:  1,
		shareUser:    1,
		importOwner:  1,
	}
	if fmt.Sprint(gotCounts) != fmt.Sprint(wantCounts) {
		t.Fatalf("Redis unread counts = %#v, want %#v", gotCounts, wantCounts)
	}

	redDotUsers := make(map[string]int)
	redDotIDs := make(map[string]struct{})
	for range rows {
		delivery, err := redDots.Receive(ctx)
		if err != nil {
			t.Fatalf("receive notify.created: %v", err)
		}
		var payload struct {
			NotificationID string `json:"notification_id"`
			UserID         string `json:"user_id"`
			Type           string `json:"type"`
			BaseID         string `json:"base_id"`
		}
		if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
			t.Fatalf("decode notify.created: %v", err)
		}
		if delivery.Event.Header.Type != "notify.created" ||
			payload.NotificationID == "" || payload.UserID == "" || payload.Type == "" {
			t.Fatalf("notify.created event = %+v payload=%+v", delivery.Event.Header, payload)
		}
		redDotUsers[payload.UserID]++
		redDotIDs[payload.NotificationID] = struct{}{}
	}
	for _, row := range rows {
		if _, ok := redDotIDs[row.ID]; !ok {
			t.Fatalf("notification %s has no notify.created event", row.ID)
		}
	}
	if fmt.Sprint(redDotUsers) != fmt.Sprint(wantCounts) {
		t.Fatalf("notify.created recipients = %#v, want %#v", redDotUsers, wantCounts)
	}
}

type notificationRow struct {
	ID         string
	UserID     string
	Type       string
	BaseID     string
	RecordID   string
	CommentID  string
	PayloadMap []byte
	Read       bool
	CreatedAt  int64
}

func newNotifyHarness(t *testing.T) *notifyHarness {
	t.Helper()
	ctx := context.Background()
	config, err := base.LoadConfig(filepath.Join("..", "..", "configs", "worker_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	db, err := base.OpenDB(config.MySQLDSN)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var notifyTableExists int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_name = 'excel_notify_notifications'
	`).Scan(&notifyTableExists); err != nil {
		t.Fatalf("inspect notify table: %v", err)
	}
	if notifyTableExists == 0 {
		if err := base.MigrateUp(db); err != nil {
			t.Fatalf("MigrateUp() error = %v", err)
		}
	}
	redisClient, err := workerredis.Open(ctx, workerNotifyTestRedisAddr)
	if err != nil {
		t.Fatalf("redis.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = redisClient.Close() })

	harness := &notifyHarness{
		db:        db,
		counters:  workerredis.NewNotifyCounters(redisClient),
		events:    eventbus.NewMemoryBus(),
		prefix:    fmt.Sprintf("%026d", time.Now().UnixNano()),
		usernames: map[string]string{"invite": fmt.Sprintf("notify%013d", time.Now().UnixNano()%10000000000000)},
	}
	for index := 0; index < 11; index++ {
		harness.userIDs = append(harness.userIDs, harness.prefix[:25]+string(rune('A'+index)))
	}
	repository := base.NewNotifyRepository(db)
	harness.service = notify.NewService(repository, harness.counters, harness.events, time.Now)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	notify.Register(server, harness.service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient(
		"passthrough:///notify-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	harness.client = notifyv1.NewNotifyServiceClient(connection)
	harness.consumer = workerbus.NewConsumer(db, harness.events, workerbus.ConsumerConfig{})
	if err := notify.Consumers(harness.consumer, harness.service); err != nil {
		t.Fatalf("Consumers() error = %v", err)
	}
	if err := harness.consumer.Start(ctx); err != nil {
		t.Fatalf("Consumer.Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = harness.consumer.Stop(stopCtx)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM excel_core.excel_iam_users WHERE username = ?`, harness.usernames["invite"])
		_, _ = db.ExecContext(context.Background(), `DELETE FROM excel_notify_notifications WHERE user_id IN (`+placeholders(len(harness.userIDs))+`)`, stringsToAny(harness.userIDs)...)
		if len(harness.eventIDs) > 0 {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM excel_bus_consumer_events WHERE event_id IN (`+placeholders(len(harness.eventIDs))+`)`, stringsToAny(harness.eventIDs)...)
		}
		for _, userID := range harness.userIDs {
			_ = redisClient.Del(context.Background(), workerredis.NotifyUnreadKey(userID)).Err()
		}
	})
	return harness
}

func notifyPrincipal(t *testing.T, userID string) context.Context {
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

func (harness *notifyHarness) seedUser(t *testing.T, userID, username string) {
	t.Helper()
	if _, err := harness.db.ExecContext(context.Background(), `
		INSERT INTO excel_core.excel_iam_users (
			id, username, display_name, avatar_uri, password_hash,
			enterprise_id, internal, created_at
		) VALUES (?, ?, ?, '', 'test-hash', NULL, 1, ?)
	`, userID, username, username, time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed core user: %v", err)
	}
}

func (harness *notifyHarness) nextEventID() string {
	eventID := fmt.Sprintf("%026d", time.Now().UnixNano()+int64(len(harness.eventIDs)))
	harness.eventIDs = append(harness.eventIDs, eventID)
	return eventID
}

func notifyTestEvent(
	harness *notifyHarness,
	topic string,
	baseID string,
	payload map[string]any,
) eventbus.Event {
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    harness.nextEventID(),
			Type:       topic,
			Version:    1,
			BaseID:     baseID,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "notify-test",
		},
		Payload: encoded,
	}
}

func waitForNotificationCount(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	userIDs []string,
	want int,
) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM excel_notify_notifications
			WHERE user_id IN (`+placeholders(len(userIDs))+`)
		`, stringsToAny(userIDs)...).Scan(&count); err != nil {
			t.Fatalf("count notifications: %v", err)
		}
		if count == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for notifications: %v", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("notifications did not reach %d", want)
}

func queryNotifications(t *testing.T, db *sql.DB, userIDs []string) []notificationRow {
	t.Helper()
	payloadColumn := "payload_map"
	readColumn := "`read`"
	var legacyPayload, legacyRead int
	if err := db.QueryRow(`
		SELECT
			SUM(column_name = 'payload_json'),
			SUM(column_name = 'is_read')
		FROM information_schema.columns
		WHERE table_schema = DATABASE()
		  AND table_name = 'excel_notify_notifications'
	`).Scan(&legacyPayload, &legacyRead); err != nil {
		t.Fatalf("inspect notify test columns: %v", err)
	}
	if legacyPayload > 0 {
		payloadColumn = "payload_json"
	}
	if legacyRead > 0 {
		readColumn = "is_read"
	}
	rows, err := db.Query(`
		SELECT id, user_id, type,
		       COALESCE(base_id, ''), COALESCE(record_id, ''), COALESCE(comment_id, ''),
		       `+payloadColumn+`, `+readColumn+`, created_at
		FROM excel_notify_notifications
		WHERE user_id IN (`+placeholders(len(userIDs))+`)
		ORDER BY created_at, id
	`, stringsToAny(userIDs)...)
	if err != nil {
		t.Fatalf("query notifications: %v", err)
	}
	defer rows.Close()
	result := make([]notificationRow, 0)
	for rows.Next() {
		var row notificationRow
		if err := rows.Scan(
			&row.ID, &row.UserID, &row.Type,
			&row.BaseID, &row.RecordID, &row.CommentID,
			&row.PayloadMap, &row.Read, &row.CreatedAt,
		); err != nil {
			t.Fatalf("scan notification: %v", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate notifications: %v", err)
	}
	return result
}

func placeholders(count int) string {
	result := ""
	for index := 0; index < count; index++ {
		if index > 0 {
			result += ", "
		}
		result += "?"
	}
	return result
}

func stringsToAny(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}
