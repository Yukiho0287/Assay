package tokenaccounting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Yukiho0287/assay/server/internal/probe"
)

// fakeVendor 可配置假上游。默认计量完全自洽：pt = 50 + chars/4（边际率 0.25、漂移 0）。
// 同时校验请求形状（max_tokens=4、无 temperature、流式必须带 include_usage），
// 形状不对返回 400 —— 负向测试一等公民：探针发错请求必须在测试里炸出来。
type fakeVendor struct {
	mu   sync.Mutex
	seen map[int]int // chars → 该长度已收到的请求数（用于模拟确定性破坏）

	ptFn               func(chars, nth int) int64
	identityDelta      int64 // total = pt + ct + delta
	omitUsageNonStream bool
	streamOmitUsage    bool
	streamPtDelta      int64
	status             int // >0 时所有请求返回该状态码
}

func (f *fakeVendor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.status > 0 {
		w.WriteHeader(f.status)
		io.WriteString(w, `{"error":{"message":"rate limited"}}`)
		return
	}
	data, _ := io.ReadAll(r.Body)
	var body struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
		Stream        bool `json:"stream"`
		StreamOptions *struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
		MaxTokens   int      `json:"max_tokens"`
		Temperature *float64 `json:"temperature"`
	}
	if err := json.Unmarshal(data, &body); err != nil || len(body.Messages) != 1 {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if body.MaxTokens != 4 || body.Temperature != nil {
		http.Error(w, "unexpected request shape", http.StatusBadRequest)
		return
	}
	if body.Stream && (body.StreamOptions == nil || !body.StreamOptions.IncludeUsage) {
		http.Error(w, "stream without include_usage", http.StatusBadRequest)
		return
	}

	chars := len(body.Messages[0].Content)
	f.mu.Lock()
	f.seen[chars]++
	nth := f.seen[chars]
	f.mu.Unlock()

	pt := int64(50 + chars/4)
	if f.ptFn != nil {
		pt = f.ptFn(chars, nth)
	}

	if body.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		if !f.streamOmitUsage {
			spt := pt + f.streamPtDelta
			fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":4,\"total_tokens\":%d}}\n\n",
				spt, spt+4+f.identityDelta)
		}
		io.WriteString(w, "data: [DONE]\n\n")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if f.omitUsageNonStream {
		io.WriteString(w, `{"id":"t","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
		return
	}
	fmt.Fprintf(w, `{"id":"t","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":%d,"completion_tokens":4,"total_tokens":%d,"prompt_tokens_details":{"cached_tokens":0}}}`,
		pt, pt+4+f.identityDelta)
}

func key(suite string, line int, mode string) string {
	return fmt.Sprintf("%s/%d/%s", suite, line, mode)
}

// runProbe 跑完整 Run，按 (suite,line,mode) 收集结果并校验行数与进度到达 9/9。
func runProbe(t *testing.T, f *fakeVendor, params probe.Params) map[string]probe.CaseResult {
	t.Helper()
	f.seen = map[int]int{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	var mu sync.Mutex
	results := map[string]probe.CaseResult{}
	maxDone, total := 0, 0
	in := probe.RunInput{
		Target: probe.Target{BaseURL: srv.URL, Model: "test-model", Protocols: []string{"openai_chat"}},
		APIKey: "test-key",
		Params: params,
		Client: srv.Client(),
		Report: func(_ context.Context, r probe.CaseResult) error {
			mu.Lock()
			defer mu.Unlock()
			results[key(r.Suite, r.Line, r.Mode)] = r
			return nil
		},
		Progress: func(_ context.Context, done, tot int) {
			mu.Lock()
			defer mu.Unlock()
			if done > maxDone {
				maxDone = done
			}
			total = tot
		},
	}
	if err := New().Run(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if maxDone != 9 || total != 9 {
		t.Fatalf("进度 %d/%d, want 9/9", maxDone, total)
	}
	if len(results) != 9 {
		t.Fatalf("结果行数 = %d, want 9", len(results))
	}
	return results
}

func wantStatus(t *testing.T, results map[string]probe.CaseResult, k, status string) probe.CaseResult {
	t.Helper()
	r, ok := results[k]
	if !ok {
		t.Fatalf("缺少结果行 %s", k)
	}
	if r.Status != status {
		t.Fatalf("%s status = %s (message: %s), want %s", k, r.Status, r.Message, status)
	}
	return r
}

// 全自洽 → 9 行全 passed，message 带实测数字，arguments 存 usage 原文。
func TestAllSelfConsistentPassed(t *testing.T) {
	results := runProbe(t, &fakeVendor{}, probe.Params{Concurrency: 4})
	for k := range results {
		wantStatus(t, results, k, probe.StatusPassed)
	}
	m4 := results[key(suiteMarginal, 4, probe.ModeNonStream)]
	for _, want := range []string{"边际率=", "漂移=", "cached=0", "pt="} {
		if !strings.Contains(m4.Message, want) {
			t.Fatalf("marginal/4 message 缺 %q: %s", want, m4.Message)
		}
	}
	if !strings.Contains(results[key(suiteDeterminism, 2, probe.ModeNonStream)].Message, "一致") {
		t.Fatal("determinism/2 message 应注明与基准一致")
	}
	st := results[key(suiteStream, 1, probe.ModeStream)]
	if !strings.Contains(st.Message, "与非流式 pt 一致") {
		t.Fatalf("stream 行 message: %s", st.Message)
	}
	if !strings.Contains(st.Arguments, "prompt_tokens") {
		t.Fatalf("arguments 应存 usage 原文: %s", st.Arguments)
	}
}

// 恒等式破坏 → 9 行全 violated。
func TestIdentityViolated(t *testing.T) {
	results := runProbe(t, &fakeVendor{identityDelta: 1}, probe.Params{Concurrency: 4})
	for k := range results {
		r := wantStatus(t, results, k, probe.StatusViolated)
		if !strings.Contains(r.Message, "恒等式破坏") {
			t.Fatalf("%s message: %s", k, r.Message)
		}
	}
}

// 纯 ASCII 上限破坏（pt = chars+200 > chars+100；边际率恰为 1.0 不违规）→ 9 行全 violated 且只因上限。
func TestAsciiCeilingViolated(t *testing.T) {
	f := &fakeVendor{ptFn: func(chars, nth int) int64 { return int64(chars) + 200 }}
	results := runProbe(t, f, probe.Params{Concurrency: 4})
	for k := range results {
		r := wantStatus(t, results, k, probe.StatusViolated)
		if !strings.Contains(r.Message, "纯 ASCII 上限破坏") {
			t.Fatalf("%s message: %s", k, r.Message)
		}
		if strings.Contains(r.Message, "边际率 ") {
			t.Fatalf("%s 边际率恰为 1.0 不应违规: %s", k, r.Message)
		}
	}
}

// 确定性破坏：第 3 个 8000 字符请求（= determinism 第 2 发，Concurrency=1 顺序确定）pt 抖动。
func TestDeterminismViolated(t *testing.T) {
	f := &fakeVendor{ptFn: func(chars, nth int) int64 {
		pt := int64(50 + chars/4)
		if chars == 2*step && nth == 3 {
			pt += 7
		}
		return pt
	}}
	results := runProbe(t, f, probe.Params{Concurrency: 1})
	r := wantStatus(t, results, key(suiteDeterminism, 2, probe.ModeNonStream), probe.StatusViolated)
	if !strings.Contains(r.Message, "确定性破坏") {
		t.Fatalf("message: %s", r.Message)
	}
	wantStatus(t, results, key(suiteDeterminism, 1, probe.ModeNonStream), probe.StatusPassed)
	wantStatus(t, results, key(suiteDeterminism, 3, probe.ModeNonStream), probe.StatusPassed)
	wantStatus(t, results, key(suiteMarginal, 2, probe.ModeNonStream), probe.StatusPassed)
}

// 边际率 > 1.0（斜率 1.05 但绝对值仍在上限内）→ 仅 marginal 2-4 违规。
func TestMarginalRateViolated(t *testing.T) {
	f := &fakeVendor{ptFn: func(chars, nth int) int64 { return int64(chars)*105/100 - 4100 }}
	results := runProbe(t, f, probe.Params{Concurrency: 4})
	for line := 2; line <= 4; line++ {
		r := wantStatus(t, results, key(suiteMarginal, line, probe.ModeNonStream), probe.StatusViolated)
		if !strings.Contains(r.Message, "边际率") {
			t.Fatalf("marginal/%d message: %s", line, r.Message)
		}
	}
	wantStatus(t, results, key(suiteMarginal, 1, probe.ModeNonStream), probe.StatusPassed)
	wantStatus(t, results, key(suiteDeterminism, 2, probe.ModeNonStream), probe.StatusPassed)
	wantStatus(t, results, key(suiteStream, 1, probe.ModeStream), probe.StatusPassed)
}

// 漂移 ≥3%（边际率 0.25/0.25/0.30，各自 ≤1.0）→ 仅 marginal/4 违规。
func TestDriftViolated(t *testing.T) {
	pts := map[int]int64{4000: 1000, 8000: 2000, 12000: 3000, 16000: 4200}
	f := &fakeVendor{ptFn: func(chars, nth int) int64 { return pts[chars] }}
	results := runProbe(t, f, probe.Params{Concurrency: 4})
	r := wantStatus(t, results, key(suiteMarginal, 4, probe.ModeNonStream), probe.StatusViolated)
	if !strings.Contains(r.Message, "漂移") {
		t.Fatalf("message: %s", r.Message)
	}
	for line := 1; line <= 3; line++ {
		wantStatus(t, results, key(suiteMarginal, line, probe.ModeNonStream), probe.StatusPassed)
	}
	wantStatus(t, results, key(suiteStream, 1, probe.ModeStream), probe.StatusPassed)
}

// 流式缺 usage → 仅 stream 行 violated。
func TestStreamMissingUsage(t *testing.T) {
	results := runProbe(t, &fakeVendor{streamOmitUsage: true}, probe.Params{Concurrency: 4})
	r := wantStatus(t, results, key(suiteStream, 1, probe.ModeStream), probe.StatusViolated)
	if !strings.Contains(r.Message, "未包含 usage") {
		t.Fatalf("message: %s", r.Message)
	}
	wantStatus(t, results, key(suiteStream, 1, probe.ModeNonStream), probe.StatusPassed)
	wantStatus(t, results, key(suiteMarginal, 1, probe.ModeNonStream), probe.StatusPassed)
}

// 流式与非流式 pt 不一致 → stream 行 violated。
func TestStreamPtMismatch(t *testing.T) {
	results := runProbe(t, &fakeVendor{streamPtDelta: 3}, probe.Params{Concurrency: 4})
	r := wantStatus(t, results, key(suiteStream, 1, probe.ModeStream), probe.StatusViolated)
	if !strings.Contains(r.Message, "流式 pt=") {
		t.Fatalf("message: %s", r.Message)
	}
	wantStatus(t, results, key(suiteStream, 1, probe.ModeNonStream), probe.StatusPassed)
}

// 非流式响应无 usage → 7 个非流式行 violated；stream 行自身有 usage，
// 但非流式对照缺失 → 一致性不判、行本身 passed（数据缺失不判违规路径）。
func TestNonStreamMissingUsage(t *testing.T) {
	results := runProbe(t, &fakeVendor{omitUsageNonStream: true}, probe.Params{Concurrency: 4})
	for _, d := range slotDefs() {
		k := key(d.suite, d.line, d.mode)
		if d.mode == probe.ModeNonStream {
			r := wantStatus(t, results, k, probe.StatusViolated)
			if !strings.Contains(r.Message, "usage 缺失") {
				t.Fatalf("%s message: %s", k, r.Message)
			}
		} else {
			r := wantStatus(t, results, k, probe.StatusPassed)
			if !strings.Contains(r.Message, "不判") {
				t.Fatalf("%s message 应注明对照缺失不判: %s", k, r.Message)
			}
		}
	}
}

// 429 → 全 rejected，且只重试 rejected：Reruns=1 → attempts=2。
func TestRejectedRetriesAndAttempts(t *testing.T) {
	results := runProbe(t, &fakeVendor{status: http.StatusTooManyRequests}, probe.Params{Concurrency: 2, Reruns: 1})
	for k, r := range results {
		if r.Status != probe.StatusRejected {
			t.Fatalf("%s status = %s, want rejected", k, r.Status)
		}
		if r.Attempts != 2 {
			t.Fatalf("%s attempts = %d, want 2", k, r.Attempts)
		}
		if r.HTTPStatus != http.StatusTooManyRequests {
			t.Fatalf("%s httpStatus = %d, want 429", k, r.HTTPStatus)
		}
	}
}

// 增量落库：请求层已定型的行（rejected）在采集阶段即上报，不等阶段二统一评估。
// marginal/4 阻塞、其余 8 个请求 429 → 放行前 Report 应已收到恰好 8 条 rejected。
func TestEarlyReportFinalizedRows(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(data, &body)
		if len(body.Messages) == 1 && len(body.Messages[0].Content) == 4*step {
			<-release // marginal/4 卡住，模拟慢渠道
		}
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	t.Cleanup(srv.Close)

	var mu sync.Mutex
	reported := map[string]probe.CaseResult{}
	in := probe.RunInput{
		Target: probe.Target{BaseURL: srv.URL, Model: "test-model", Protocols: []string{"openai_chat"}},
		APIKey: "test-key",
		Params: probe.Params{Concurrency: 4},
		Client: srv.Client(),
		Report: func(_ context.Context, r probe.CaseResult) error {
			mu.Lock()
			defer mu.Unlock()
			reported[key(r.Suite, r.Line, r.Mode)] = r
			return nil
		},
	}
	done := make(chan error, 1)
	go func() { done <- New().Run(context.Background(), in) }()

	// 轮询等 8 条早报到位（阻塞行未放行，评估阶段必然未开始）
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		n := len(reported)
		mu.Unlock()
		if n == 8 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("放行前早报行数 = %d, want 8", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	if _, ok := reported[key(suiteMarginal, 4, probe.ModeNonStream)]; ok {
		t.Fatal("阻塞中的 marginal/4 不应已上报")
	}
	for k, r := range reported {
		if r.Status != probe.StatusRejected {
			t.Fatalf("早报行 %s status = %s, want rejected", k, r.Status)
		}
	}
	mu.Unlock()

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 9 {
		t.Fatalf("终态行数 = %d, want 9", len(reported))
	}
	wantStatus(t, reported, key(suiteMarginal, 4, probe.ModeNonStream), probe.StatusRejected)
}

// violated 不重试：请求层即定型的 violated（流式缺 usage）在 Reruns=2 下 attempts 仍为 1。
func TestViolatedNotRetried(t *testing.T) {
	results := runProbe(t, &fakeVendor{streamOmitUsage: true}, probe.Params{Concurrency: 4, Reruns: 2})
	r := wantStatus(t, results, key(suiteStream, 1, probe.ModeStream), probe.StatusViolated)
	if r.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1（violated 不应重试）", r.Attempts)
	}
}
