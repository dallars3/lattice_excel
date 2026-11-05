package static

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"
)

const (
	entryFile             = "index.html"
	immutableCacheControl = "public,max-age=31536000,immutable"
	entryCacheControl     = "no-cache"
)

// hashedAsset 匹配以「.<内容hash>.<扩展名>」结尾的资产（如 assets/app.3f9a2b7c.js）。
// Vite 产物命名 [name].[hash][extname] 与其对齐（见 web/vite.config.ts）。
var hashedAsset = regexp.MustCompile(`\.[0-9a-fA-F]{8,}\.[A-Za-z0-9]+$`)

// Handler 从 fsys 提供静态文件，并对无扩展名的导航路径回退 index.html，
// 以支撑 history 路由深链。挂载于 ServeMux 的 "/" 兜底，更具体模式（API/WS）优先。
type Handler struct {
	fsys fs.FS
}

// NewHandler 以给定文件系统构造静态托管处理器。
func NewHandler(fsys fs.FS) http.Handler {
	return &Handler{fsys: fsys}
}

// Mount 把静态兜底注册到 mux 的 "/"，更具体模式（/api/v1/、/healthz、WS）优先命中。
func Mount(mux *http.ServeMux, fsys fs.FS) {
	mux.Handle("/", NewHandler(fsys))
}

func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+request.URL.Path), "/")
	if name == "" {
		name = entryFile
	}
	if file, err := h.fsys.Open(name); err == nil {
		if stat, statErr := file.Stat(); statErr == nil && !stat.IsDir() {
			_ = file.Close()
			h.serveFile(response, request, name)
			return
		}
		_ = file.Close()
	}
	// 缺文件时：带扩展名的资源请求一律 404；导航路径回退入口文档。
	if path.Ext(name) != "" {
		http.NotFound(response, request)
		return
	}
	h.serveFile(response, request, entryFile)
}

func (h *Handler) serveFile(response http.ResponseWriter, request *http.Request, name string) {
	data, err := fs.ReadFile(h.fsys, name)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		response.Header().Set("Content-Type", contentType)
	}
	if hashedAsset.MatchString(name) {
		response.Header().Set("Cache-Control", immutableCacheControl)
	} else {
		response.Header().Set("Cache-Control", entryCacheControl)
	}
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodGet {
		_, _ = response.Write(data)
	}
}
