package static_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"excel_web_api/internal/static"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":              {Data: []byte("SPA INDEX")},
		"assets/app.3f9a2b7c.js":  {Data: []byte("console.log(1)")},
		"assets/app.3f9a2b7c.css": {Data: []byte("body{}")},
	}
}

func get(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestHandlerServesEntryAtRoot(t *testing.T) {
	recorder := get(t, static.NewHandler(testFS()), "/")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); body != "SPA INDEX" {
		t.Fatalf("body = %q, want %q", body, "SPA INDEX")
	}
	if cc := recorder.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}
}

func TestHandlerFallsBackForDeepLink(t *testing.T) {
	recorder := get(t, static.NewHandler(testFS()), "/bases/abc")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); body != "SPA INDEX" {
		t.Fatalf("body = %q, want %q", body, "SPA INDEX")
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
}

func TestHandlerServesHashedAssetImmutable(t *testing.T) {
	recorder := get(t, static.NewHandler(testFS()), "/assets/app.3f9a2b7c.js")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); body != "console.log(1)" {
		t.Fatalf("body = %q, want %q", body, "console.log(1)")
	}
	if cc := recorder.Header().Get("Cache-Control"); cc != "public,max-age=31536000,immutable" {
		t.Fatalf("Cache-Control = %q, want immutable long cache", cc)
	}
}

func TestHandler404sMissingAsset(t *testing.T) {
	recorder := get(t, static.NewHandler(testFS()), "/assets/nope.3f9a2b7c.js")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestMountPreservesAPIOtherRoutesPriority(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/", func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte("API"))
	})
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte("HEALTH"))
	})
	mux.HandleFunc("/bases/{base_id}/tables/{table_id}/ws", func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte("WS:" + request.PathValue("base_id") + ":" + request.PathValue("table_id")))
	})
	mux.HandleFunc("/s/{link_token}", func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte("SHARE:" + request.PathValue("link_token")))
	})
	static.Mount(mux, testFS())

	if recorder := get(t, mux, "/api/v1/users"); recorder.Body.String() != "API" {
		t.Fatalf("/api/v1/users body = %q, want API（静态回退吞了 API）", recorder.Body.String())
	}
	if recorder := get(t, mux, "/healthz"); recorder.Body.String() != "HEALTH" {
		t.Fatalf("/healthz body = %q, want HEALTH", recorder.Body.String())
	}
	if recorder := get(t, mux, "/bases/b1/tables/t1/ws"); recorder.Body.String() != "WS:b1:t1" {
		t.Fatalf("ws body = %q, want WS:b1:t1（静态回退吞了 WS 或 PathValue 失效）", recorder.Body.String())
	}
	if recorder := get(t, mux, "/s/tok123"); recorder.Body.String() != "SHARE:tok123" {
		t.Fatalf("/s/{link_token} body = %q, want SHARE:tok123（静态回退吞了分享入口）", recorder.Body.String())
	}
	if recorder := get(t, mux, "/bases/abc"); recorder.Body.String() != "SPA INDEX" {
		t.Fatalf("/bases/abc body = %q, want SPA INDEX（深链未回退入口）", recorder.Body.String())
	}
}

func TestDistFSIncludesPlaceholder(t *testing.T) {
	data, err := fs.ReadFile(static.DistFS(), "index.html")
	if err != nil {
		t.Fatalf("占位 index.html 不可读: %v（前端未构建时应仍可编译）", err)
	}
	if len(data) == 0 {
		t.Fatal("占位 index.html 为空")
	}
}
