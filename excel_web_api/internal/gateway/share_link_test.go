package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
)

type shareLinkStoreFake struct {
	bases     map[string]string
	baseErr   error
	setErr    error
	sets      map[string]string
	deleteErr error
	deletes   []string

	resolveCalls int
	setCalls     int
	deleteCalls  int
}

func (fake *shareLinkStoreFake) DeleteShareLinkBase(ctx context.Context, token string) error {
	fake.deleteCalls++
	fake.deletes = append(fake.deletes, token)
	return fake.deleteErr
}

func (fake *shareLinkStoreFake) ShareLinkBase(ctx context.Context, token string) (string, error) {
	fake.resolveCalls++
	if fake.baseErr != nil {
		return "", fake.baseErr
	}
	if fake.bases == nil {
		return "", nil
	}
	return fake.bases[token], nil
}

func (fake *shareLinkStoreFake) SetShareLinkBase(ctx context.Context, token, baseID string) error {
	fake.setCalls++
	if fake.setErr != nil {
		return fake.setErr
	}
	if fake.sets == nil {
		fake.sets = map[string]string{}
	}
	fake.sets[token] = baseID
	return nil
}

func linkSession(baseID, token string) *iamv1.ExchangeSessionResponse {
	return &iamv1.ExchangeSessionResponse{
		Principal: &commonv1.Principal{
			UserId:   "user_1",
			Internal: false,
			LinkContext: &commonv1.LinkContext{
				BaseId:    baseID,
				LinkToken: token,
				Role:      "viewer",
			},
		},
	}
}

func shareLinkRequest(path string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.SetPathValue("link_token", "tok_abc")
	return request
}

func TestShareLinkEntry(t *testing.T) {
	tests := []struct {
		name       string
		session    string
		fake       *shareLinkStoreFake
		iam        *pipelineIamRPC
		share      *pipelineShareRPC
		wantStatus int
		wantLoc    string
		wantSet    map[string]string
		wantExch   int
	}{
		{
			name:       "unauthenticated redirects to register with token",
			fake:       &shareLinkStoreFake{},
			iam:        &pipelineIamRPC{},
			share:      &pipelineShareRPC{},
			wantStatus: http.StatusFound,
			wantLoc:    "/register?link_token=" + url.QueryEscape("tok_abc"),
			wantExch:   0,
		},
		{
			name:       "cache hit redirects to base without calling rpc",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{bases: map[string]string{"tok_abc": "base_1"}},
			iam:        &pipelineIamRPC{},
			share:      &pipelineShareRPC{},
			wantStatus: http.StatusFound,
			wantLoc:    "/base/base_1",
			wantExch:   0,
		},
		{
			name:       "cache miss verifies with core and writes cache",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{},
			iam:        &pipelineIamRPC{exchangeResponse: linkSession("base_2", "tok_abc")},
			share:      &pipelineShareRPC{checkResponse: allowedDecision(false)},
			wantStatus: http.StatusFound,
			wantLoc:    "/base/base_2",
			wantSet:    map[string]string{"tok_abc": "base_2"},
			wantExch:   1,
		},
		{
			name:       "revoked token falls back to guide page on cache miss",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{},
			iam:        &pipelineIamRPC{exchangeResponse: linkSession("base_2", "tok_abc")},
			share:      &pipelineShareRPC{checkResponse: &sharev1.CheckAccessResponse{Allow: false}},
			wantStatus: http.StatusFound,
			wantLoc:    "/err?kind=share-link-unavailable",
			wantExch:   1,
		},
		{
			name:       "core verification failure redirects to guide page",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{},
			iam:        &pipelineIamRPC{exchangeResponse: linkSession("base_2", "tok_abc")},
			share:      &pipelineShareRPC{checkErr: errors.New("unavailable")},
			wantStatus: http.StatusFound,
			wantLoc:    "/err?kind=share-link-unavailable",
			wantExch:   1,
		},
		{
			name:       "unknown token redirects to guide page",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{},
			iam:        &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "")},
			share:      &pipelineShareRPC{},
			wantStatus: http.StatusFound,
			wantLoc:    "/err?kind=share-link-unavailable",
			wantExch:   1,
		},
		{
			name:       "session exchange failure redirects to guide page",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{},
			iam:        &pipelineIamRPC{exchangeErr: errors.New("expired")},
			share:      &pipelineShareRPC{},
			wantStatus: http.StatusFound,
			wantLoc:    "/err?kind=share-link-unavailable",
			wantExch:   1,
		},
		{
			name:       "cache read failure fails open to exchange session",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{baseErr: context.Canceled},
			iam:        &pipelineIamRPC{exchangeResponse: linkSession("base_3", "tok_abc")},
			share:      &pipelineShareRPC{checkResponse: allowedDecision(false)},
			wantStatus: http.StatusFound,
			wantLoc:    "/base/base_3",
			wantSet:    map[string]string{"tok_abc": "base_3"},
			wantExch:   1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newShareLinkHandler(test.iam, test.share, test.fake, nil)
			request := shareLinkRequest("/s/tok_abc")
			if test.session != "" {
				request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: test.session})
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if location := response.Header().Get("Location"); location != test.wantLoc {
				t.Fatalf("Location = %q, want %q", location, test.wantLoc)
			}
			if got := test.iam.exchangeCalls; got != test.wantExch {
				t.Fatalf("ExchangeSession calls = %d, want %d", got, test.wantExch)
			}
			if test.wantSet != nil {
				if len(test.fake.sets) != len(test.wantSet) {
					t.Fatalf("cached sets = %v, want %v", test.fake.sets, test.wantSet)
				}
				for token, baseID := range test.wantSet {
					if test.fake.sets[token] != baseID {
						t.Fatalf("cached set[%q] = %q, want %q", token, test.fake.sets[token], baseID)
					}
				}
			}
		})
	}
}

func TestShareLinkEntryCoexistsWithStaticFallback(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: linkSession("base_1", "tok_abc")}
	share := &pipelineShareRPC{checkResponse: allowedDecision(false)}
	shareLink := newShareLinkHandler(iam, share, &shareLinkStoreFake{}, nil)
	fallback := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("static"))
	})

	mux := http.NewServeMux()
	mux.Handle("/", fallback)
	mux.Handle("/s/{link_token}", shareLink)

	request := httptest.NewRequest(http.MethodGet, "/s/tok_abc", nil)
	request.SetPathValue("link_token", "tok_abc")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess_1"})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusFound || response.Header().Get("Location") != "/base/base_1" {
		t.Fatalf("share link route = HTTP %d %q, want 302 /base/base_1", response.Code, response.Header().Get("Location"))
	}

	root := httptest.NewRequest(http.MethodGet, "/", nil)
	rootResponse := httptest.NewRecorder()
	mux.ServeHTTP(rootResponse, root)
	if rootResponse.Code != http.StatusOK || rootResponse.Body.String() != "static" {
		t.Fatalf("static fallback = HTTP %d %q, want 200 static", rootResponse.Code, rootResponse.Body.String())
	}
}

// TestResolvePrincipalDoesNotSeedShareLinkCache 锁 ticket 05/07 的失效语义：
// 失效判断只能靠回源，会话里的 link_context 是注册快照（链接重置/关闭后仍在），
// 若按会话回填 token→Base 缓存，已删除的旧 token 会被重新写入并跳过回源校验，
// 旧链再次跳进 Base。缓存只允许由校验通过的 /s/{token} 入口写入。
func TestResolvePrincipalDoesNotSeedShareLinkCache(t *testing.T) {
	store := &shareLinkStoreFake{}
	iam := &pipelineIamRPC{exchangeResponse: linkSession("base_9", "tok_abc")}
	pipeline := NewPipeline(iam, nil, nil, nil)
	pipeline.links = store
	businessCalls := 0
	pipeline.SessionRequired(http.MethodGet, "/api/v1/link-aware", http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) {
			businessCalls++
		},
	))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/link-aware", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess_1"})
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || businessCalls != 1 {
		t.Fatalf("business route = HTTP %d calls %d, want 200 and one", response.Code, businessCalls)
	}
	if store.setCalls != 0 || len(store.sets) != 0 {
		t.Fatalf("share link cache writes = %v, want none outside the verified entry", store.sets)
	}
}
