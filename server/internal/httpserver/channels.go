package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/db"
)

// maskKey 生成 API key 脱敏前缀：仅保留前 7 个字符（key 写后不可回读）
func maskKey(k string) string {
	r := []rune(k)
	if len(r) > 7 {
		r = r[:7]
	}
	return string(r) + "***"
}

// normalizeBaseURL 校验并规范化 base_url：必须是 http(s) 绝对地址，末尾斜杠自动去除
func normalizeBaseURL(raw string) (string, bool) {
	s := strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(s) > 512 {
		return "", false
	}
	return s, true
}

// validProtocols 协议集合非空且每项都是已知枚举
func validProtocols(ps []api.Protocol) bool {
	if len(ps) == 0 {
		return false
	}
	for _, p := range ps {
		if !p.Valid() {
			return false
		}
	}
	return true
}

func protocolsToDB(ps []api.Protocol) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = string(p)
	}
	return out
}

func protocolsFromDB(ss []string) []api.Protocol {
	out := make([]api.Protocol, len(ss))
	for i, s := range ss {
		out[i] = api.Protocol(s)
	}
	return out
}

// validatePrices 定价跨字段规则（契约 ModelEntryUpsert）：输入/输出价成对，缓存读价依赖前两者
func validatePrices(m api.ModelEntryUpsert) string {
	if (m.InputPrice == nil) != (m.OutputPrice == nil) {
		return "输入价与输出价须成对填写"
	}
	if m.CachedInputPrice != nil && m.InputPrice == nil {
		return "缓存读价需先填写输入价与输出价"
	}
	for _, p := range []*float32{m.InputPrice, m.OutputPrice, m.CachedInputPrice} {
		if p != nil && *p < 0 {
			return "价格不能为负数"
		}
	}
	return ""
}

func f32(p *float64) *float32 {
	if p == nil {
		return nil
	}
	v := float32(*p)
	return &v
}

func f64(p *float32) *float64 {
	if p == nil {
		return nil
	}
	v := float64(*p)
	return &v
}

func (h *handlers) channelToAPI(c db.GetChannelRow) api.Channel {
	out := api.Channel{
		Id:         c.ID,
		Name:       c.Name,
		BaseUrl:    c.BaseUrl,
		KeyPrefix:  c.KeyPrefix,
		Protocols:  protocolsFromDB(c.Protocols),
		Currency:   api.Currency(c.Currency),
		Note:       c.Note,
		Disabled:   c.Disabled,
		ModelCount: int(c.ModelCount),
		CreatedAt:  c.CreatedAt,
	}
	if len(c.LastTest) > 0 {
		var t api.ConnectivityTest
		if err := json.Unmarshal(c.LastTest, &t); err == nil {
			out.LastTest = &t
		} else {
			h.log.Error("last_test 数据损坏", "channel", c.Name, "err", err)
		}
	}
	return out
}

func modelToAPI(m db.GetChannelModelRow) api.ModelEntry {
	return api.ModelEntry{
		Id:               m.ID,
		Name:             m.Name,
		InputPrice:       f32(m.InputPrice),
		OutputPrice:      f32(m.OutputPrice),
		CachedInputPrice: f32(m.CachedInputPrice),
		CreatedAt:        m.CreatedAt,
	}
}

func (h *handlers) ListChannels(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requirePerm(w, r, "channels"); !ok {
		return
	}
	rows, err := h.q.ListChannels(r.Context())
	if err != nil {
		h.log.Error("查询渠道列表失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	out := make([]api.Channel, 0, len(rows))
	for _, c := range rows {
		out = append(out, h.channelToAPI(db.GetChannelRow(c)))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) CreateChannel(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requirePerm(w, r, "channels")
	if !ok {
		return
	}
	var req api.ChannelCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "请求格式错误"})
		return
	}
	if n := utf8.RuneCountInString(req.Name); n == 0 || n > 64 {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "渠道名不能为空且最多 64 字符"})
		return
	}
	baseURL, ok := normalizeBaseURL(req.BaseUrl)
	if !ok {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "base_url 必须是 http(s) 绝对地址"})
		return
	}
	if req.ApiKey == "" {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "API key 不能为空"})
		return
	}
	if !validProtocols(req.Protocols) {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "至少选择一个有效协议"})
		return
	}
	currency := api.USD
	if req.Currency != nil {
		if !req.Currency.Valid() {
			writeJSON(w, http.StatusBadRequest, api.Error{Error: "币种无效"})
			return
		}
		currency = *req.Currency
	}
	note := ""
	if req.Note != nil {
		if utf8.RuneCountInString(*req.Note) > 500 {
			writeJSON(w, http.StatusBadRequest, api.Error{Error: "备注最多 500 字符"})
			return
		}
		note = *req.Note
	}

	id, err := h.q.CreateChannel(r.Context(), db.CreateChannelParams{
		Name:      req.Name,
		BaseUrl:   baseURL,
		ApiKey:    req.ApiKey,
		KeyPrefix: maskKey(req.ApiKey),
		Protocols: protocolsToDB(req.Protocols),
		Currency:  string(currency),
		Note:      note,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, api.Error{Error: "渠道名已存在"})
			return
		}
		h.log.Error("创建渠道失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	c, err := h.q.GetChannel(r.Context(), id)
	if err != nil {
		h.log.Error("回读新建渠道失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	h.log.Info("创建渠道", "channel", req.Name, "operator", s.Username)
	writeJSON(w, http.StatusCreated, h.channelToAPI(c))
}

func (h *handlers) GetChannel(w http.ResponseWriter, r *http.Request, id api.IdPath) {
	if _, ok := h.requirePerm(w, r, "channels"); !ok {
		return
	}
	c, err := h.q.GetChannel(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, api.Error{Error: "渠道不存在"})
			return
		}
		h.log.Error("查询渠道失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	rows, err := h.q.ListChannelModels(r.Context(), id)
	if err != nil {
		h.log.Error("查询模型条目失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	models := make([]api.ModelEntry, 0, len(rows))
	for _, m := range rows {
		models = append(models, modelToAPI(db.GetChannelModelRow(m)))
	}
	ch := h.channelToAPI(c)
	writeJSON(w, http.StatusOK, api.ChannelDetail{
		Id: ch.Id, Name: ch.Name, BaseUrl: ch.BaseUrl, KeyPrefix: ch.KeyPrefix,
		Protocols: ch.Protocols, Currency: ch.Currency, Note: ch.Note, Disabled: ch.Disabled,
		ModelCount: ch.ModelCount, LastTest: ch.LastTest, CreatedAt: ch.CreatedAt,
		Models: models,
	})
}

func (h *handlers) UpdateChannel(w http.ResponseWriter, r *http.Request, id api.IdPath) {
	s, ok := h.requirePerm(w, r, "channels")
	if !ok {
		return
	}
	var req api.ChannelUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "请求格式错误"})
		return
	}

	p := db.UpdateChannelParams{ID: id, Disabled: req.Disabled}
	if req.Name != nil {
		if n := utf8.RuneCountInString(*req.Name); n == 0 || n > 64 {
			writeJSON(w, http.StatusBadRequest, api.Error{Error: "渠道名不能为空且最多 64 字符"})
			return
		}
		p.Name = req.Name
	}
	if req.BaseUrl != nil {
		u, ok := normalizeBaseURL(*req.BaseUrl)
		if !ok {
			writeJSON(w, http.StatusBadRequest, api.Error{Error: "base_url 必须是 http(s) 绝对地址"})
			return
		}
		p.BaseUrl = &u
	}
	if req.ApiKey != nil {
		if *req.ApiKey == "" {
			writeJSON(w, http.StatusBadRequest, api.Error{Error: "API key 不能为空"})
			return
		}
		p.ApiKey = req.ApiKey
		prefix := maskKey(*req.ApiKey)
		p.KeyPrefix = &prefix
	}
	if req.Protocols != nil {
		if !validProtocols(*req.Protocols) {
			writeJSON(w, http.StatusBadRequest, api.Error{Error: "至少选择一个有效协议"})
			return
		}
		p.Protocols = protocolsToDB(*req.Protocols)
	}
	if req.Currency != nil {
		if !req.Currency.Valid() {
			writeJSON(w, http.StatusBadRequest, api.Error{Error: "币种无效"})
			return
		}
		c := string(*req.Currency)
		p.Currency = &c
	}
	if req.Note != nil {
		if utf8.RuneCountInString(*req.Note) > 500 {
			writeJSON(w, http.StatusBadRequest, api.Error{Error: "备注最多 500 字符"})
			return
		}
		p.Note = req.Note
	}

	n, err := h.q.UpdateChannel(r.Context(), p)
	if err != nil {
		if isUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, api.Error{Error: "渠道名已存在"})
			return
		}
		h.log.Error("更新渠道失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	if n == 0 {
		writeJSON(w, http.StatusNotFound, api.Error{Error: "渠道不存在"})
		return
	}
	c, err := h.q.GetChannel(r.Context(), id)
	if err != nil {
		h.log.Error("回读渠道失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	h.log.Info("更新渠道", "channel", c.Name, "operator", s.Username)
	writeJSON(w, http.StatusOK, h.channelToAPI(c))
}

func (h *handlers) DeleteChannel(w http.ResponseWriter, r *http.Request, id api.IdPath) {
	s, ok := h.requirePerm(w, r, "channels")
	if !ok {
		return
	}
	n, err := h.q.DeleteChannel(r.Context(), id)
	if err != nil {
		h.log.Error("删除渠道失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	if n == 0 {
		writeJSON(w, http.StatusNotFound, api.Error{Error: "渠道不存在"})
		return
	}
	h.log.Info("删除渠道", "id", id.String(), "operator", s.Username)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) AddChannelModel(w http.ResponseWriter, r *http.Request, id api.IdPath) {
	s, ok := h.requirePerm(w, r, "channels")
	if !ok {
		return
	}
	var req api.ModelEntryUpsert
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "请求格式错误"})
		return
	}
	if n := utf8.RuneCountInString(req.Name); n == 0 || n > 128 {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "模型名不能为空且最多 128 字符"})
		return
	}
	if msg := validatePrices(req); msg != "" {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: msg})
		return
	}
	if _, err := h.q.GetChannel(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, api.Error{Error: "渠道不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}

	m, err := h.q.CreateChannelModel(r.Context(), db.CreateChannelModelParams{
		ChannelID:        id,
		Name:             req.Name,
		InputPrice:       f64(req.InputPrice),
		OutputPrice:      f64(req.OutputPrice),
		CachedInputPrice: f64(req.CachedInputPrice),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, api.Error{Error: "该渠道下模型名已存在"})
			return
		}
		h.log.Error("添加模型条目失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	h.log.Info("添加模型条目", "model", req.Name, "channel", id.String(), "operator", s.Username)
	writeJSON(w, http.StatusCreated, modelToAPI(db.GetChannelModelRow(m)))
}

func (h *handlers) UpdateChannelModel(w http.ResponseWriter, r *http.Request, id api.IdPath, modelID api.ModelIdPath) {
	s, ok := h.requirePerm(w, r, "channels")
	if !ok {
		return
	}
	var req api.ModelEntryUpsert
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "请求格式错误"})
		return
	}
	if n := utf8.RuneCountInString(req.Name); n == 0 || n > 128 {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "模型名不能为空且最多 128 字符"})
		return
	}
	if msg := validatePrices(req); msg != "" {
		writeJSON(w, http.StatusBadRequest, api.Error{Error: msg})
		return
	}

	m, err := h.q.UpdateChannelModel(r.Context(), db.UpdateChannelModelParams{
		ID:               modelID,
		ChannelID:        id,
		Name:             req.Name,
		InputPrice:       f64(req.InputPrice),
		OutputPrice:      f64(req.OutputPrice),
		CachedInputPrice: f64(req.CachedInputPrice),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, api.Error{Error: "渠道或模型条目不存在"})
			return
		}
		if isUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, api.Error{Error: "该渠道下模型名已存在"})
			return
		}
		h.log.Error("更新模型条目失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	h.log.Info("更新模型条目", "model", req.Name, "operator", s.Username)
	writeJSON(w, http.StatusOK, modelToAPI(db.GetChannelModelRow(m)))
}

func (h *handlers) DeleteChannelModel(w http.ResponseWriter, r *http.Request, id api.IdPath, modelID api.ModelIdPath) {
	s, ok := h.requirePerm(w, r, "channels")
	if !ok {
		return
	}
	n, err := h.q.DeleteChannelModel(r.Context(), db.DeleteChannelModelParams{ID: modelID, ChannelID: id})
	if err != nil {
		h.log.Error("删除模型条目失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	if n == 0 {
		writeJSON(w, http.StatusNotFound, api.Error{Error: "渠道或模型条目不存在"})
		return
	}
	h.log.Info("删除模型条目", "id", modelID.String(), "operator", s.Username)
	w.WriteHeader(http.StatusNoContent)
}
