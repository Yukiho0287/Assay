package stability

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	rpmProbeID = "rpm_probe"
	// rpmPrompt 固定小 prompt：RPM 只关心请求到达率，生成量取小（RpmMaxTokens）即可
	rpmPrompt = "用一句话简要介绍你自己。"
	// rpmRateTol 二分收敛精度：档间速率差窄于此即停（≈30 RPM 分辨率）
	rpmRateTol = 0.5
)

// NewRpmProbe RPM 实测检测项（开环）：恒定到达率阶梯升压，出现持续 429 后二分收敛真实 RPM 边界。
func NewRpmProbe() Probe {
	return Probe{
		Info: Info{
			ID:          rpmProbeID,
			Name:        "RPM 实测",
			Description: "开环恒定到达率发压：从起始速率几何递增，出现持续 429 后在通过/限速档间二分，收敛渠道可持续的每分钟请求数（RPM）边界，并采集限速响应头。",
			Protocols:   nil, // 三协议通用
			EstRequests: estRpmRequests,
		},
		Run: runRpm,
	}
}

// rampRates 几何递增速率序列：start, 2·start, 4·start … 直到（含）max。确定性、无副作用。
func rampRates(start, max float64) []float64 {
	if start <= 0 {
		start = 1
	}
	if max < start {
		max = start
	}
	var rates []float64
	r := start
	for {
		rates = append(rates, r)
		if r >= max {
			break
		}
		r *= 2
		if r > max {
			r = max
		}
	}
	return rates
}

// perStageEst 单档最坏请求数上界：ceil(rate×秒) + 1（含 tick0）。
func perStageEst(rate float64, sec int) int {
	return int(math.Ceil(rate*float64(sec))) + 1
}

// estRpmRequests 最坏预估：ramp 各档按其速率、binary 各档按护栏顶速率之和。
func estRpmRequests(p StabilityParams) int {
	total := 0
	for _, r := range rampRates(p.RpmStartRate, p.RpmMaxRate) {
		total += perStageEst(r, p.RpmStageSec)
	}
	total += p.RpmBinarySteps * perStageEst(p.RpmMaxRate, p.RpmStageSec)
	return total
}

// rpmStageLabel 速率档标识（图表 x 轴），如 r2 / r6.5。distinct float64 → distinct 串，
// 单次运行内 ramp/binary 各档速率互异，不会撞 stability_metrics 的 (task,probe,stage) 唯一约束。
func rpmStageLabel(rate float64) string { return fmt.Sprintf("r%g", rate) }

// isRateLimited 本档是否判定为触发限速：429 样本占比 ≥ 阈值。仅认 429（RPM 的限速信号），
// 传输/5xx 等其它失败不计入速率边界，由全局硬闸兜底防跑飞。
func isRateLimited(samples []Sample, threshold float64) bool {
	if len(samples) == 0 {
		return false
	}
	limited := 0
	for _, s := range samples {
		if s.ErrorClass == ErrRateLimited {
			limited++
		}
	}
	return float64(limited)/float64(len(samples)) >= threshold
}

func runRpm(ctx context.Context, in RunInput) error {
	p := in.Params
	total := estRpmRequests(p)
	var pmu sync.Mutex
	done := 0
	afterEach := func() {
		pmu.Lock()
		done++
		if in.Progress != nil {
			in.Progress(ctx, done, total)
		}
		pmu.Unlock()
	}
	stageDur := time.Duration(p.RpmStageSec) * time.Second

	var (
		overall     []Sample
		lastHeaders map[string]string
		stageIndex  int
		lo          float64 // 已知可持续的最高速率
		hi          float64 // 已知触发限速的最低速率
		haveHi      bool
		stopped     bool
	)

	// runOne 跑一档：发压→评估→落档级指标→并入 overall；返回本档是否触发限速。
	runOne := func(rate float64) (bool, error) {
		stage := rpmStageLabel(rate)
		cfg := pacedStageConfig{
			TargetRate:  rate,
			MaxTokens:   p.RpmMaxTokens,
			Prompt:      rpmPrompt,
			Duration:    stageDur,
			MaxInFlight: p.RpmMaxInFlight,
		}
		res, err := runPacedStage(ctx, in, stageIndex, stage, cfg, afterEach)
		if err != nil {
			return false, err
		}
		limited := isRateLimited(res.Samples, p.RpmLimitThreshold)
		achieved := 0.0
		if res.DurationSec > 0 {
			achieved = math.Round(float64(res.Dispatched)/res.DurationSec*100) / 100
		}
		if res.RateHeaders != nil {
			lastHeaders = res.RateHeaders
		}
		sm := evaluatePacedStage(in.Probe, stage, stageIndex, rate, achieved, limited, res.RateHeaders, res.Samples)
		if in.Metric != nil {
			if err := in.Metric(ctx, sm); err != nil {
				return false, err
			}
		}
		overall = append(overall, res.Samples...)
		stageIndex++
		if res.Stopped {
			stopped = true
		}
		return limited, nil
	}

	// —— ramp：几何递增，撞硬闸 / 触发限速 / 到护栏顶即止 ——
	for _, rate := range rampRates(p.RpmStartRate, p.RpmMaxRate) {
		limited, err := runOne(rate)
		if err != nil {
			return err
		}
		if stopped || ctx.Err() != nil {
			break
		}
		if limited {
			hi, haveHi = rate, true
			break
		}
		lo = rate
	}

	// —— binary：找到限速档后，在通过档 lo 与限速档 hi 间二分细化真实边界 ——
	if haveHi && !stopped {
		for i := 0; i < p.RpmBinarySteps && (hi-lo) > rpmRateTol; i++ {
			if ctx.Err() != nil {
				break
			}
			mid := (lo + hi) / 2
			limited, err := runOne(mid)
			if err != nil {
				return err
			}
			if stopped {
				break
			}
			if limited {
				hi = mid
			} else {
				lo = mid
			}
		}
	}

	// reachedCap：一路升到护栏顶仍未触发限速 → 真实边界 ≥ 护栏，未探到顶
	reachedCap := !haveHi && lo >= p.RpmMaxRate-1e-9
	om := evaluateRpmOverall(in.Probe, overall, lo*60, reachedCap, lastHeaders)
	if in.Metric != nil {
		if err := in.Metric(ctx, om); err != nil {
			return err
		}
	}
	return nil
}
