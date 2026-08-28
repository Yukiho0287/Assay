// Package tasks 任务队列与执行：River（PG 原生队列，入队与任务行同事务）+ 进程内 worker
// + pg_notify 进度通知。API 层的 SSE broker LISTEN 同一通道原样转发给前端。
package tasks

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NotifyChannel 与 httpserver SSE broker 约定的 pg_notify 通道名。
const NotifyChannel = "assay_task_events"

// Event 进度事件载荷，字段名与契约 TaskProgressEvent 完全一致，SSE 层不做转换直接转发。
type Event struct {
	TaskID uuid.UUID `json:"taskId"`
	Status string    `json:"status"`
	Done   int       `json:"done"`
	Total  int       `json:"total"`
}

// NotifyEvent 发进度通知。失败不致命（SSE 断线重连会从 DB 补全快照），只记日志。
func NotifyEvent(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, ev Event) {
	payload, err := json.Marshal(ev)
	if err != nil {
		log.Error("进度事件序列化失败", "task", ev.TaskID, "err", err)
		return
	}
	if _, err := pool.Exec(ctx, "select pg_notify($1, $2)", NotifyChannel, string(payload)); err != nil {
		log.Warn("进度通知发送失败", "task", ev.TaskID, "err", err)
	}
}
