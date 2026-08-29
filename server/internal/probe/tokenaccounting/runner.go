package tokenaccounting

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Yukiho0287/assay/server/internal/probe"
)

const (
	probeID = "token_accounting"

	// requestTimeout 单请求硬超时（与 toolschema 口径一致）
	requestTimeout = 120 * time.Second
	// maxResponseBytes 响应体安全上限；usage 响应极小，超限即异常
	maxResponseBytes = 4 << 20

	// asciiAllowance 绝对上限的聊天模板开销余量：pt ≤ chars + asciiAllowance。
	// 模板 token 是常数级（通常几十以内），100 足够宽；边际率断言不受此常数影响（差分消掉）。
	asciiAllowance = 100
	// marginalRateLimit 纯 ASCII 文本每字符至多 1 token（BPE 只合并不拆分单字节）
	marginalRateLimit = 1.0
	// driftLimitPct 边际率漂移阈值（百分比），与 marginal_rate.py 判定一致：≥3% 违规
	driftLimitPct = 3.0
)

// 套件名同时充当 selection_reason，喂报告的按分类聚合。
const (
	suiteMarginal    = "marginal"           // 4 档长度，恒定边际率 + 漂移
	suiteDeterminism = "determinism"        // 同字节三连发，同计数
	suiteStream      = "stream_consistency" // 流式/非流式 prompt_tokens 一致
)

// slotDef 一个槽位的静态定义：请求矩阵编译期固定，不受 maxCases 影响。
type slotDef struct {
	suite string
	line  int
	mode  string
	chars int
}

func slotDefs() []slotDef {
	defs := make([]slotDef, 0, 9)
	for i := 1; i <= levels; i++ {
		defs = append(defs, slotDef{suiteMarginal, i, probe.ModeNonStream, step * i})
	}
	for i := 1; i <= 3; i++ {
		defs = append(defs, slotDef{suiteDeterminism, i, probe.ModeNonStream, 2 * step})
	}
	defs = append(defs, slotDef{suiteStream, 1, probe.ModeNonStream, step})
	defs = append(defs, slotDef{suiteStream, 1, probe.ModeStream, step})
	return defs
}

// New 注册「token 记账自洽」检测项。
func New() probe.Probe {
	defs := slotDefs()
	return probe.Probe{
		Info: probe.Info{
			ID:               probeID,
			Name:             "token 记账自洽",
			Description:      "向渠道发送确定性纯 ASCII 样本（4 档长度 + 同文三连发 + 流式/非流式对照，共 9 个 max_tokens=4 的最小请求），校验 usage 记账是否数学自洽：total=prompt+completion 恒等式、纯 ASCII token 上限、同字节同计数、恒定边际率与漂移。只测自洽不比官方，无需对照渠道。",
			CostTier:         "cheap",
			Protocols:        []string{"openai_chat"},
			CaseCount:        len(defs),
			RequestsPerCase:  1,
			SupportsMaxCases: false, // 固定请求矩阵：断言相互依赖，砍任何一格都破坏套件语义
		},
		SlotCount: func(probe.Params) int { return len(defs) },
		Run:       run,
	}
}

// slotFetch 一个槽位的采集状态。status 为空 = 拿到 usage，进入断言阶段；
// rejected/violated = 请求层已定型。
type slotFetch struct {
	def        slotDef
	attempts   int
	status     string
	message    string
	httpStatus int
	latencyMs  int
	usage      usageData
}

// run 两阶段执行：阶段一并发采集 9 个请求的 usage（进度实时跳），
// 阶段二数据齐后统一评估断言、按固定顺序落库 9 行。
//
// 重试口径（对 toolschema 的故意偏离）：只重试 rejected（网络/HTTP 层瞬态）。
// violated 是测量事实——如确定性破坏，重跑到"碰巧一致"反而掩盖问题，故不重试。
func run(ctx context.Context, in probe.RunInput) error {
	client := in.Client
	if client == nil {
		client = http.DefaultClient
	}
	concurrency := in.Params.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	defs := slotDefs()
	fetched := make([]*slotFetch, len(defs))
	for i := range defs {
		fetched[i] = &slotFetch{def: defs[i]}
	}

	var mu sync.Mutex
	done := 0
	total := len(defs)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)
	for _, s := range fetched {
		g.Go(func() error {
			for {
				if gctx.Err() != nil {
					return gctx.Err()
				}
				s.attempts++
				fetchOne(gctx, in, client, s)
				if s.status != probe.StatusRejected || s.attempts > in.Params.Reruns {
					break
				}
			}
			// 中止时不再上报进度（与 toolschema 同理：取消不是渠道行为）
			if gctx.Err() != nil {
				return gctx.Err()
			}
			// 进度回调必须在锁内：保证 done 的落库按递增序发生，
			// 否则并发完成时旧值可能后写，任务终态进度停在中间值
			mu.Lock()
			done++
			if in.Progress != nil {
				in.Progress(gctx, done, total)
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	for _, res := range evaluate(fetched) {
		if err := in.Report(ctx, res); err != nil {
			return fmt.Errorf("结果落库失败: %w", err)
		}
	}
	return nil
}

// fetchOne 执行单个请求并提取 usage；每次重试前重置本槽位状态。
func fetchOne(ctx context.Context, in probe.RunInput, client *http.Client, s *slotFetch) {
	s.status, s.message, s.httpStatus, s.latencyMs = "", "", 0, 0
	s.usage = usageData{}
	rejected := func(msg string) {
		s.status = probe.StatusRejected
		s.message = msg
	}

	body := map[string]any{
		"model":      in.Target.Model,
		"messages":   []any{map[string]any{"role": "user", "content": sampleText(s.def.chars)}},
		"max_tokens": 4, // 只要 usage，不要生成内容；不带 temperature（与 marginal_rate.py 一致）
	}
	if s.def.mode == probe.ModeStream {
		body["stream"] = true
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	payload, err := probe.MarshalNoEscape(body)
	if err != nil {
		rejected("构造请求失败: " + err.Error())
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, in.Target.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		rejected("构造请求失败: " + err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+in.APIKey)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		s.latencyMs = int(time.Since(start).Milliseconds())
		rejected("请求失败: " + err.Error())
		return
	}
	defer resp.Body.Close()
	s.httpStatus = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		s.latencyMs = int(time.Since(start).Milliseconds())
		msg := strings.TrimSpace(string(snippet))
		if msg == "" {
			msg = resp.Status
		}
		rejected("HTTP " + strconv.Itoa(resp.StatusCode) + ": " + msg)
		return
	}

	var u usageData
	var violation string
	if s.def.mode == probe.ModeStream {
		u, violation, err = extractStreamUsage(resp.Body)
	} else {
		u, violation, err = extractNonStreamUsage(resp.Body)
	}
	s.latencyMs = int(time.Since(start).Milliseconds())
	if err != nil {
		rejected("读取响应失败: " + err.Error())
		return
	}
	if violation != "" {
		s.status = probe.StatusViolated
		s.message = violation
		return
	}
	s.usage = u
}

// evaluate 数据齐后统一评估断言，返回与 slotDefs 同序的 9 行结果。
// 跨行断言（边际率/确定性/流式一致）的依赖行数据缺失时：message 注明、不判违规。
func evaluate(fetched []*slotFetch) []probe.CaseResult {
	get := func(suite string, line int, mode string) *slotFetch {
		for _, s := range fetched {
			if s.def.suite == suite && s.def.line == line && s.def.mode == mode {
				return s
			}
		}
		return nil
	}

	results := make([]probe.CaseResult, 0, len(fetched))
	for _, s := range fetched {
		res := probe.CaseResult{
			Probe:           probeID,
			Suite:           s.def.suite,
			Line:            s.def.line,
			Mode:            s.def.mode,
			SelectionReason: s.def.suite,
			HTTPStatus:      s.httpStatus,
			LatencyMs:       s.latencyMs,
			Attempts:        s.attempts,
		}
		if s.status != "" { // 请求层已定型（rejected / 无 usage 的 violated）
			res.Status = s.status
			res.Message = truncateMessage(s.message)
			results = append(results, res)
			continue
		}

		u := s.usage
		res.Arguments = truncateRunes(u.Raw, 1000)
		var viols, notes []string
		notes = append(notes, fmt.Sprintf("pt=%d ct=%d total=%d chars=%d", u.Prompt, u.Completion, u.Total, s.def.chars))
		if u.Cached >= 0 {
			notes = append(notes, fmt.Sprintf("cached=%d", u.Cached))
		}

		// ② 恒等式：只断言顶层三元组，不受 details 细分（如 reasoning tokens）影响
		if u.Total != u.Prompt+u.Completion {
			viols = append(viols, fmt.Sprintf("恒等式破坏: total=%d ≠ prompt=%d + completion=%d", u.Total, u.Prompt, u.Completion))
		}
		// ③ 纯 ASCII 绝对上限
		if u.Prompt > int64(s.def.chars)+asciiAllowance {
			viols = append(viols, fmt.Sprintf("纯 ASCII 上限破坏: pt=%d > chars=%d + 模板余量 %d", u.Prompt, s.def.chars, asciiAllowance))
		}

		switch s.def.suite {
		case suiteMarginal:
			if s.def.line >= 2 {
				prev := get(suiteMarginal, s.def.line-1, probe.ModeNonStream)
				if prev != nil && prev.status == "" {
					rate := float64(u.Prompt-prev.usage.Prompt) / step
					notes = append(notes, fmt.Sprintf("边际率=%.4f", rate))
					if rate > marginalRateLimit {
						viols = append(viols, fmt.Sprintf("边际率 %.4f > %.1f（纯 ASCII 每字符至多 1 token；差分已消模板常数）", rate, marginalRateLimit))
					}
				} else {
					notes = append(notes, "上一档数据缺失，边际率不判")
				}
			}
			if s.def.line == levels {
				rates := marginalRates(fetched)
				if len(rates) < levels-1 {
					notes = append(notes, fmt.Sprintf("边际率仅 %d/%d 档可算，漂移不判", len(rates), levels-1))
					break
				}
				lo, hi := rates[0], rates[0]
				for _, r := range rates[1:] {
					lo, hi = min(lo, r), max(hi, r)
				}
				if lo <= 0 {
					notes = append(notes, "存在非正边际率，漂移不判")
					break
				}
				drift := (hi - lo) / lo * 100
				notes = append(notes, fmt.Sprintf("漂移=%.2f%%", drift))
				if drift >= driftLimitPct {
					viols = append(viols, fmt.Sprintf("边际率漂移 %.2f%% ≥ %.0f%%（恒定边际率被破坏，计数与内容长度非线性）", drift, driftLimitPct))
				}
			}
		case suiteDeterminism:
			// 基准 = 首个拿到 usage 的行。同字节必须同计数：
			// 渠道命中缓存只应改 cached 字段，不应改 prompt_tokens
			var ref *slotFetch
			for i := 1; i <= 3; i++ {
				if c := get(suiteDeterminism, i, probe.ModeNonStream); c != nil && c.status == "" {
					ref = c
					break
				}
			}
			if ref != nil && ref != s {
				if u.Prompt != ref.usage.Prompt {
					viols = append(viols, fmt.Sprintf("确定性破坏: pt=%d ≠ 基准行(第%d发) pt=%d（同字节应同计数）", u.Prompt, ref.def.line, ref.usage.Prompt))
				} else {
					notes = append(notes, fmt.Sprintf("与基准行(第%d发)一致", ref.def.line))
				}
			}
		case suiteStream:
			if s.def.mode == probe.ModeStream {
				ns := get(suiteStream, 1, probe.ModeNonStream)
				if ns != nil && ns.status == "" {
					if u.Prompt != ns.usage.Prompt {
						viols = append(viols, fmt.Sprintf("流式 pt=%d ≠ 非流式 pt=%d（completion 因生成随机性不比）", u.Prompt, ns.usage.Prompt))
					} else {
						notes = append(notes, "与非流式 pt 一致")
					}
				} else {
					notes = append(notes, "非流式对照数据缺失，一致性不判")
				}
			}
		}

		if len(viols) > 0 {
			res.Status = probe.StatusViolated
			res.Message = truncateMessage(strings.Join(append(viols, notes...), "; "))
		} else {
			res.Status = probe.StatusPassed
			res.Message = truncateMessage(strings.Join(notes, "; "))
		}
		results = append(results, res)
	}
	return results
}

// marginalRates 收集全部可算的边际率（相邻两档都拿到 usage 才可算）。
func marginalRates(fetched []*slotFetch) []float64 {
	var rates []float64
	var prev *slotFetch
	for _, s := range fetched {
		if s.def.suite != suiteMarginal {
			continue
		}
		if s.status == "" && prev != nil && prev.status == "" {
			rates = append(rates, float64(s.usage.Prompt-prev.usage.Prompt)/step)
		}
		prev = s
	}
	return rates
}

// truncateMessage 与 toolschema 同口径：换行压成 \n 字面量，超 500 字符截断。
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
