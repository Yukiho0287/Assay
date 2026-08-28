// Package config 集中读取并校验环境变量配置。
// Fail-Fast：缺失或非法的关键配置在启动时立即报错，不允许带病运行。
package config

import (
	"fmt"
	"os"
)

type Config struct {
	// Addr HTTP 监听地址，如 ":8080"
	Addr string
	// DatabaseURL PostgreSQL 连接串（必填），如 postgres://localhost:5432/assay
	DatabaseURL string
	// AdminPassword 初始管理员密码（可选）；为空时首次启动随机生成并打印日志
	AdminPassword string
}

// Load 从环境变量加载配置并校验。新增配置项必须在此处校验。
func Load() (*Config, error) {
	cfg := &Config{
		Addr:          getenv("ASSAY_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("ASSAY_DATABASE_URL"),
		AdminPassword: os.Getenv("ASSAY_ADMIN_PASSWORD"),
	}
	if cfg.Addr == "" {
		return nil, fmt.Errorf("ASSAY_ADDR 不能为空")
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("必须设置 ASSAY_DATABASE_URL（如 postgres://localhost:5432/assay）")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
