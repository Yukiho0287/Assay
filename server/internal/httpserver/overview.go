package httpserver

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/db"
)

// ListOverviewChannels 总览渠道卡片：渠道基本信息 + 各模型最近终态质量任务的即时得分。
// 与任务列表共用同一批量聚合查询与 scoreProbes 内核，口径与任务报告完全一致；得分绝不持久化。
func (h *handlers) ListOverviewChannels(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuth(w, r); !ok {
		return
	}
	channels, err := h.q.ListChannels(r.Context())
	if err != nil {
		h.log.Error("查询渠道列表失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	tasks, err := h.q.ListLatestTerminalQualityTasks(r.Context())
	if err != nil {
		h.log.Error("查询最近终态任务失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}

	// 一次批量聚合拿全部任务的用例计数，再逐任务喂 scoreProbes
	countsByTask := make(map[uuid.UUID][]caseCount, len(tasks))
	if len(tasks) > 0 {
		ids := make([]uuid.UUID, len(tasks))
		for i, t := range tasks {
			ids[i] = t.ID
		}
		countRows, err := h.q.AggregateCaseResultsByTaskIDs(r.Context(), ids)
		if err != nil {
			h.log.Error("聚合任务用例计数失败", "err", err)
			writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
			return
		}
		for _, c := range countRows {
			countsByTask[c.TaskID] = append(countsByTask[c.TaskID],
				caseCount{Probe: c.Probe, Suite: c.Suite, Mode: c.Mode, Status: c.Status, N: int(c.N)})
		}
	}

	// 任务按渠道分桶成模型得分行（查询已按 渠道×模型名 去重取最近）
	byChannel := make(map[uuid.UUID][]api.OverviewModelScore)
	for _, t := range tasks {
		var target api.TaskTarget
		if err := json.Unmarshal(t.Target, &target); err != nil {
			h.log.Error("任务快照数据损坏", "task", t.ID.String(), "err", err)
			continue
		}
		row := api.OverviewModelScore{
			Model:        target.Model,
			ModelEntryId: target.ModelEntryId,
			TaskId:       t.ID,
			TaskStatus:   api.TaskStatus(t.Status),
		}
		if t.FinishedAt.Valid {
			ts := t.FinishedAt.Time
			row.FinishedAt = &ts
		}
		_, row.Score = scoreProbes(t.Probes, countsByTask[t.ID])
		cid := uuid.UUID(t.ChannelID.Bytes)
		byChannel[cid] = append(byChannel[cid], row)
	}

	items := make([]api.OverviewChannel, 0, len(channels))
	for _, c := range channels {
		ch := h.channelToAPI(db.GetChannelRow(c))
		models := byChannel[ch.Id]
		if models == nil {
			models = []api.OverviewModelScore{}
		}
		items = append(items, api.OverviewChannel{
			Id: ch.Id, Name: ch.Name, BaseUrl: ch.BaseUrl, KeyPrefix: ch.KeyPrefix,
			Protocols: ch.Protocols, Currency: ch.Currency, Note: ch.Note, Disabled: ch.Disabled,
			ModelCount: ch.ModelCount, LastTest: ch.LastTest,
			ProbeIntervalMinutes: ch.ProbeIntervalMinutes, ProbeModelId: ch.ProbeModelId,
			CreatedAt: ch.CreatedAt,
			Models:    models,
		})
	}
	writeJSON(w, http.StatusOK, struct {
		Items []api.OverviewChannel `json:"items"`
	}{items})
}
