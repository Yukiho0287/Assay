package connectivity

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/db"
)

const tickInterval = time.Minute

// Scheduler 定时探活：每分钟扫一次到期渠道（已配间隔 + 未停用 + 探活模型仍在），
// 逐渠道串行探测（自限流），结果与手动测试走同一 SaveTest 路径；tick 末清理 7 天前的历史。
// 蓝绿双实例期间双跑无害：多一个数据点 + 重复清理而已。
type Scheduler struct {
	q   *db.Queries
	log *slog.Logger
}

func NewScheduler(pool *pgxpool.Pool, log *slog.Logger) *Scheduler {
	return &Scheduler{q: db.New(pool), log: log}
}

// Run 阻塞执行直到 ctx 取消；调用方用 go 起协程
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	due, err := s.q.ListDueProbeChannels(ctx)
	if err != nil {
		s.log.Error("查询到期探活渠道失败", "err", err)
		return
	}
	for _, ch := range due {
		if ctx.Err() != nil {
			return
		}
		s.probeChannel(ctx, ch)
	}
	if n, err := s.q.DeleteOldConnectivityResults(ctx); err != nil {
		s.log.Error("清理过期连通历史失败", "err", err)
	} else if n > 0 {
		s.log.Info("清理过期连通历史", "rows", n)
	}
}

// probeChannel 单渠道探测 + 落库；失败只记日志不中断本 tick 其余渠道
func (s *Scheduler) probeChannel(ctx context.Context, ch db.ListDueProbeChannelsRow) {
	c, err := s.q.GetChannelSecret(ctx, ch.ID)
	if err != nil {
		s.log.Error("定时探活读取渠道失败", "channel", ch.ID.String(), "err", err)
		return
	}
	test := api.ConnectivityTest{
		TestedAt: time.Now().UTC(),
		Model:    ch.ModelName,
		Results:  RunProbes(ctx, c.BaseUrl, c.ApiKey, c.Protocols, ch.ModelName),
	}
	if err := SaveTest(ctx, s.q, ch.ID, SourceScheduled, test); err != nil {
		s.log.Error("定时探活结果落库失败", "channel", ch.ID.String(), "err", err)
		return
	}
	s.log.Info("定时探活", "channel", ch.ID.String(), "model", ch.ModelName)
}
