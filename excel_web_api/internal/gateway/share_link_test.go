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
)

type shareLinkStoreFake struct {
	bases   map[string]string
	baseErr error
	setErr  error
	sets    map[string]string

	resolveCalls int
	setCalls     int
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
		wantStatus int
		wantLoc    string
		wantSet    map[string]string
		wantExch   int
	}{
		{
			name:       "unauthenticated redirects to register with token",
			fake:       &shareLinkStoreFake{},
			iam:        &pipelineIamRPC{},
			wantStatus: http.StatusFound,
			wantLoc:    "/register?link_token=" + url.QueryEscape("tok_abc"),
			wantExch:   0,
		},
		{
			name:       "cache hit redirects to base without calling rpc",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{bases: map[string]string{"tok_abc": "base_1"}},
			iam:        &pipelineIamRPC{},
			wantStatus: http.StatusFound,
			wantLoc:    "/bases/base_1",
			wantExch:   0,
		},
		{
			name:       "cache miss resolves via exchange session and writes cache",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{},
			iam:        &pipelineIamRPC{exchangeResponse: linkSession("base_2", "tok_abc")},
			wantStatus: http.StatusFound,
			wantLoc:    "/bases/base_2",
			wantSet:    map[string]string{"tok_abc": "base_2"},
			wantExch:   1,
		},
		{
			name:       "unknown token redirects to guide page",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{},
			iam:        &pipelineIamRPC{exchangeResponse: allowedSession("user_1", "")},
			wantStatus: http.StatusFound,
			wantLoc:    "/err?kind=share-link-unavailable",
			wantExch:   1,
		},
		{
			name:       "session exchange failure redirects to guide page",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{},
			iam:        &pipelineIamRPC{exchangeErr: errors.New("expired")},
			wantStatus: http.StatusFound,
			wantLoc:    "/err?kind=share-link-unavailable",
			wantExch:   1,
		},
		{
			name:       "cache read failure fails open to exchange session",
			session:    "sess_1",
			fake:       &shareLinkStoreFake{baseErr: context.Canceled},
			iam:        &pipelineIamRPC{exchangeResponse: linkSession("base_3", "tok_abc")},
			wantStatus: http.StatusFound,
			wantLoc:    "/bases/base_3",
			wantSet:    map[string]string{"tok_abc": "base_3"},
			wantExch:   1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newShareLinkHandler(test.iam, test.fake, nil)
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
	shareLink := newShareLinkHandler(iam, &shareLinkStoreFake{}, nil)
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

	if response.Code != http.StatusFound || response.Header().Get("Location") != "/bases/base_1" {
		t.Fatalf("share link route = HTTP %d %q, want 302 /bases/base_1", response.Code, response.Header().Get("Location"))
	}

	root := httptest.NewRequest(http.MethodGet, "/", nil)
	rootResponse := httptest.NewRecorder()
	mux.ServeHTTP(rootResponse, root)
	if rootResponse.Code != http.StatusOK || rootResponse.Body.String() != "static" {
		t.Fatalf("static fallback = HTTP %d %q, want 200 static", rootResponse.Code, rootResponse.Body.String())
	}
}

func TestResolvePrincipalRemembersLinkContext(t *testing.T) {
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
	if store.sets["tok_abc"] != "base_9" {
		t.Fatalf("link context cache = %v, want tok_abc -> base_9", store.sets)
	}
}
