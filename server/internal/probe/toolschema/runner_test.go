package toolschema

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

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Yukiho0287/assay/server/internal/probe"
)

// 测试用最小包装 schema：value 必须是整数
const testSchemaJSON = `{"type":"object","required":["value"],"additionalProperties":false,"properties":{"value":{"type":"integer"}}}`

// runFake 用一个合成用例（非流式 + 流式两个槽位）跑 runSlots，按 mode 收集末次结果。
func runFake(t *testing.T, handler http.Handler, params probe.Params) map[string]probe.CaseResult {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	schema, err := probe.DecodeUseNumber([]byte(testSchemaJSON))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	cases := []Case{{Suite: "TestFake", Line: 1, Reason: "object_parameter_schema", Schema: schema}}

	var mu sync.Mutex
	results := map[string]probe.CaseResult{}
	in := probe.RunInput{
		Target: probe.Target{BaseURL: srv.URL, Model: "test-model"},
		APIKey: "sk-test",
		Params: params,
		Client: srv.Client(),
		Report: func(_ context.Context, r probe.CaseResult) error {
			mu.Lock()
			defer mu.Unlock()
			results[r.Mode] = r
			return nil
		},
	}
	if err := runSlots(context.Background(), in, cases, []*jsonschema.Schema{compiled}); err != nil {
		t.Fatalf("runSlots: %v", err)
	}
	return results
}

// toolCallHandler 假上游：非流式直接返回 arguments；流式把 arguments 拆两个分片发送。
func toolCallHandler(argsJSON string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			half := len(argsJSON) / 2
			writeChunk(w, map[string]any{"index": 0, "function": map[string]any{"name": toolName, "arguments": argsJSON[:half]}})
			writeChunk(w, map[string]any{"index": 0, "function": map[string]any{"arguments": argsJSON[half:]}})
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		writeJSONResp(w, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{
				"tool_calls": []any{map[string]any{"function": map[string]any{"name": toolName, "arguments": argsJSON}}},
			}}},
		})
	}
}

func writeChunk(w http.ResponseWriter, toolCall map[string]any) {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{toolCall}}}}})
	fmt.Fprintf(w, "data: %s\n\n", b)
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestRunPassed(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	inner := toolCallHandler(`{"value":42}`)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		r.Body = io.NopCloser(strings.NewReader(string(raw)))
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("认证头 = %q", r.Header.Get("Authorization"))
		}
		inner(w, r)
	})

	results := runFake(t, handler, probe.Params{Concurrency: 2})
	for _, mode := range []string{probe.ModeNonStream, probe.ModeStream} {
		r, ok := results[mode]
		if !ok {
			t.Fatalf("%s 无结果", mode)
		}
		if r.Status != probe.StatusPassed {
			t.Errorf("%s status = %s (%s), 期望 passed", mode, r.Status, r.Message)
		}
		if r.HTTPStatus != 200 || r.Attempts != 1 || r.Arguments != `{"value":42}` {
			t.Errorf("%s http=%d attempts=%d args=%q", mode, r.HTTPStatus, r.Attempts, r.Arguments)
		}
	}

	// 请求保真：KVV 关键字段逐项在场，且不带 temperature
	body := bodies[0]
	for _, want := range []string{
		`"tool_choice":"required"`, `"strict":true`, `"name":"kvv_walle_case"`,
		`"max_tokens":2048`, "minimumruntime", `"additionalProperties":false`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("请求体缺少 %s；body=%s", want, body)
		}
	}
	if strings.Contains(body, "temperature") {
		t.Errorf("请求体不应带 temperature；body=%s", body)
	}
}

func TestRunViolatedSchemaFail(t *testing.T) {
	// value 是字符串，不满足 integer 约束 → violated
	results := runFake(t, toolCallHandler(`{"value":"nope"}`), probe.Params{Concurrency: 2})
	for mode, r := range results {
		if r.Status != probe.StatusViolated {
			t.Errorf("%s status = %s, 期望 violated", mode, r.Status)
		}
		if !strings.Contains(r.Message, "Schema 校验失败") {
			t.Errorf("%s message = %q", mode, r.Message)
		}
		if r.Arguments != `{"value":"nope"}` {
			t.Errorf("%s 违规证据 arguments = %q", mode, r.Arguments)
		}
	}
}

func TestRunViolatedNoToolCall(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "我拒绝调用工具"}}}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
			return
		}
		writeJSONResp(w, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "我拒绝调用工具"}}}})
	})
	results := runFake(t, handler, probe.Params{Concurrency: 2})
	for mode, r := range results {
		if r.Status != probe.StatusViolated {
			t.Errorf("%s status = %s (%s), 期望 violated", mode, r.Status, r.Message)
		}
	}
}

func TestRunRejected(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	})
	results := runFake(t, handler, probe.Params{Concurrency: 2})
	for mode, r := range results {
		if r.Status != probe.StatusRejected || r.HTTPStatus != 429 {
			t.Errorf("%s status=%s http=%d, 期望 rejected/429", mode, r.Status, r.HTTPStatus)
		}
		if !strings.Contains(r.Message, "rate limited") {
			t.Errorf("%s message 应含上游错误体，= %q", mode, r.Message)
		}
	}
}

func TestRunReruns(t *testing.T) {
	// 每个模式第一次 500，重跑后成功 → 末次结果 passed，attempts=2
	var mu sync.Mutex
	seen := map[bool]int{}
	inner := toolCallHandler(`{"value":7}`)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(raw, &req)
		stream := req["stream"] == true
		mu.Lock()
		seen[stream]++
		n := seen[stream]
		mu.Unlock()
		if n == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(raw)))
		inner(w, r)
	})
	results := runFake(t, handler, probe.Params{Concurrency: 2, Reruns: 2})
	for mode, r := range results {
		if r.Status != probe.StatusPassed || r.Attempts != 2 {
			t.Errorf("%s status=%s attempts=%d, 期望 passed/2", mode, r.Status, r.Attempts)
		}
	}
}

// runFakeAll 同 runFake，但收集每个模式按序的全部上报（在途 + 终态），用于验证重试注记。
func runFakeAll(t *testing.T, handler http.Handler, params probe.Params) map[string][]probe.CaseResult {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	schema, err := probe.DecodeUseNumber([]byte(testSchemaJSON))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	cases := []Case{{Suite: "TestFake", Line: 1, Reason: "object_parameter_schema", Schema: schema}}

	var mu sync.Mutex
	reports := map[string][]probe.CaseResult{}
	in := probe.RunInput{
		Target: probe.Target{BaseURL: srv.URL, Model: "test-model"},
		APIKey: "sk-test",
		Params: params,
		Client: srv.Client(),
		Report: func(_ context.Context, r probe.CaseResult) error {
			mu.Lock()
			defer mu.Unlock()
			reports[r.Mode] = append(reports[r.Mode], r)
			return nil
		},
	}
	if err := runSlots(context.Background(), in, cases, []*jsonschema.Schema{compiled}); err != nil {
		t.Fatalf("runSlots: %v", err)
	}
	return reports
}

// 重试注记只出现在非末轮：rejected 每轮都落库，非末轮 message 带「后重试」，末轮干净。
// 「0ms 后重试」同时证明 Retry-After: 0 被解析采纳（否则退避应为 1s）。
func TestRetryAnnotationRejected(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0")
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	})
	reports := runFakeAll(t, handler, probe.Params{Concurrency: 2, Reruns: 1})
	for _, mode := range []string{probe.ModeNonStream, probe.ModeStream} {
		rs := reports[mode]
		if len(rs) != 2 {
			t.Fatalf("%s 上报次数 = %d, want 2（每轮各一次）", mode, len(rs))
		}
		first, last := rs[0], rs[1]
		if first.Status != probe.StatusRejected ||
			!strings.Contains(first.Message, "第 1/2 次尝试失败") || !strings.Contains(first.Message, "0ms 后重试") {
			t.Fatalf("%s 非末轮行缺重试注记: status=%s message=%s", mode, first.Status, first.Message)
		}
		if last.Status != probe.StatusRejected || last.Attempts != 2 {
			t.Fatalf("%s 末轮 status=%s attempts=%d, want rejected/2", mode, last.Status, last.Attempts)
		}
		if strings.Contains(last.Message, "后重试") {
			t.Fatalf("%s 末轮不应残留重试注记: %s", mode, last.Message)
		}
	}
}

// toolschema 连 violated 也重试（对齐 KVV），注记同样适用；200 无 Retry-After → 退避 1s。
func TestRetryAnnotationViolated(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		writeJSONResp(w, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "拒绝调用"}}}})
	})
	reports := runFakeAll(t, handler, probe.Params{Concurrency: 2, Reruns: 1})
	for _, mode := range []string{probe.ModeNonStream, probe.ModeStream} {
		rs := reports[mode]
		if len(rs) != 2 {
			t.Fatalf("%s 上报次数 = %d, want 2", mode, len(rs))
		}
		if rs[0].Status != probe.StatusViolated || !strings.Contains(rs[0].Message, "1s 后重试") {
			t.Fatalf("%s 非末轮 violated 行缺退避注记: status=%s message=%s", mode, rs[0].Status, rs[0].Message)
		}
		if rs[1].Status != probe.StatusViolated || strings.Contains(rs[1].Message, "后重试") {
			t.Fatalf("%s 末轮: status=%s message=%s", mode, rs[1].Status, rs[1].Message)
		}
	}
}

func TestExtractStreamMultiIndex(t *testing.T) {
	// index 0 是别的工具，index 1 才是目标：必须按 name 精确匹配选中 index 1
	chunk := func(tc map[string]any) string {
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{tc}}}}})
		return "data: " + string(b) + "\n"
	}
	sse := chunk(map[string]any{"index": 0, "function": map[string]any{"name": "other_tool", "arguments": "{}"}}) +
		chunk(map[string]any{"index": 1, "function": map[string]any{"name": toolName, "arguments": `{"val`}}) +
		chunk(map[string]any{"index": 1, "function": map[string]any{"arguments": `ue":1}`}}) +
		"data: [DONE]\n"
	args, violation, err := extractStreamArguments(strings.NewReader(sse))
	if err != nil || violation != "" {
		t.Fatalf("err=%v violation=%q", err, violation)
	}
	if args != `{"value":1}` {
		t.Errorf("重组 arguments = %q", args)
	}
}
