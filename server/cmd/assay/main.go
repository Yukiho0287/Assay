package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Yukiho0287/assay/server/internal/auth"
	"github.com/Yukiho0287/assay/server/internal/config"
	"github.com/Yukiho0287/assay/server/internal/db"
	"github.com/Yukiho0287/assay/server/internal/httpserver"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("配置加载失败", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("数据库初始化失败", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := auth.EnsureAdmin(ctx, db.New(pool), log, cfg.AdminPassword); err != nil {
		log.Error("初始管理员引导失败", "err", err)
		os.Exit(1)
	}

	if err := httpserver.New(cfg.Addr, log, pool).Run(ctx); err != nil {
		log.Error("http server exited", "err", err)
		os.Exit(1)
	}
	log.Info("shutdown complete")
}
