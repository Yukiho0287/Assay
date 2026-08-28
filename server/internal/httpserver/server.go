// Package httpserver 组装路由与 HTTP 服务。
// 业务路由一律挂在 /api 下、由 OpenAPI 契约生成的接口约束（契约优先）。
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/db"
	"github.com/Yukiho0287/assay/server/internal/tasks"
	"github.com/Yukiho0287/assay/server/internal/update"
	"github.com/Yukiho0287/assay/server/internal/web"
)

type Server struct {
	http   *http.Server
	log    *slog.Logger
	broker *taskEventBroker
}

func New(addr string, log *slog.Logger, pool *pgxpool.Pool, gh *update.Client, tq *tasks.Client) *Server {
	mux := http.NewServeMux()
	broker := newTaskEventBroker(pool, log)
	h := &handlers{log: log, q: db.New(pool), pool: pool, gh: gh, tq: tq, broker: broker}
	api.HandlerFromMuxWithBaseURL(h, mux, "/api")
	if wh := web.Handler(); wh != nil {
		// 发布构建内嵌前端：非 /api 路径全部交给 SPA
		mux.Handle("/", wh)
	}

	return &Server{
		http: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		},
		log:    log,
		broker: broker,
	}
}

// Run 启动服务并阻塞，ctx 取消后优雅退出。
// ln 非 nil 时复用外部传入的 socket（systemd socket activation，
// 热更新时连接在内核队列排队、不吃拒绝），否则按 addr 自行监听。
func (s *Server) Run(ctx context.Context, ln net.Listener) error {
	go s.broker.run(ctx) // 任务事件 LISTEN 循环与 http 同生命周期

	errCh := make(chan error, 1)
	go func() {
		var err error
		if ln != nil {
			s.log.Info("http server serving on inherited socket", "addr", ln.Addr().String())
			err = s.http.Serve(ln)
		} else {
			s.log.Info("http server listening", "addr", s.http.Addr)
			err = s.http.ListenAndServe()
		}
		if !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
