package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/db"
	"github.com/Yukiho0287/assay/server/internal/probe"
	"github.com/Yukiho0287/assay/server/internal/probe/registry"
)

// taskKindQuality tasks.kind 取值：质量检测大类
const taskKindQuality = "quality"

func (h *handlers) ListProbes(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requirePerm(w, r, "quality"); !ok {
		return
	}
	items := make([]api.ProbeInfo, 0, len(registry.All()))
	for _, p := range registry.All() {
		items = append(items, probeInfoToAPI(p.Info))
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *handlers) CreateQualityTask(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requirePerm(w, r, "quality")
	if !ok {
		return
	}
	var req api.QualityTaskCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "请求格式错误"})
		return
	}
	if len(req.Probes) == 0 {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "至少勾选一个检测项"})
		return
	}
	probes := make([]probe.Probe, 0, len(req.Probes))
	for _, id := range req.Probes {
		p, ok := registry.Get(id)
		if !ok {
			writeJSON(w, http.StatusBadRequest, api.Error{Error: fmt.Sprintf("未知检测项 %q", id)})
			return
		}
		probes = append(probes, p)
	}

	ch, err := h.q.GetChannel(r.Context(), req.ChannelId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, api.Error{Error: "渠道不存在"})
			return
		}
		h.internalError(w, "读取渠道失败", err)
		return
	}
	if ch.Disabled {
		writeJSON(w, http.StatusConflict, api.Error{Error: "渠道已停用，不能发起新任务"})
		return
	}
	m, err := h.q.GetChannelModel(r.Context(), db.GetChannelModelParams{ID: req.ModelEntryId, ChannelID: req.ChannelId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, api.Error{Error: "模型条目不存在"})
			return
		}
		h.internalError(w, "读取模型条目失败", err)
		return
	}

	// fail-fast：协议与定价前置校验，问题在创建时暴露而不是跑到一半才失败
	for _, p := range probes {
		if !slices.ContainsFunc(p.Info.Protocols, func(proto string) bool {
			return slices.Contains(ch.Protocols, proto)
		}) {
			writeJSON(w, http.StatusConflict, api.Error{
				Error: fmt.Sprintf("渠道未声明检测项「%s」所需协议（需 %v）", p.Info.Name, p.Info.Protocols),
			})
			return
		}
		if p.Info.NeedsPricing && m.InputPrice == nil {
			writeJSON(w, http.StatusBadRequest, api.Error{
				Error: fmt.Sprintf("检测项「%s」需要模型定价，请先在渠道详情补全该模型单价", p.Info.Name),
			})
			return
		}
	}

	params, errMsg := resolveParams(req.Params)
	if errMsg != "" {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: errMsg})
		return
	}

	total := 0
	for _, p := range probes {
		total += p.SlotCount(params)
	}

	// 参数快照：证据链要求，历史报告不受渠道后续编辑/删除影响
	target := probe.Target{
		ChannelID:        ch.ID.String(),
		ChannelName:      ch.Name,
		BaseURL:          ch.BaseUrl,
		ModelEntryID:     m.ID.String(),
		Model:            m.Name,
		Protocols:        ch.Protocols,
		Currency:         ch.Currency,
		InputPrice:       m.InputPrice,
		OutputPrice:      m.OutputPrice,
		CachedInputPrice: m.CachedInputPrice,
	}
	targetJSON, err := json.Marshal(target)
	if err != nil {
		h.internalError(w, "序列化任务快照失败", err)
		return
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		h.internalError(w, "序列化任务参数失败", err)
		return
	}

	// 任务行与队列条目同一事务：要么都在要么都不在
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		h.internalError(w, "开启事务失败", err)
		return
	}
	defer tx.Rollback(context.WithoutCancel(r.Context()))
	qtx := h.q.WithTx(tx)
	row, err := qtx.CreateTask(r.Context(), db.CreateTaskParams{
		Kind:          taskKindQuality,
		ChannelID:     pgtype.UUID{Bytes: req.ChannelId, Valid: true},
		Target:        targetJSON,
		Probes:        req.Probes,
		Params:        paramsJSON,
		ProgressTotal: int32(total),
		CreatedBy:     pgtype.UUID{Bytes: s.ID, Valid: true},
	})
	if err != nil {
		h.internalError(w, "创建任务失败", err)
		return
	}
	jobID, err := h.tq.EnqueueQualityTaskTx(r.Context(), tx, row.ID)
	if err != nil {
		h.internalError(w, "任务入队失败", err)
		return
	}
	if err := qtx.SetTaskRiverJobID(r.Context(), db.SetTaskRiverJobIDParams{
		ID: row.ID, RiverJobID: pgtype.Int8{Int64: jobID, Valid: true},
	}); err != nil {
		h.internalError(w, "回写队列 ID 失败", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.internalError(w, "提交事务失败", err)
		return
	}

	h.log.Info("质量检测任务已创建", "task", row.ID, "channel", ch.Name, "model", m.Name, "probes", req.Probes, "slots", total)
	task, err := h.q.GetTask(r.Context(), row.ID)
	if err != nil {
		h.internalError(w, "读取新建任务失败", err)
		return
	}
	out, err := taskToAPI(task)
	if err != nil {
		h.internalError(w, "任务数据损坏", err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *handlers) ListQualityTasks(w http.ResponseWriter, r *http.Request, params api.ListQualityTasksParams) {
	if _, ok := h.requirePerm(w, r, "quality"); !ok {
		return
	}
	limit, offset := 50, 0
	if params.Limit != nil && *params.Limit >= 1 && *params.Limit <= 200 {
		limit = *params.Limit
	}
	if params.Offset != nil && *params.Offset > 0 {
		offset = *params.Offset
	}
	// 筛选条件：nil = 不过滤；列表与总数必须用同一组条件，否则页码算错
	var status *string
	if params.Status != nil {
		status = (*string)(params.Status)
	}
	var channelID pgtype.UUID
	if params.ChannelId != nil {
		channelID = pgtype.UUID{Bytes: *params.ChannelId, Valid: true}
	}
	rows, err := h.q.ListTasks(r.Context(), db.ListTasksParams{
		Kind: taskKindQuality, Limit: int32(limit), Offset: int32(offset),
		Status: status, ChannelID: channelID,
	})
	if err != nil {
		h.internalError(w, "读取任务列表失败", err)
		return
	}
	total, err := h.q.CountTasks(r.Context(), db.CountTasksParams{
		Kind: taskKindQuality, Status: status, ChannelID: channelID,
	})
	if err != nil {
		h.internalError(w, "统计任务数失败", err)
		return
	}
	items := make([]api.QualityTask, 0, len(rows))
	for _, row := range rows {
		// ListTasksRow 与 GetTaskRow 字段完全一致（同一 select），直接转换复用映射
		t, err := taskToAPI(db.GetTaskRow(row))
		if err != nil {
			h.internalError(w, "任务数据损坏", err)
			return
		}
		items = append(items, t)
	}
	writeJSON(w, http.StatusOK, api.QualityTaskList{Items: items, Total: int(total)})
}

func (h *handlers) GetQualityTask(w http.ResponseWriter, r *http.Request, id api.IdPath) {
	if _, ok := h.requirePerm(w, r, "quality"); !ok {
		return
	}
	task, ok := h.loadQualityTask(w, r, id)
	if !ok {
		return
	}
	out, err := taskToAPI(task)
	if err != nil {
		h.internalError(w, "任务数据损坏", err)
		return
	}
	agg, err := h.q.AggregateTaskCaseResults(r.Context(), id)
	if err != nil {
		h.internalError(w, "聚合统计失败", err)
		return
	}
	if stats := buildStats(agg); stats != nil {
		out.Stats = stats
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) CancelQualityTask(w http.ResponseWriter, r *http.Request, id api.IdPath) {
	if _, ok := h.requirePerm(w, r, "quality"); !ok {
		return
	}
	task, ok := h.loadQualityTask(w, r, id)
	if !ok {
		return
	}
	rows, err := h.q.CancelTask(r.Context(), id)
	if err != nil {
		h.internalError(w, "取消任务失败", err)
		return
	}
	if rows == 0 {
		// 状态守卫兜底并发：读到 queued/running 之后可能已到终态
		writeJSON(w, http.StatusConflict, api.Error{Error: "任务已结束，不可取消"})
		return
	}
	// 任务行已翻 canceled（权威状态），再取消 river job：排队的原子取消；
	// 运行中的通知 worker 取消 ctx 中止在途请求。失败只告警不回滚——
	// MarkTaskRunning/FinishTask 均守卫状态，残余 worker 写不脏已取消的任务
	if task.RiverJobID.Valid {
		if err := h.tq.CancelJob(r.Context(), task.RiverJobID.Int64); err != nil {
			h.log.Warn("取消 river job 失败（任务行已取消，靠状态守卫兜底）", "task", id, "job", task.RiverJobID.Int64, "err", err)
		}
	}
	h.broker.notify(r.Context(), id, "canceled", int(task.ProgressDone), int(task.ProgressTotal))
	h.log.Info("任务已取消", "task", id)

	task, err = h.q.GetTask(r.Context(), id)
	if err != nil {
		h.internalError(w, "读取任务失败", err)
		return
	}
	out, err := taskToAPI(task)
	if err != nil {
		h.internalError(w, "任务数据损坏", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) ListQualityTaskResults(w http.ResponseWriter, r *http.Request, id api.IdPath, params api.ListQualityTaskResultsParams) {
	if _, ok := h.requirePerm(w, r, "quality"); !ok {
		return
	}
	if _, ok := h.loadQualityTask(w, r, id); !ok {
		return
	}
	var status *string
	if params.Status != nil {
		s := string(*params.Status)
		status = &s
	}
	rows, err := h.q.ListTaskCaseResults(r.Context(), db.ListTaskCaseResultsParams{TaskID: id, Status: status})
	if err != nil {
		h.internalError(w, "读取用例结果失败", err)
		return
	}
	items := make([]api.QualityCaseResult, 0, len(rows))
	for _, row := range rows {
		items = append(items, caseResultToAPI(row))
	}
	writeJSON(w, http.StatusOK, items)
}

// loadQualityTask 读任务并校验 kind，查无此任务（或不是质量任务）统一 404。
func (h *handlers) loadQualityTask(w http.ResponseWriter, r *http.Request, id api.IdPath) (db.GetTaskRow, bool) {
	task, err := h.q.GetTask(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, api.Error{Error: "任务不存在"})
			return db.GetTaskRow{}, false
		}
		h.internalError(w, "读取任务失败", err)
		return db.GetTaskRow{}, false
	}
	if task.Kind != taskKindQuality {
		writeJSON(w, http.StatusNotFound, api.Error{Error: "任务不存在"})
		return db.GetTaskRow{}, false
	}
	return task, true
}

func (h *handlers) internalError(w http.ResponseWriter, msg string, err error) {
	h.log.Error(msg, "err", err)
	writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
}

// resolveParams 落默认值 + 范围校验；返回非空 errMsg 表示 400。
func resolveParams(in *api.QualityTaskParams) (probe.Params, string) {
	p := probe.Params{Concurrency: 4, Reruns: 2}
	if in == nil {
		return p, ""
	}
	if in.Concurrency != nil {
		if *in.Concurrency < 1 || *in.Concurrency > 16 {
			return p, "并发数须在 1-16 之间"
		}
		p.Concurrency = *in.Concurrency
	}
	if in.Reruns != nil {
		if *in.Reruns < 0 || *in.Reruns > 5 {
			return p, "重跑轮数须在 0-5 之间"
		}
		p.Reruns = *in.Reruns
	}
	if in.MaxCases != nil {
		if *in.MaxCases < 1 {
			return p, "用例数上限须不小于 1"
		}
		p.MaxCases = *in.MaxCases
	}
	return p, ""
}

func probeInfoToAPI(info probe.Info) api.ProbeInfo {
	protocols := make([]api.Protocol, 0, len(info.Protocols))
	for _, p := range info.Protocols {
		protocols = append(protocols, api.Protocol(p))
	}
	return api.ProbeInfo{
		Id:               info.ID,
		Name:             info.Name,
		Description:      info.Description,
		Protocols:        protocols,
		NeedsControl:     info.NeedsControl,
		NeedsPricing:     info.NeedsPricing,
		CaseCount:        info.CaseCount,
		RequestsPerCase:  info.RequestsPerCase,
		SupportsMaxCases: info.SupportsMaxCases,
	}
}

func taskToAPI(t db.GetTaskRow) (api.QualityTask, error) {
	var target api.TaskTarget
	if err := json.Unmarshal(t.Target, &target); err != nil {
		return api.QualityTask{}, fmt.Errorf("解析任务快照: %w", err)
	}
	var params api.QualityTaskParams
	if err := json.Unmarshal(t.Params, &params); err != nil {
		return api.QualityTask{}, fmt.Errorf("解析任务参数: %w", err)
	}
	out := api.QualityTask{
		Id:            t.ID,
		Status:        api.TaskStatus(t.Status),
		Target:        target,
		Params:        params,
		Probes:        t.Probes,
		ProgressTotal: int(t.ProgressTotal),
		ProgressDone:  int(t.ProgressDone),
		Error:         t.Error,
		CreatedAt:     t.CreatedAt,
		CreatedBy:     t.CreatedByName,
	}
	if t.StartedAt.Valid {
		v := t.StartedAt.Time
		out.StartedAt = &v
	}
	if t.FinishedAt.Valid {
		v := t.FinishedAt.Time
		out.FinishedAt = &v
	}
	return out, nil
}

func caseResultToAPI(r db.ListTaskCaseResultsRow) api.QualityCaseResult {
	out := api.QualityCaseResult{
		Probe:           r.Probe,
		Suite:           r.Suite,
		Line:            int(r.Line),
		Mode:            api.CaseMode(r.Mode),
		SelectionReason: r.SelectionReason,
		Status:          api.CaseStatus(r.Status),
		Message:         r.Message,
		Arguments:       r.Arguments,
		Attempts:        int(r.Attempts),
	}
	if r.HttpStatus.Valid {
		v := int(r.HttpStatus.Int32)
		out.HttpStatus = &v
	}
	if r.LatencyMs.Valid {
		v := int(r.LatencyMs.Int32)
		out.LatencyMs = &v
	}
	return out
}

// buildStats 聚合行 →（总计 + 按模式 + 按选例理由）三视图；无结果返回 nil（如 queued 任务）。
func buildStats(rows []db.AggregateTaskCaseResultsRow) *api.TaskStats {
	if len(rows) == 0 {
		return nil
	}
	stats := &api.TaskStats{}
	modes := map[string]*api.TaskStatBucket{}
	reasons := map[string]*api.TaskStatBucket{}
	bucket := func(m map[string]*api.TaskStatBucket, name string) *api.TaskStatBucket {
		if b, ok := m[name]; ok {
			return b
		}
		b := &api.TaskStatBucket{Name: name}
		m[name] = b
		return b
	}
	add := func(b *api.TaskStatBucket, status string, n int) {
		b.Total += n
		switch status {
		case probe.StatusPassed:
			b.Passed += n
		case probe.StatusRejected:
			b.Rejected += n
		case probe.StatusViolated:
			b.Violated += n
		case probe.StatusCollected:
			b.Collected += n
		}
	}
	for _, row := range rows {
		n := int(row.N)
		stats.Total += n
		switch row.Status {
		case probe.StatusPassed:
			stats.Passed += n
		case probe.StatusRejected:
			stats.Rejected += n
		case probe.StatusViolated:
			stats.Violated += n
		case probe.StatusCollected:
			stats.Collected += n
		}
		add(bucket(modes, row.BucketMode), row.Status, n)
		add(bucket(reasons, row.BucketReason), row.Status, n)
	}
	stats.ByMode = sortedBuckets(modes)
	stats.ByReason = sortedBuckets(reasons)
	return stats
}

func sortedBuckets(m map[string]*api.TaskStatBucket) []api.TaskStatBucket {
	out := make([]api.TaskStatBucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
