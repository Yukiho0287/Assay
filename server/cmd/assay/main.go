package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/coreos/go-systemd/v22/activation"

	"github.com/Yukiho0287/assay/server/internal/auth"
	"github.com/Yukiho0287/assay/server/internal/config"
	"github.com/Yukiho0287/assay/server/internal/db"
	"github.com/Yukiho0287/assay/server/internal/httpserver"
	"github.com/Yukiho0287/assay/server/internal/update"
	"github.com/Yukiho0287/assay/server/internal/version"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		serve()
	case "version":
		fmt.Println(version.Version)
	case "migrate":
		runMigrate()
	default:
		fmt.Fprintf(os.Stderr, "未知命令 %q，可用：serve（默认）| version | migrate pending|up\n", cmd)
		os.Exit(2)
	}
}

// runMigrate 供部署脚本与手工运维使用：
// pending 输出未应用迁移数（0 = 可热重启，>0 = 需备份 + 短停机），up 只跑迁移不起服务。
func runMigrate() {
	sub := ""
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "配置加载失败:", err)
		os.Exit(1)
	}
	switch sub {
	case "pending":
		n, err := db.PendingMigrations(cfg.DatabaseURL)
		if err != nil {
			fmt.Fprintln(os.Stderr, "检查待应用迁移失败:", err)
			os.Exit(1)
		}
		fmt.Println(n)
	case "up":
		if err := db.Migrate(cfg.DatabaseURL); err != nil {
			fmt.Fprintln(os.Stderr, "执行迁移失败:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "用法: assay migrate pending|up")
		os.Exit(2)
	}
}

func serve() {
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

	// systemd socket activation：有传入 socket 就复用，重启期间连接由内核排队不被拒绝
	var ln net.Listener
	if listeners, err := activation.Listeners(); err == nil && len(listeners) > 0 {
		ln = listeners[0]
	}

	gh := update.New(cfg.GitHubRepo, cfg.GitHubToken)

	log.Info("assay 启动", "version", version.Version)
	if err := httpserver.New(cfg.Addr, log, pool, gh).Run(ctx, ln); err != nil {
		log.Error("http server exited", "err", err)
		os.Exit(1)
	}
	log.Info("shutdown complete")
}
