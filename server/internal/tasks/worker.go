package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/Yukiho0287/assay/server/internal/db"
	"github.com/Yukiho0287/assay/server/internal/probe"
	"github.com/Yukiho0287/assay/server/internal/probe/registry"
)

// QualityArgs 质量检测任务的 River 载荷：只带任务 ID，其余全部执行时从 tasks 行现读。
type QualityArgs struct {
	TaskID uuid.UUID `json:"taskId"`
}

func (QualityArgs) Kind() string { return "quality_task" }

func (QualityArgs) InsertOpts() river.InsertOpts {
	// 总尝试 2 次：实例被杀（蓝绿切换/重启）后重启的实例自动重跑一次；
	// 第二次仍失败则 discard，由 Work 里的最终失败分支落任务终态。
	return river.InsertOpts{MaxAttempts: 2}
}

// QualityWorker 质量检测执行器。状态机全程带守卫（MarkTaskRunning/FinishTask 都有
// 状态前置条件），重复投递与中途取消都不会破坏终态。
type QualityWorker struct {
	river.WorkerDefaults[QualityArgs]
	pool   *pgxpool.Pool
	q      *db.Queries
	log    *slog.Logger
	client *http.Client
}

// Timeout -1 关闭 River 的 job 级超时：单任务 408 请求可能跑几十分钟，
// 超时控制在请求级（runner 里每请求 120s）。
func (w *QualityWorker) Timeout(*river.Job[QualityArgs]) time.Duration { return -1 }

func (w *QualityWorker) Work(ctx context.Context, job *river.Job[QualityArgs]) error {
	taskID := job.Args.TaskID
	err := w.work(ctx, taskID)
	if err != nil && job.Attempt >= job.MaxAttempts {
		// 最后一次尝试也失败：job 即将 discard，必须在这里落任务终态，
		// 否则任务永远挂在 running 只能等下次启动的孤儿清扫。
		// 用 WithoutCancel：即使失败原因是 ctx 取消，终态也要写进 DB。
		w.failTask(context.WithoutCancel(ctx), taskID, err)
	}
	return err
}

func (w *QualityWorker) work(ctx context.Context, taskID uuid.UUID) error {
	rows, err := w.q.MarkTaskRunning(ctx, taskID)
	if err != nil {
		return fmt.Errorf("标记任务运行中: %w", err)
	}
	if rows == 0 {
		// 已被取消或已终结（入队后、执行前用户取消）：静默跳过
		w.log.Info("任务已终结，跳过执行", "task", taskID)
		return nil
	}

	task, err := w.q.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("读取任务: %w", err)
	}
	var target probe.Target
	if err := json.Unmarshal(task.Target, &target); err != nil {
		return fmt.Errorf("解析任务快照: %w", err)
	}
	var params probe.Params
	if err := json.Unmarshal(task.Params, &params); err != nil {
		return fmt.Errorf("解析任务参数: %w", err)
	}
	total := int(task.ProgressTotal)
	NotifyEvent(ctx, w.pool, w.log, Event{TaskID: taskID, Status: "running", Done: 0, Total: total})

	// 重试幂等：清掉上次中断留下的半截结果，整个任务从头重跑
	if err := w.q.DeleteTaskCaseResults(ctx, taskID); err != nil {
		return fmt.Errorf("清理旧结果: %w", err)
	}
	if task.ProgressDone != 0 {
		if err := w.q.UpdateTaskProgress(ctx, db.UpdateTaskProgressParams{ID: taskID, ProgressTotal: task.ProgressTotal}); err != nil {
			return fmt.Errorf("重置进度: %w", err)
		}
	}

	// API key 执行时现读（绝不进快照）；渠道已删则任务失败
	if !task.ChannelID.Valid {
		return errors.New("渠道已被删除，无法执行")
	}
	secret, err := w.q.GetChannelSecret(ctx, uuid.UUID(task.ChannelID.Bytes))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("渠道已被删除，无法执行")
		}
		return fmt.Errorf("读取渠道密钥: %w", err)
	}

	// 按注册顺序执行各检测项，offset 把各 probe 的局部进度串成全局进度
	offset := 0
	for _, id := range task.Probes {
		p, ok := registry.Get(id)
		if !ok {
			return fmt.Errorf("未知检测项 %q", id)
		}
		in := probe.RunInput{
			Target: target,
			APIKey: secret.ApiKey,
			Params: params,
			Client: w.client,
			Report: func(ctx context.Context, r probe.CaseResult) error {
				return w.q.UpsertTaskCaseResult(ctx, upsertParams(taskID, r))
			},
			Progress: func(ctx context.Context, done, probeTotal int) {
				global := offset + done
				if err := w.q.UpdateTaskProgress(ctx, db.UpdateTaskProgressParams{
					ID: taskID, ProgressTotal: task.ProgressTotal, ProgressDone: int32(global),
				}); err != nil {
					w.log.Warn("写进度失败", "task", taskID, "err", err)
					return
				}
				NotifyEvent(ctx, w.pool, w.log, Event{TaskID: taskID, Status: "running", Done: global, Total: total})
			},
		}
		if err := p.Run(ctx, in); err != nil {
			return fmt.Errorf("检测项 %s: %w", id, err)
		}
		offset += p.SlotCount(params)
	}

	if _, err := w.q.FinishTask(ctx, db.FinishTaskParams{ID: taskID, Status: "succeeded"}); err != nil {
		return fmt.Errorf("落任务终态: %w", err)
	}
	NotifyEvent(ctx, w.pool, w.log, Event{TaskID: taskID, Status: "succeeded", Done: offset, Total: total})
	w.log.Info("任务完成", "task", taskID, "slots", offset)
	return nil
}

// failTask 把任务标记为 failed 并广播终态。FinishTask 有 running 守卫，
// 已终结的任务（如 MarkTaskRunning 前就被取消）不会被覆盖。
func (w *QualityWorker) failTask(ctx context.Context, taskID uuid.UUID, cause error) {
	msg := truncateError(cause.Error(), 500)
	rows, err := w.q.FinishTask(ctx, db.FinishTaskParams{ID: taskID, Status: "failed", Error: &msg})
	if err != nil {
		w.log.Error("落任务失败终态出错", "task", taskID, "err", err)
		return
	}
	if rows == 0 {
		return
	}
	task, err := w.q.GetTask(ctx, taskID)
	if err != nil {
		w.log.Warn("读任务进度失败，终态通知用零值", "task", taskID, "err", err)
	}
	NotifyEvent(ctx, w.pool, w.log, Event{TaskID: taskID, Status: "failed", Done: int(task.ProgressDone), Total: int(task.ProgressTotal)})
	w.log.Error("任务失败", "task", taskID, "err", cause)
}

// upsertParams CaseResult → sqlc 参数映射。HTTP 状态 0（传输层失败）与未发出请求
// 的延迟落 NULL，报告里区分「没响应」与「响应了 0」。
func upsertParams(taskID uuid.UUID, r probe.CaseResult) db.UpsertTaskCaseResultParams {
	p := db.UpsertTaskCaseResultParams{
		TaskID:          taskID,
		Probe:           r.Probe,
		Suite:           r.Suite,
		Line:            int32(r.Line),
		Mode:            r.Mode,
		SelectionReason: r.SelectionReason,
		Status:          r.Status,
		Message:         r.Message,
		Attempts:        int32(r.Attempts),
	}
	if r.HTTPStatus != 0 {
		p.HttpStatus = pgtype.Int4{Int32: int32(r.HTTPStatus), Valid: true}
	}
	if r.HTTPStatus != 0 || r.LatencyMs > 0 {
		p.LatencyMs = pgtype.Int4{Int32: int32(r.LatencyMs), Valid: true}
	}
	if r.Arguments != "" {
		p.Arguments = &r.Arguments
	}
	return p
}

func truncateError(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit-3]) + "..."
}
