package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/tasks"
)

// taskEventBroker 用独立 PG 连接 LISTEN 任务进度通道，按 taskId 分发给 SSE 订阅者。
// 走 pg_notify 而不是进程内直发：蓝绿双实例期间事件由哪个实例产生都能到达所有前端。
type taskEventBroker struct {
	pool *pgxpool.Pool
	log  *slog.Logger

	mu   sync.Mutex
	subs map[uuid.UUID]map[chan string]struct{}
}

func newTaskEventBroker(pool *pgxpool.Pool, log *slog.Logger) *taskEventBroker {
	return &taskEventBroker{pool: pool, log: log, subs: map[uuid.UUID]map[chan string]struct{}{}}
}

// notify API 侧动作（如取消）产生的事件也走 pg_notify，与 worker 同一条链路。
func (b *taskEventBroker) notify(ctx context.Context, taskID uuid.UUID, status string, done, total int) {
	tasks.NotifyEvent(ctx, b.pool, b.log, tasks.Event{TaskID: taskID, Status: status, Done: done, Total: total})
}

// subscribe 订阅一个任务的事件流，返回只收通道与退订函数。
// 通道带缓冲，写满即丢（进度事件后来的覆盖先来的；断线重连有 DB 快照兜底）。
func (b *taskEventBroker) subscribe(taskID uuid.UUID) (<-chan string, func()) {
	ch := make(chan string, 64)
	b.mu.Lock()
	if b.subs[taskID] == nil {
		b.subs[taskID] = map[chan string]struct{}{}
	}
	b.subs[taskID][ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs[taskID], ch)
		if len(b.subs[taskID]) == 0 {
			delete(b.subs, taskID)
		}
		b.mu.Unlock()
	}
}

// run LISTEN 主循环：连接断了退避一秒重连，ctx 取消退出。
func (b *taskEventBroker) run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := b.listen(ctx); err != nil && ctx.Err() == nil {
			b.log.Warn("任务事件监听中断，1s 后重连", "err", err)
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
			}
		}
	}
}

func (b *taskEventBroker) listen(ctx context.Context) error {
	// LISTEN 需要独占连接；不从池里借（还回去会把订阅状态污染给下个使用者），单开一条
	conn, err := pgx.ConnectConfig(ctx, b.pool.Config().ConnConfig.Copy())
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, "listen "+tasks.NotifyChannel); err != nil {
		return err
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		b.dispatch(n.Payload)
	}
}

func (b *taskEventBroker) dispatch(payload string) {
	var ev struct {
		TaskID uuid.UUID `json:"taskId"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		b.log.Warn("任务事件载荷不可解析，丢弃", "err", err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[ev.TaskID] {
		select {
		case ch <- payload:
		default: // 订阅者写太慢：丢弃，前端靠后续事件/重连快照追平
		}
	}
}

func isTerminalStatus(s string) bool {
	return s == "succeeded" || s == "failed" || s == "canceled"
}

func (h *handlers) StreamQualityTaskEvents(w http.ResponseWriter, r *http.Request, id api.IdPath) {
	if _, ok := h.requirePerm(w, r, "quality"); !ok {
		return
	}
	if _, ok := h.loadQualityTask(w, r, id); !ok {
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务不支持事件流"})
		return
	}

	// 先订阅、再读快照：快照与订阅之间不留事件真空
	ch, cancel := h.broker.subscribe(id)
	defer cancel()
	task, err := h.q.GetTask(r.Context(), id)
	if err != nil {
		h.internalError(w, "读取任务快照失败", err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // OpenResty：关代理缓冲，事件即时下发
	w.WriteHeader(http.StatusOK)

	snapshot, err := json.Marshal(tasks.Event{
		TaskID: task.ID,
		Status: task.Status,
		Done:   int(task.ProgressDone),
		Total:  int(task.ProgressTotal),
	})
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", snapshot)
	fl.Flush()
	if isTerminalStatus(task.Status) {
		return
	}

	// 15s 心跳注释帧：撑住 OpenResty 默认 60s 读超时
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case payload := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", payload)
			fl.Flush()
			var ev struct {
				Status string `json:"status"`
			}
			if json.Unmarshal([]byte(payload), &ev) == nil && isTerminalStatus(ev.Status) {
				return
			}
		}
	}
}
