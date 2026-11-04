// gateway/tracking_routes_test.go：埋点批上报端点表驱动/边界测试（内存 eventbus + 内存 fake 去重）。
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"excel-libs/eventbus"

	"excel_web_api/deal"
)

type trackingDedupeFake struct {
	mu   sync.Mutex
	seen map[string]bool
	err  error
}

func (fake *trackingDedupeFake) Dedupe(_ context.Context, eventID string, _ time.Duration) (bool, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.err != nil {
		return true, fake.err
	}
	if fake.seen == nil {
		fake.seen = make(map[string]bool)
	}
	if fake.seen[eventID] {
		return false, nil
	}
	fake.seen[eventID] = true
	return true, nil
}

type trackingBusFake struct {
	mu         sync.Mutex
	events     []eventbus.Event
	publishErr error
	publishN   int
}

func (fake *trackingBusFake) Publish(_ context.Context, event eventbus.Event) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.publishN++
	if fake.publishErr != nil {
		return fake.publishErr
	}
	fake.events = append(fake.events, event.Clone())
	return nil
}

func (fake *trackingBusFake) Subscribe(context.Context, eventbus.SubscribeOptions) (eventbus.Subscription, error) {
	return nil, errors.New("tracking bus fake does not support subscribe")
}

func (fake *trackingBusFake) publishedCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return len(fake.events)
}

var _ trackingDeduper = (*trackingDedupeFake)(nil)
var _ eventbus.EventBus = (*trackingBusFake)(nil)

func TestTrackingBatchRequiresSession(t *testing.T) {
	pipeline := NewPipeline(&pipelineIamRPC{}, nil, nil, nil)
	RegisterTrackingRoutes(pipeline, eventbus.NewMemoryBus(), &trackingDedupeFake{})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/tracking/batch", strings.NewReader(`{"events":[]}`))
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != deal.CodeUnauthenticated {
		t.Fatalf("code = %d, want 2001", envelope.Code)
	}
}

func TestTrackingBatchAcceptsAndStampsSingleEvent(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	subscription := subscribeTracking(t, bus)
	pipeline := newTrackingPipeline(t, bus, &trackingDedupeFake{})

	response := performTrackingRequest(t, pipeline, `{"events":[`+
		`{"event_id":"evt_0001","event_name":"fe_page_view_home","detail":{"base_id":"base-1"}}`+
		`]}`)
	accepted, duplicated, rejected := trackingCounts(t, response)
	if accepted != 1 || duplicated != 0 || rejected != 0 {
		t.Fatalf("counts = accepted:%d duplicated:%d rejected:%d, want 1/0/0", accepted, duplicated, rejected)
	}

	event := receiveTrackingEvent(t, subscription)
	if event.Header.Type != "track.reported" || event.Header.Producer != "tracking" ||
		event.Header.Version != 1 || event.Header.OccurredAt <= 0 || event.Header.EventID == "" {
		t.Fatalf("header = %+v, want stamped track.reported envelope", event.Header)
	}
	var payload struct {
		EventID      string          `json:"event_id"`
		EventName    string          `json:"event_name"`
		Detail       json.RawMessage `json:"detail"`
		UserID       string          `json:"user_id"`
		UserType     string          `json:"user_type"`
		ReceivedAtMs int64           `json:"received_at_ms"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.EventID != "evt_0001" || payload.EventName != "fe_page_view_home" {
		t.Fatalf("payload ids = event_id:%q event_name:%q", payload.EventID, payload.EventName)
	}
	if payload.UserID != "user-1" || payload.UserType != "internal" {
		t.Fatalf("payload stamp = user_id:%q user_type:%q, want user-1/internal", payload.UserID, payload.UserType)
	}
	if payload.ReceivedAtMs <= 0 {
		t.Fatalf("received_at_ms = %d, want positive server timestamp", payload.ReceivedAtMs)
	}
	if !json.Valid(payload.Detail) || string(payload.Detail) != `{"base_id":"base-1"}` {
		t.Fatalf("detail = %s, want canonical object", payload.Detail)
	}
}

func TestTrackingBatchStampsExternalUserType(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	subscription := subscribeTracking(t, bus)
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-2", "")}
	iam.exchangeResponse.Principal.Internal = false
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterTrackingRoutes(pipeline, bus, &trackingDedupeFake{})

	response := performTrackingRequest(t, pipeline, `{"events":[`+
		`{"event_id":"evt_0002","event_name":"fe_base_create","detail":{}}`+
		`]}`)
	if accepted, _, _ := trackingCounts(t, response); accepted != 1 {
		t.Fatalf("accepted = %d, want 1", accepted)
	}
	event := receiveTrackingEvent(t, subscription)
	var payload struct {
		UserType string `json:"user_type"`
		UserID   string `json:"user_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.UserType != "external" || payload.UserID != "user-2" {
		t.Fatalf("stamp = user_id:%q user_type:%q, want user-2/external", payload.UserID, payload.UserType)
	}
}

func TestTrackingBatchRejectsInvalidEntriesWithoutRejectingBatch(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	subscription := subscribeTracking(t, bus)
	pipeline := newTrackingPipeline(t, bus, &trackingDedupeFake{})

	response := performTrackingRequest(t, pipeline, `{"events":[`+
		`{"event_id":"evt_0001","event_name":"fe_page_view_home","detail":{"base_id":"base-1"}},`+
		`{"event_id":"evt_0002","event_name":"login_success","detail":{}},`+ // 非法：缺 fe_ 前缀
		`{"event_id":"bad","event_name":"fe_base_create","detail":{}}`+ // 非法：event_id 过短
		`]}`)
	accepted, duplicated, rejected := trackingCounts(t, response)
	if accepted != 1 || duplicated != 0 || rejected != 2 {
		t.Fatalf("counts = accepted:%d duplicated:%d rejected:%d, want 1/0/2", accepted, duplicated, rejected)
	}

	event := receiveTrackingEvent(t, subscription)
	var payload struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.EventID != "evt_0001" {
		t.Fatalf("published event_id = %q, want only the valid evt_0001", payload.EventID)
	}
}

func TestTrackingBatchRejectsOverBatchWholeRequest(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	pipeline := newTrackingPipeline(t, bus, &trackingDedupeFake{})

	events := make([]string, 0, 101)
	for i := 0; i < 101; i++ {
		events = append(events, `{"event_id":"evt_0001","event_name":"fe_page_view_home","detail":{}}`)
	}
	response := performTrackingRequest(t, pipeline, `{"events":[`+strings.Join(events, ",")+`]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != deal.CodeInvalidArgument {
		t.Fatalf("code = %d, want 1001", envelope.Code)
	}
}

func TestTrackingBatchRejectsInvalidJSON(t *testing.T) {
	bus := &trackingBusFake{}
	pipeline := newTrackingPipeline(t, bus, &trackingDedupeFake{})

	response := performTrackingRequest(t, pipeline, `{"events": [}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if bus.publishedCount() != 0 {
		t.Fatalf("published = %d, want zero for malformed JSON", bus.publishedCount())
	}
}

func TestTrackingBatchMarksDuplicateEventIDOnSecondSubmit(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	subscription := subscribeTracking(t, bus)
	dedupe := &trackingDedupeFake{}
	pipeline := newTrackingPipeline(t, bus, dedupe)

	body := `{"events":[{"event_id":"evt_0001","event_name":"fe_page_view_home","detail":{}}]}`
	if accepted, duplicated, rejected := trackingCounts(t, performTrackingRequest(t, pipeline, body)); accepted != 1 || duplicated != 0 || rejected != 0 {
		t.Fatalf("first submit counts = accepted:%d duplicated:%d rejected:%d, want 1/0/0", accepted, duplicated, rejected)
	}
	_ = receiveTrackingEvent(t, subscription)

	response := performTrackingRequest(t, pipeline, body)
	accepted, duplicated, rejected := trackingCounts(t, response)
	if accepted != 0 || duplicated != 1 || rejected != 0 {
		t.Fatalf("second submit counts = accepted:%d duplicated:%d rejected:%d, want 0/1/0", accepted, duplicated, rejected)
	}
}

func TestTrackingBatchFailsOpenWhenDedupeUnavailable(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	subscription := subscribeTracking(t, bus)
	pipeline := newTrackingPipeline(t, bus, &trackingDedupeFake{err: errors.New("redis down")})

	response := performTrackingRequest(t, pipeline, `{"events":[`+
		`{"event_id":"evt_0001","event_name":"fe_page_view_home","detail":{}}`+
		`]}`)
	accepted, duplicated, rejected := trackingCounts(t, response)
	if accepted != 1 || duplicated != 0 || rejected != 0 {
		t.Fatalf("counts = accepted:%d duplicated:%d rejected:%d, want 1/0/0 (fail-open)", accepted, duplicated, rejected)
	}
	_ = receiveTrackingEvent(t, subscription)
}

func TestTrackingBatchReturns503WhenPublishFails(t *testing.T) {
	bus := &trackingBusFake{publishErr: errors.New("stream down")}
	pipeline := newTrackingPipeline(t, bus, &trackingDedupeFake{})

	response := performTrackingRequest(t, pipeline, `{"events":[`+
		`{"event_id":"evt_0001","event_name":"fe_page_view_home","detail":{}}`+
		`]}`)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != deal.CodeServiceUnavailable {
		t.Fatalf("code = %d, want 9001", envelope.Code)
	}
	bus.mu.Lock()
	publishCalls := bus.publishN
	bus.mu.Unlock()
	if publishCalls != 1 {
		t.Fatalf("publish calls = %d, want exactly one (no retry amplification)", publishCalls)
	}
}

func TestTrackingBatchHasDedicatedRateBucket(t *testing.T) {
	scope, limit, ok := privacyRateRule(routeSpec{method: http.MethodPost, path: "/api/v1/tracking/batch"})
	if !ok || scope != "tracking" || limit != trackingRateLimit {
		t.Fatalf("rate rule = scope:%q limit:%d ok:%v, want tracking/%d/true", scope, limit, ok, trackingRateLimit)
	}
}

func TestNormalizeTrackingDetailRejectsTrailingJSON(t *testing.T) {
	if got, ok := normalizeTrackingDetail(json.RawMessage(`{"base_id":"base-1"}`)); !ok {
		t.Fatalf("normalizeTrackingDetail(valid object) = ok=false, want true")
	} else if string(got) != `{"base_id":"base-1"}` {
		t.Fatalf("normalizeTrackingDetail(valid object) = %s, want canonical object", got)
	}
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"a":1}{"b":2}`),
		json.RawMessage(`{}{}`),
		json.RawMessage(`{"a":1} {}`),
		json.RawMessage(`{"a":1} trailing`),
	} {
		if _, ok := normalizeTrackingDetail(raw); ok {
			t.Fatalf("normalizeTrackingDetail(%s) = ok=true, want reject trailing garbage", raw)
		}
	}
}

func newTrackingPipeline(t *testing.T, bus eventbus.EventBus, dedupe trackingDeduper) *Pipeline {
	t.Helper()
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterTrackingRoutes(pipeline, bus, dedupe)
	return pipeline
}

func performTrackingRequest(t *testing.T, pipeline *Pipeline, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tracking/batch", strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)
	return response
}

func subscribeTracking(t *testing.T, bus *eventbus.MemoryBus) eventbus.Subscription {
	t.Helper()
	subscription, err := bus.Subscribe(context.Background(), eventbus.SubscribeOptions{Topic: "track.reported"})
	if err != nil {
		t.Fatalf("subscribe track.reported: %v", err)
	}
	t.Cleanup(func() { _ = subscription.Close() })
	return subscription
}

func receiveTrackingEvent(t *testing.T, subscription eventbus.Subscription) eventbus.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	delivery, err := subscription.Receive(ctx)
	if err != nil {
		t.Fatalf("receive published event: %v", err)
	}
	return delivery.Event
}

func trackingCounts(t *testing.T, response *httptest.ResponseRecorder) (int, int, int) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Accepted   int `json:"accepted"`
			Duplicated int `json:"duplicated"`
			Rejected   int `json:"rejected"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 0 {
		t.Fatalf("envelope code = %d, want success", envelope.Code)
	}
	return envelope.Data.Accepted, envelope.Data.Duplicated, envelope.Data.Rejected
}