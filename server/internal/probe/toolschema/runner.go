package toolschema

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/sync/errgroup"

	"github.com/Yukiho0287/assay/server/internal/probe"
)

const (
	probeID         = "tool_call_json_schema"
	toolName        = "kvv_walle_case"
	toolDescription = "Submit minimal JSON arguments that validate against this JSON Schema."
	// kvvPrompt 逐字复制自 validator.py（含 "minimumruntime" 缺空格的原版笔误，保真不修）
	kvvPrompt = "Call the kvv_walle_case tool exactly once with minimumruntime arguments that satisfy its parameter schema, try your best to create the arguments. Do not copy or describe the JSON Schema itself. Do not include schema keywords like type, properties, required, or additionalProperties unless the schema explicitly requires them as argument property names. If the schema defines a top-level value argument, provide the minimal valid value for it. Always include every required property. Respect minItems, minProperties, enum, const, minimum, and minLength constraints. Prefer empty arrays and empty objects only when those constraints allow them. Do not answer with plain text."

	// requestTimeout 单请求总超时。与 KVV（httpx 读超时 120s，逐次读间隔计时）口径略异：
	// Go 这里是整请求 120s 硬上限，更严格且不会被慢滴流拖死
	requestTimeout = 120 * time.Second
	// maxResponseBytes 响应体安全上限；正常响应远小于此，超限会解析失败判 rejected
	maxResponseBytes = 16 << 20
)

// New 注册「工具调用 JSON Schema 遵从」检测项。
func New() probe.Probe {
	caseCount := len(SelectedCases())
	return probe.Probe{
		Info: probe.Info{
			ID:               probeID,
			Name:             "工具调用 JSON Schema 遵从",
			Description:      "发送带 JSON Schema 约束的工具定义（tool_choice=required），校验模型返回的工具调用参数是否符合 Draft 2020-12 Schema。每个用例分别以非流式与流式各测一次。移植自 KVV tool_call_json_schema。",
			CostTier:         "medium",
			Protocols:        []string{"openai_chat"},
			CaseCount:        caseCount,
			RequestsPerCase:  2, // 非流式 + 流式
			SupportsMaxCases: true,
		},
		SlotCount: func(p probe.Params) int {
			n := caseCount
			if p.MaxCases > 0 && p.MaxCases < n {
				n = p.MaxCases
			}
			return n * 2 // 非流式 + 流式
		},
		Run: run,
	}
}

// slotState 一个槽位（用例 × 模式）的执行状态，跨重跑轮累积 attempts。
type slotState struct {
	caseIdx  int
	mode     string
	attempts int
	status   string
	counted  bool // 是否已计入 progress done（仅首轮完成计一次）
}

// runState 单次任务运行的共享状态。
type runState struct {
	in       probe.RunInput
	client   *http.Client
	cases    []Case
	compiled []*jsonschema.Schema

	mu    sync.Mutex
	done  int
	total int
}

func run(ctx context.Context, in probe.RunInput) error {
	cases := SelectedCases()
	if in.Params.MaxCases > 0 && in.Params.MaxCases < len(cases) {
		cases = cases[:in.Params.MaxCases]
	}
	// 预编译全部本地校验 schema（金标测试保证 204 个全部可编译，这里失败属程序性错误）
	compiled := make([]*jsonschema.Schema, len(cases))
	for i := range cases {
		s, err := compileSchema(cases[i].Schema)
		if err != nil {
			return fmt.Errorf("用例 %s:%d schema 编译失败: %w", cases[i].Suite, cases[i].Line, err)
		}
		compiled[i] = s
	}
	return runSlots(ctx, in, cases, compiled)
}

// runSlots 扇出执行全部槽位，失败槽位（rejected/violated）重跑 Reruns 轮，末次结果为准。
func runSlots(ctx context.Context, in probe.RunInput, cases []Case, compiled []*jsonschema.Schema) error {
	concurrency := in.Params.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	client := in.Client
	if client == nil {
		client = http.DefaultClient
	}

	slots := make([]*slotState, 0, len(cases)*2)
	for i := range cases {
		for _, mode := range []string{probe.ModeNonStream, probe.ModeStream} {
			slots = append(slots, &slotState{caseIdx: i, mode: mode})
		}
	}
	st := &runState{in: in, client: client, cases: cases, compiled: compiled, total: len(slots)}

	pending := slots
	for round := 0; ; round++ {
		if err := st.runRound(ctx, pending, concurrency); err != nil {
			return err
		}
		var failed []*slotState
		for _, s := range pending {
			if s.status != probe.StatusPassed {
				failed = append(failed, s)
			}
		}
		if len(failed) == 0 || round >= in.Params.Reruns {
			return nil
		}
		pending = failed
	}
}

func (st *runState) runRound(ctx context.Context, pending []*slotState, concurrency int) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)
	for _, s := range pending {
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			s.attempts++
			res := st.runCase(gctx, s)
			// 中止（关停/同轮其他槽位落库失败）时不落污染行：
			// "context canceled" 不是渠道行为，写进证据链会误导报告
			if gctx.Err() != nil {
				return gctx.Err()
			}
			res.Attempts = s.attempts
			s.status = res.Status
			if err := st.in.Report(gctx, res); err != nil {
				return fmt.Errorf("结果落库失败: %w", err)
			}
			// 进度回调必须在锁内：保证 done 的落库按递增序发生，
			// 否则并发完成时旧值可能后写，任务终态进度停在中间值
			st.mu.Lock()
			if !s.counted {
				s.counted = true
				st.done++
			}
			if st.in.Progress != nil {
				st.in.Progress(gctx, st.done, st.total)
			}
			st.mu.Unlock()
			return nil
		})
	}
	return g.Wait()
}

// runCase 执行单个槽位：构造请求 → 发送 → 提取工具调用参数 → 本地 Schema 校验。
// 判定（对 KVV 的故意偏离见 probe 包状态常量注释）：
//
//	任何 HTTP 非 2xx / 传输错误 / 超时 → rejected
//	200 但无合规工具调用 / arguments 非 JSON / Schema 校验失败 → violated
//	否则 → passed
func (st *runState) runCase(ctx context.Context, s *slotState) probe.CaseResult {
	c := &st.cases[s.caseIdx]
	res := probe.CaseResult{Probe: probeID, Suite: c.Suite, Line: c.Line, Mode: s.mode, SelectionReason: c.Reason}
	rejected := func(msg string) probe.CaseResult {
		res.Status = probe.StatusRejected
		res.Message = truncateMessage(msg)
		return res
	}
	violated := func(msg string) probe.CaseResult {
		res.Status = probe.StatusViolated
		res.Message = truncateMessage(msg)
		return res
	}

	payload, err := probe.MarshalNoEscape(requestBody(st.in.Target.Model, c.Schema, s.mode == probe.ModeStream))
	if err != nil {
		return rejected("构造请求失败: " + err.Error())
	}
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, st.in.Target.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return rejected("构造请求失败: " + err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+st.in.APIKey)

	start := time.Now()
	resp, err := st.client.Do(req)
	if err != nil {
		res.LatencyMs = int(time.Since(start).Milliseconds())
		return rejected("请求失败: " + err.Error())
	}
	defer resp.Body.Close()
	res.HTTPStatus = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		res.LatencyMs = int(time.Since(start).Milliseconds())
		msg := strings.TrimSpace(string(snippet))
		if msg == "" {
			msg = resp.Status
		}
		return rejected("HTTP " + strconv.Itoa(resp.StatusCode) + ": " + msg)
	}

	var arguments, violation string
	if s.mode == probe.ModeStream {
		arguments, violation, err = extractStreamArguments(resp.Body)
	} else {
		arguments, violation, err = extractNonStreamArguments(resp.Body)
	}
	res.LatencyMs = int(time.Since(start).Milliseconds()) // 含完整响应体读取（流式即全流耗时）
	if err != nil {
		return rejected("读取响应失败: " + err.Error())
	}
	if violation != "" {
		return violated(violation)
	}

	res.Arguments = truncateRunes(arguments, 1000)
	inst, err := probe.DecodeUseNumber([]byte(arguments))
	if err != nil {
		return violated("arguments 不是合法 JSON: " + err.Error())
	}
	if err := st.compiled[s.caseIdx].Validate(inst); err != nil {
		return violated("Schema 校验失败: " + err.Error())
	}
	res.Status = probe.StatusPassed
	return res
}

// requestBody 与 KVV 逐字段对齐：同 prompt、同工具定义（strict:true）、tool_choice=required、
// max_tokens=2048、不带 temperature。
func requestBody(model string, schema any, stream bool) map[string]any {
	body := map[string]any{
		"model":    model,
		"messages": []any{map[string]any{"role": "user", "content": kvvPrompt}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        toolName,
				"description": toolDescription,
				"parameters":  schema,
				"strict":      true,
			},
		}},
		"tool_choice": "required",
		"max_tokens":  2048,
	}
	if stream {
		body["stream"] = true
	}
	return body
}

// extractNonStreamArguments 从非流式响应提取 kvv_walle_case 的 arguments 字符串。
// 返回 (arguments, violation, err)：err 非空 = 传输/解析层失败（判 rejected）；
// violation 非空 = 响应结构不合规（判 violated）。
func extractNonStreamArguments(body io.Reader) (string, string, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxResponseBytes))
	if err != nil {
		return "", "", err
	}
	doc, err := probe.DecodeUseNumber(data)
	if err != nil {
		return "", "", fmt.Errorf("响应体不是 JSON: %w", err)
	}
	choices := asList(getField(doc, "choices"))
	if len(choices) == 0 {
		return "", "响应没有 choices", nil
	}
	msg := getField(choices[0], "message")
	toolCalls := asList(getField(msg, "tool_calls"))
	if len(toolCalls) == 0 {
		return "", "响应未包含 tool_calls；content=" + previewJSON(getField(msg, "content")), nil
	}
	for _, tc := range toolCalls {
		fn := getField(tc, "function")
		if name, _ := getField(fn, "name").(string); name != toolName {
			continue
		}
		args := getField(fn, "arguments")
		if s, ok := args.(string); ok {
			return s, "", nil
		}
		// arguments 非字符串：序列化后照常校验（KVV 同行为）
		b, err := probe.MarshalNoEscape(args)
		if err != nil {
			return "", "arguments 无法序列化: " + err.Error(), nil
		}
		return string(b), "", nil
	}
	return "", "未调用 " + toolName + "；tool_calls=" + previewJSON(toolCalls), nil
}

// extractStreamArguments 读 SSE 流并按 tool_calls[].index 重组分片（KVV reassemble_stream 移植）。
func extractStreamArguments(body io.Reader) (string, string, error) {
	type entry struct {
		name strings.Builder
		args strings.Builder
	}
	byIndex := map[int]*entry{}

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024) // 单行分片可能很大，放宽扫描缓冲
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[len("data:"):])
		if payload == "" || payload == "[DONE]" {
			continue
		}
		chunk, err := probe.DecodeUseNumber([]byte(payload))
		if err != nil {
			// 分片坏损属传输层问题（OpenAI SDK 同样会抛错），判 rejected
			return "", "", fmt.Errorf("流式分片不是 JSON: %w", err)
		}
		choices := asList(getField(chunk, "choices"))
		if len(choices) == 0 {
			continue
		}
		for _, tc := range asList(getField(getField(choices[0], "delta"), "tool_calls")) {
			idx := 0
			if n, ok := getField(tc, "index").(json.Number); ok {
				if i, err := n.Int64(); err == nil {
					idx = int(i)
				}
			}
			e := byIndex[idx]
			if e == nil {
				e = &entry{}
				byIndex[idx] = e
			}
			fn := getField(tc, "function")
			if s, ok := getField(fn, "name").(string); ok {
				e.name.WriteString(s)
			}
			if s, ok := getField(fn, "arguments").(string); ok {
				e.args.WriteString(s)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", "", err
	}

	if len(byIndex) == 0 {
		return "", "流式响应未包含 tool_calls 分片", nil
	}
	idxs := make([]int, 0, len(byIndex))
	for i := range byIndex {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	names := make([]string, 0, len(idxs))
	for _, i := range idxs {
		if byIndex[i].name.String() == toolName {
			return byIndex[i].args.String(), "", nil
		}
		names = append(names, byIndex[i].name.String())
	}
	return "", "未调用 " + toolName + "；重组到的工具=" + strings.Join(names, ","), nil
}

// getField 宽容取字段：非 map 或键缺失返回 nil（对齐 KVV 对任意响应形状的容错）。
func getField(v any, key string) any {
	if m, ok := v.(map[string]any); ok {
		return m[key]
	}
	return nil
}

func asList(v any) []any {
	list, _ := v.([]any)
	return list
}

// previewJSON 字段值序列化后截断，用于 violation 消息里附带证据。
func previewJSON(v any) string {
	b, err := probe.MarshalNoEscape(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return truncateRunes(string(b), 200)
}

// truncateMessage 移植 KVV _truncate：换行压成 \n 字面量，超 500 字符截断。
func truncateMessage(s string) string {
	return truncateRunes(strings.ReplaceAll(s, "\n", "\\n"), 500)
}

func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit-3]) + "..."
}
