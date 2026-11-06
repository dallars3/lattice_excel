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
		"index.html":                      {Data: []byte("SPA INDEX")},
		"assets/app.3f9a2b7c.js":          {Data: []byte("console.log(1)")},
		"assets/app.3f9a2b7c.css":         {Data: []byte("body{}")},
		"assets/index.D3YJoxEW.js":        {Data: []byte("JS")},
		"assets/index.CrMK72br.css":       {Data: []byte("CSS")},
		"assets/vendor-react.B6bjA8E9.js": {Data: []byte("VJS")},
		"assets/logo.png":                 {Data: []byte("PNG")},
		"favicon.ico":                     {Data: []byte("ICO")},
		"legacy.js":                       {Data: []byte("legacy")},
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
	// 真实 Vite 产物 hash 为 base64url 片段（含大小写字母/-/_），不是纯 hex；
	// 缺陷 #21 的回归面：混合大小写 hash（如 index.CrMK72br.css）必须命中长缓存。
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "entry chunk hex hash", path: "/assets/app.3f9a2b7c.js", body: "console.log(1)"},
		{name: "entry chunk mixed-case hash", path: "/assets/index.D3YJoxEW.js", body: "JS"},
		{name: "css mixed-case hash", path: "/assets/index.CrMK72br.css", body: "CSS"},
		{name: "manual chunk with dash", path: "/assets/vendor-react.B6bjA8E9.js", body: "VJS"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := get(t, static.NewHandler(testFS()), testCase.path)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			if body := recorder.Body.String(); body != testCase.body {
				t.Fatalf("body = %q, want %q", body, testCase.body)
			}
			if cc := recorder.Header().Get("Cache-Control"); cc != "public,max-age=31536000,immutable" {
				t.Fatalf("Cache-Control = %q, want immutable long cache", cc)
			}
		})
	}
}

func TestHandlerServesNonHashedAssetNoCache(t *testing.T) {
	// 缺陷 #21 收口面之一：无内容 hash 的资源（public/ 产物、根图标等）不得误享长缓存。
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "asset dir without hash", path: "/assets/logo.png", body: "PNG"},
		{name: "root icon", path: "/favicon.ico", body: "ICO"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := get(t, static.NewHandler(testFS()), testCase.path)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			if body := recorder.Body.String(); body != testCase.body {
				t.Fatalf("body = %q, want %q", body, testCase.body)
			}
			if cc := recorder.Header().Get("Cache-Control"); cc != "no-cache" {
				t.Fatalf("Cache-Control = %q, want no-cache（无 hash 资源不得长缓存）", cc)
			}
		})
	}
}

func TestHandlerServesNonAssetDocumentNoCache(t *testing.T) {
	// 无 hash 的 .js/.css（如手工放置的 legacy 脚本）同样保持 no-cache，避免误判长缓存。
	recorder := get(t, static.NewHandler(testFS()), "/legacy.js")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if cc := recorder.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
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
