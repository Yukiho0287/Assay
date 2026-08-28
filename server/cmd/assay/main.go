package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Yukiho0287/assay/server/internal/config"
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

	if err := httpserver.New(cfg.Addr, log).Run(ctx); err != nil {
		log.Error("http server exited", "err", err)
		os.Exit(1)
	}
	log.Info("shutdown complete")
}
