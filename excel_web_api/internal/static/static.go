// Package static 嵌入并托管前端构建产物（web/dist 经构建脚本拷入 dist/）。
// 生产经 go:embed，测试经 fstest.MapFS / os.DirFS 注入同一文件系统接口。
package static

import (
	"embed"
	"io/fs"
)

// dist 目录只提交占位 index.html，保证前端未构建时网关仍可编译、
// 根路径不 500；真实产物由 scripts\build-all.ps1 拷入且被 .gitignore 忽略。
//
//go:embed all:dist
var distTree embed.FS

// DistFS 返回以 dist 为根的嵌入式前端产物文件系统。
func DistFS() fs.FS {
	sub, err := fs.Sub(distTree, "dist")
	if err != nil {
		panic("static: embed dist subtree missing: " + err.Error())
	}
	return sub
}
