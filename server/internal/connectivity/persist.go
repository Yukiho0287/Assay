package connectivity

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/db"
)

// 探测来源：手动测试与定时探活共用同一落库路径，靠 source 区分节奏归属
// （调度器判「到期」只看 scheduled 行，手动测试不重置定时节奏）
const (
	SourceManual    = "manual"
	SourceScheduled = "scheduled"
)

// SaveTest 测试结果双写：逐协议行入历史表 + 覆盖渠道 last_test 快照（列表页状态点沿用）。
// 同一次测试各协议行共享 test.TestedAt，前端按时间戳分组即可还原一次测试。
// 不包事务：最坏是快照比历史旧一拍，无害；任一写失败返回错误（结果没被记住不能装作成功）。
func SaveTest(ctx context.Context, q *db.Queries, channelID uuid.UUID, source string, test api.ConnectivityTest) error {
	for _, r := range test.Results {
		p := db.InsertConnectivityResultParams{
			ChannelID: channelID,
			Model:     test.Model,
			Source:    source,
			Protocol:  string(r.Protocol),
			Ok:        r.Ok,
			Error:     r.Error,
			TestedAt:  test.TestedAt,
		}
		if r.Status != nil {
			p.HttpStatus = pgtype.Int4{Int32: int32(*r.Status), Valid: true}
		}
		if r.TtftMs != nil {
			p.TtftMs = pgtype.Int4{Int32: int32(*r.TtftMs), Valid: true}
		}
		if err := q.InsertConnectivityResult(ctx, p); err != nil {
			return fmt.Errorf("写入连通历史: %w", err)
		}
	}
	raw, err := json.Marshal(test)
	if err != nil {
		return fmt.Errorf("序列化测试结果: %w", err)
	}
	if _, err := q.UpdateChannelLastTest(ctx, db.UpdateChannelLastTestParams{ID: channelID, LastTest: raw}); err != nil {
		return fmt.Errorf("更新渠道快照: %w", err)
	}
	return nil
}
