package stability

import (
	"math"
	"sort"
	"time"
)

// percentile 线性插值分位数（对齐 numpy.percentile 默认 type-7，与 vLLM bench 口径一致）。
// sorted 须已升序；q ∈ [0,100]。结果四舍五入到整毫秒（亚毫秒精度对延迟无意义）。
func percentile(sorted []int, q float64) int {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	rank := q / 100 * float64(n-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return int(math.Round(float64(sorted[lo]) + frac*float64(sorted[hi]-sorted[lo])))
}

// summarize 把一组延迟观测（毫秒）聚合成分位数摘要；空则返回 nil（指标里 omitempty）。
func summarize(v []int) *Percentiles {
	if len(v) == 0 {
		return nil
	}
	s := append([]int(nil), v...)
	sort.Ints(s)
	var sum int
	for _, x := range s {
		sum += x
	}
	return &Percentiles{
		P50: percentile(s, 50),
		P95: percentile(s, 95),
		P99: percentile(s, 99),
		Min: s[0],
		Max: s[len(s)-1],
		Avg: math.Round(float64(sum)/float64(len(s))*100) / 100,
	}
}

// measured 剔除预热样本
func measured(samples []Sample) []Sample {
	out := samples[:0:0]
	for _, s := range samples {
		if !s.Warmup {
			out = append(out, s)
		}
	}
	return out
}

// aggregate 把一组（已剔除预热的）样本聚合成 Metrics（延迟分位数 + 错误分类 + 吞吐）。
// 延迟分位数只统计成功样本（失败无延迟）；吞吐按样本时间跨度确定性计算。
func aggregate(samples []Sample) Metrics {
	m := Metrics{Requests: len(samples), ByErrorClass: map[string]int{}}
	var ttfb, ttft, total []int
	for _, s := range samples {
		if s.Ok {
			if s.TTFBms >= 0 {
				ttfb = append(ttfb, s.TTFBms)
			}
			if s.TTFTms >= 0 {
				ttft = append(ttft, s.TTFTms)
			}
			if s.TotalMs >= 0 {
				total = append(total, s.TotalMs)
			}
		} else {
			m.Errors++
			if s.ErrorClass != "" {
				m.ByErrorClass[s.ErrorClass]++
			}
		}
	}
	if m.Requests > 0 {
		m.ErrorRate = math.Round(float64(m.Errors)/float64(m.Requests)*10000) / 10000
	}
	if len(m.ByErrorClass) == 0 {
		m.ByErrorClass = nil // 无错误时不落空对象
	}
	m.TTFBms = summarize(ttfb)
	m.TTFTms = summarize(ttft)
	m.TotalMs = summarize(total)
	m.ThroughputRps, m.TokensPerSec = throughput(samples)
	return m
}

// throughput 按成功样本的时间跨度算吞吐：请求/秒 与 生成 token/秒。
// 跨度 = 最早排定到最晚完成（dispatched_at + total_ms）。
func throughput(samples []Sample) (rps float64, tokPerSec float64) {
	var minD, maxC time.Time
	ok := 0
	var outTokens int64
	for _, s := range samples {
		if !s.Ok || s.TotalMs < 0 {
			continue
		}
		d := s.DispatchedAt
		c := s.DispatchedAt.Add(time.Duration(s.TotalMs) * time.Millisecond)
		if ok == 0 {
			minD, maxC = d, c
		} else {
			if d.Before(minD) {
				minD = d
			}
			if c.After(maxC) {
				maxC = c
			}
		}
		ok++
		if s.OutputTokens > 0 {
			outTokens += int64(s.OutputTokens)
		}
	}
	if ok == 0 {
		return 0, 0
	}
	wall := maxC.Sub(minD).Seconds()
	if wall <= 0 {
		return 0, 0
	}
	rps = math.Round(float64(ok)/wall*100) / 100
	tokPerSec = math.Round(float64(outTokens)/wall*100) / 100
	return rps, tokPerSec
}

// evaluateStage 计算某并发档的档级指标（含并发数标注）。
func evaluateStage(probeID, stage string, stageIndex, concurrency int, samples []Sample) StageMetrics {
	m := aggregate(measured(samples))
	m.Concurrency = concurrency
	return StageMetrics{Probe: probeID, Stage: stage, StageIndex: stageIndex, Metrics: m}
}

// evaluateOverall 汇总全部真实档的样本为 __overall__ 行（不标并发、不算跨档吞吐）。
func evaluateOverall(probeID string, samples []Sample) StageMetrics {
	m := aggregate(samples) // 传入的已是各档剔除预热后的样本
	m.ThroughputRps = 0     // 跨档混合吞吐无意义
	m.TokensPerSec = 0
	return StageMetrics{Probe: probeID, Stage: StageOverall, StageIndex: StageOverallIndex, Metrics: m}
}
