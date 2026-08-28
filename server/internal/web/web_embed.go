//go:build embedweb

package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist 由发布流水线在构建前拷贝 web/dist 到本目录（不入库）。
//
//go:embed all:dist
var dist embed.FS

// Handler 返回内嵌前端的处理器：静态文件直出，未知路径回退 index.html 交给前端路由。
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // embed 内容编译期确定，此处失败属构建错误
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, err := fs.Stat(sub, p); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA 回退：/login、/channels 等前端路由一律回 index.html
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}
