package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/db"
)

const (
	probeTimeout     = 15 * time.Second
	anthropicVersion = "2023-06-01"
)

// probeClient 连通测试专用客户端；总超时由每次探测的 context 控制
var probeClient = &http.Client{}

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
		Results:  runProbes(r.Context(), c.BaseUrl, c.ApiKey, c.Protocols, m.Name),
	}

	// 落库为该渠道最近一次测试；落库失败即报错（结果没被记住就不能装作成功）
	raw, err := json.Marshal(test)
	if err != nil {
		h.log.Error("序列化测试结果失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "服务内部错误"})
		return
	}
	if _, err := h.q.UpdateChannelLastTest(r.Context(), db.UpdateChannelLastTestParams{ID: id, LastTest: raw}); err != nil {
		h.log.Error("保存测试结果失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, api.Error{Error: "测试已执行但结果保存失败"})
		return
	}
	h.log.Info("连通测试", "channel", id.String(), "model", m.Name, "operator", s.Username)
	writeJSON(w, http.StatusOK, test)
}

// runProbes 对渠道声明的每个协议并发发送最小真实请求，结果按协议声明顺序返回
func runProbes(ctx context.Context, baseURL, apiKey string, protocols []string, model string) []api.ConnectivityResult {
	results := make([]api.ConnectivityResult, len(protocols))
	var wg sync.WaitGroup
	for i, p := range protocols {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = probeOne(ctx, api.Protocol(p), baseURL, apiKey, model)
		}()
	}
	wg.Wait()
	return results
}

func probeOne(ctx context.Context, proto api.Protocol, baseURL, apiKey, model string) api.ConnectivityResult {
	res := api.ConnectivityResult{Protocol: proto}
	fail := func(msg string) api.ConnectivityResult {
		msg = truncateOneLine(msg)
		res.Error = &msg
		return res
	}

	var path string
	var body map[string]any
	switch proto {
	case api.OpenaiChat:
		path = "/chat/completions"
		body = map[string]any{
			"model":      model,
			"messages":   []map[string]string{{"role": "user", "content": "ping"}},
			"max_tokens": 1,
			"stream":     true,
		}
	case api.OpenaiResponses:
		path = "/responses"
		// Responses API 的 max_output_tokens 下限是 16，取下限即最小消耗
		body = map[string]any{
			"model":             model,
			"input":             "ping",
			"max_output_tokens": 16,
			"stream":            true,
		}
	case api.AnthropicMessages:
		path = "/messages"
		body = map[string]any{
			"model":      model,
			"max_tokens": 1,
			"messages":   []map[string]string{{"role": "user", "content": "ping"}},
			"stream":     true,
		}
	default:
		return fail("未知协议")
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	payload, err := json.Marshal(body)
	if err != nil {
		return fail("构造请求失败: " + err.Error())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fail("构造请求失败: " + err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	if proto == api.AnthropicMessages {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", anthropicVersion)
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	start := time.Now()
	resp, err := probeClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fail("超时（15 秒无响应）")
		}
		return fail("连接失败: " + err.Error())
	}
	defer resp.Body.Close()

	res.Status = &resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(snippet))
		if msg == "" {
			msg = resp.Status
		}
		return fail(msg)
	}

	// 读到首个响应字节即视为流已开启，此刻耗时即 TTFT
	buf := make([]byte, 1)
	if _, err := resp.Body.Read(buf); err != nil && !errors.Is(err, io.EOF) {
		return fail("读取响应流失败: " + err.Error())
	}
	ttft := int(time.Since(start).Milliseconds())
	res.Ok = true
	res.TtftMs = &ttft
	return res
}

// truncateOneLine 错误摘要压成单行并截断，避免超长上游错误体撑爆 last_test
func truncateOneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}
