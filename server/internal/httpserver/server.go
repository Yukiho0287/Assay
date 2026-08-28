// Package httpserver 组装路由与 HTTP 服务。
// 业务路由一律挂在 /api 下、由 OpenAPI 契约生成的接口约束（契约优先）。
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/db"
)

type Server struct {
	http *http.Server
	log  *slog.Logger
}

func New(addr string, log *slog.Logger, pool *pgxpool.Pool) *Server {
	mux := http.NewServeMux()
	h := &handlers{log: log, q: db.New(pool)}
	api.HandlerFromMuxWithBaseURL(h, mux, "/api")

	return &Server{
		http: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		},
		log: log,
	}
}

// Run 启动服务并阻塞，ctx 取消后优雅退出。
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", "addr", s.http.Addr)
		if err := s.http.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
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
