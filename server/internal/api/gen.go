// Package api 存放由 api/openapi.yaml 生成的服务端接口与模型。
// api.gen.go 是生成文件，勿手改；改契约后执行 go generate ./...
package api

//go:generate go tool oapi-codegen -config ../../oapi-codegen.yaml ../../../api/openapi.yaml
