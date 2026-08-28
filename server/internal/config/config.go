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
}

// Load 从环境变量加载配置并校验。
// 目前仅有 ADDR（默认 :8080）；后续新增配置项必须在此处校验。
func Load() (*Config, error) {
	cfg := &Config{
		Addr: getenv("ASSAY_ADDR", ":8080"),
	}
	if cfg.Addr == "" {
		return nil, fmt.Errorf("ASSAY_ADDR 不能为空")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
