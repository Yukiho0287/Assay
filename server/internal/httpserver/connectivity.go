package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/connectivity"
	"github.com/Yukiho0287/assay/server/internal/db"
)

func (h *handlers) TestChannel(w http.ResponseWriter, r *http.Request, id api.IdPath) {
	s, ok := h.requirePerm(w, r, "channels")
	if !ok {
		return
	}
	var req api.ConnectivityTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "请求格式错误"})
		return
	}

	c, err := h.q.GetChannelSecret(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, api.Error{Error: "渠道不存在"})
			return
		}
		h.log.Error("查询渠道失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	m, err := h.q.GetChannelModel(r.Context(), db.GetChannelModelParams{ID: req.ModelId, ChannelID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, api.Error{Error: "模型条目不存在"})
			return
		}
		h.log.Error("查询模型条目失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}

	test := api.ConnectivityTest{
		TestedAt: time.Now().UTC(),
		Model:    m.Name,
		Results:  connectivity.RunProbes(r.Context(), c.BaseUrl, c.ApiKey, c.Protocols, m.Name),
	}

	// 双写历史行 + last_test 快照；落库失败即报错（结果没被记住就不能装作成功）
	if err := connectivity.SaveTest(r.Context(), h.q, id, connectivity.SourceManual, test); err != nil {
		h.log.Error("保存测试结果失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "测试已执行但结果保存失败"})
		return
	}
	h.log.Info("连通测试", "channel", id.String(), "model", m.Name, "operator", s.Username)
	writeJSON(w, http.StatusOK, test)
}

// GetChannelConnectivityHistory 连通历史（总览延迟曲线数据源）：登录即可，不含任何凭证信息
func (h *handlers) GetChannelConnectivityHistory(w http.ResponseWriter, r *http.Request, id api.IdPath, params api.GetChannelConnectivityHistoryParams) {
	if _, ok := h.requireAuth(w, r); !ok {
		return
	}
	hours := 24
	if params.Hours != nil {
		hours = min(max(*params.Hours, 1), 168)
	}
	if _, err := h.q.GetChannel(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, api.Error{Error: "渠道不存在"})
			return
		}
		h.log.Error("查询渠道失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	rows, err := h.q.ListConnectivityHistory(r.Context(), db.ListConnectivityHistoryParams{ChannelID: id, Hours: int32(hours)})
	if err != nil {
		h.log.Error("查询连通历史失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	items := make([]api.ConnectivityHistoryPoint, 0, len(rows))
	for _, row := range rows {
		p := api.ConnectivityHistoryPoint{
			TestedAt: row.TestedAt,
			Model:    row.Model,
			Source:   api.ConnectivitySource(row.Source),
			Protocol: api.Protocol(row.Protocol),
			Ok:       row.Ok,
		}
		if row.TtftMs.Valid {
			v := int(row.TtftMs.Int32)
			p.TtftMs = &v
		}
		items = append(items, p)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []api.ConnectivityHistoryPoint `json:"items"`
	}{items})
}
