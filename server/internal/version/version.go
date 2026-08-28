// Package version 保存构建版本号。
package version

// Version 由发布构建通过 ldflags 注入
// （-X github.com/Yukiho0287/assay/server/internal/version.Version=vX.Y.Z），开发构建保持 dev。
var Version = "dev"
