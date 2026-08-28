//go:build !embedweb

// Package web 在发布构建（-tags embedweb）中内嵌前端产物；
// 开发构建不内嵌，前端由 vite dev server 提供。
package web

import "net/http"

// Handler 开发构建下返回 nil，表示不挂载前端路由。
func Handler() http.Handler { return nil }
