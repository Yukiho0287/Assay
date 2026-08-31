// Package stability 稳定性检测：并发/RPM/TPM 等性能指标。与质量检测共享任务编排层，
// 但自成一套 probe / 结果模型 —— 产出的是时序性能指标，装不进「用例×模式→passed 占比」
// 的合规判定模型，故不复用 quality 的 probe.Probe/CaseResult/Checkpoint。
package stability

import (
	"errors"
	"fmt"
)

// StabilityParams 稳定性任务运行参数（落 tasks.params jsonb）。
// Phase 1 仅含阶梯并发字段 + 全局硬闸；RPM/TPM 字段随其 probe 在后续阶段加入。
type StabilityParams struct {
	// Protocol 本任务实选协议（三协议择一），写进快照
	Protocol string `json:"protocol"`

	// —— 阶梯并发（闭环，测延迟曲线）——
	ConcurrencyLadder []int `json:"concurrencyLadder"` // 各并发档，默认 [1,2,4,8,16]
	RequestsPerStage  int   `json:"requestsPerStage"`  // 每档计入统计的请求数，默认 20
	WarmupPerStage    int   `json:"warmupPerStage"`    // 每档预热请求数（评估剔除），默认 2
	LadderMaxTokens   int   `json:"ladderMaxTokens"`   // 每请求生成上限，默认 64

	// —— 全局硬闸（成本护栏，碰任一即停）——
	MaxTotalRequests int `json:"maxTotalRequests"` // 累计请求上限，默认 2000
	MaxTotalTokens   int `json:"maxTotalTokens"`   // 累计 token 上限，默认 2_000_000
	RequestTimeoutMs int `json:"requestTimeoutMs"` // 单请求超时，默认 60000
}

// 默认值常量（前端估算与后端 fail-fast 共用同一套口径）
const (
	DefaultRequestsPerStage = 20
	DefaultWarmupPerStage   = 2
	DefaultLadderMaxTokens  = 64
	DefaultMaxTotalRequests = 2000
	DefaultMaxTotalTokens   = 2_000_000
	DefaultRequestTimeoutMs = 60000
)

// DefaultConcurrencyLadder 默认并发阶梯
func DefaultConcurrencyLadder() []int { return []int{1, 2, 4, 8, 16} }

// ApplyDefaults 补齐零值为默认值。WarmupPerStage=0 是合法配置（不预热），不补默认。
func (p *StabilityParams) ApplyDefaults() {
	if len(p.ConcurrencyLadder) == 0 {
		p.ConcurrencyLadder = DefaultConcurrencyLadder()
	}
	if p.RequestsPerStage == 0 {
		p.RequestsPerStage = DefaultRequestsPerStage
	}
	if p.LadderMaxTokens == 0 {
		p.LadderMaxTokens = DefaultLadderMaxTokens
	}
	if p.MaxTotalRequests == 0 {
		p.MaxTotalRequests = DefaultMaxTotalRequests
	}
	if p.MaxTotalTokens == 0 {
		p.MaxTotalTokens = DefaultMaxTotalTokens
	}
	if p.RequestTimeoutMs == 0 {
		p.RequestTimeoutMs = DefaultRequestTimeoutMs
	}
}

// Validate 入口即验（fail-fast）：非法参数在创建任务时立即拒绝，不带进执行期。
func (p StabilityParams) Validate() error {
	if p.Protocol == "" {
		return errors.New("必须指定协议")
	}
	if len(p.ConcurrencyLadder) == 0 || len(p.ConcurrencyLadder) > 20 {
		return errors.New("并发阶梯需 1-20 档")
	}
	for _, c := range p.ConcurrencyLadder {
		if c < 1 || c > 512 {
			return fmt.Errorf("并发档 %d 越界（需 1-512）", c)
		}
	}
	if p.RequestsPerStage < 1 || p.RequestsPerStage > 1000 {
		return errors.New("每档请求数需 1-1000")
	}
	if p.WarmupPerStage < 0 || p.WarmupPerStage > 100 {
		return errors.New("每档预热数需 0-100")
	}
	if p.LadderMaxTokens < 1 || p.LadderMaxTokens > 4096 {
		return errors.New("生成上限需 1-4096")
	}
	if p.MaxTotalRequests < 1 {
		return errors.New("总请求上限须为正")
	}
	if p.MaxTotalTokens < 1 {
		return errors.New("总 token 上限须为正")
	}
	if p.RequestTimeoutMs < 1000 || p.RequestTimeoutMs > 600000 {
		return errors.New("单请求超时需 1000-600000ms")
	}
	return nil
}
