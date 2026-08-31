// Package probe 定义检测项（probe）的公共数据结构。
// 检测项是质量检测的基本单元：独立、可组合、带元数据；具体实现放各自子包，
// 由 registry 显式注册。这里只有纯结构体，不做接口抽象（实用主义：一个实现不值一个接口）。
package probe

import (
	"context"
	"net/http"
	"slices"
)

// Target 检测对象参数快照（渠道 × 模型条目），任务创建时写进 tasks.target jsonb。
// 证据链要求：历史报告自足，不受渠道后续编辑/删除影响。绝不含 API key。
type Target struct {
	ChannelID        string   `json:"channelId,omitempty"`
	ChannelName      string   `json:"channelName"`
	BaseURL          string   `json:"baseUrl"`
	ModelEntryID     string   `json:"modelEntryId,omitempty"`
	Model            string   `json:"model"`
	Protocols        []string `json:"protocols"`
	Currency         string   `json:"currency,omitempty"`
	InputPrice       *float64 `json:"inputPrice,omitempty"`
	OutputPrice      *float64 `json:"outputPrice,omitempty"`
	CachedInputPrice *float64 `json:"cachedInputPrice,omitempty"`
}

// Params 任务运行参数，创建任务时由 API 层校验并落默认值后写进 tasks.params jsonb。
type Params struct {
	Concurrency int `json:"concurrency"`        // 单任务内并发请求数（1-16，默认 4）
	Reruns      int `json:"reruns"`             // 失败槽位重跑轮数（0-5，默认 2），末次结果为准
	MaxCases    int `json:"maxCases,omitempty"` // 用例数上限，0 = 全量
}

// 用例判定结果三终态 + 一中间态。故意偏离 KVV：其 infra_error（HTTP/传输错误）
// 并入 rejected，因为对渠道检测而言"上游拒绝服务"本身就是被测行为，不是待剔除的基建噪声。
const (
	StatusPassed   = "passed"   // 合规
	StatusRejected = "rejected" // 请求被拒（任何 HTTP 非 2xx / 传输错误 / 超时）
	StatusViolated = "violated" // HTTP 200 但响应不合规
	// StatusCollected 已采集·待评估：跨行断言的检测项在阶段二前的中间态，
	// 只展示原始计数不给结论（提前判 passed 可能被翻案），评估后必被终态覆盖
	StatusCollected = "collected"
)

// 请求模式：同一用例分别以非流式与流式各测一次。
const (
	ModeNonStream = "non_stream"
	ModeStream    = "stream"
)

// CaseResult 一个槽位（用例 × 模式）的判定结果，对应 task_case_results 一行。
type CaseResult struct {
	Probe           string
	Suite           string
	Line            int // 套件文件内行号，1 起，与 KVV 官方报告对齐
	Mode            string
	SelectionReason string
	Status          string
	Message         string
	HTTPStatus      int // 0 = 未收到 HTTP 响应（传输层失败）
	LatencyMs       int
	Arguments       string // 提取到的工具调用参数（截断），仅拿到时非空
	Attempts        int    // 含重跑的累计尝试次数
}

// RunInput 检测项运行所需的全部输入；回调由 worker 提供（落库 + 进度通知）。
type RunInput struct {
	Target Target
	APIKey string // worker 执行时按 channel_id 现读，绝不进快照
	Params Params
	Client *http.Client

	// Report 每个槽位出结果立即回调（UPSERT 落库）；返回错误则整个任务中止。
	Report func(ctx context.Context, r CaseResult) error
	// Progress 槽位首次完成后回调（写进度 + pg_notify）。
	Progress func(ctx context.Context, done, total int)
}

// Checkpoint 评分检查点：把检测项的用例结果聚合成一个带权重的评分单元，评分板按检查点行展示。
// 检查点得分 = 命中用例中 passed 占比（rejected 与 violated 均计失败——上游拒绝服务本身就是被测行为）；
// 检测项得分 = 各检查点得分按 Weight 加权平均（total=0 的未采样检查点不参与）。
type Checkpoint struct {
	ID     string   // 稳定标识，进导出报告，不可随意改
	Name   string   // 中文展示名
	Weight float64  // 同一检测项内的相对权重，须 > 0
	Suites []string // 命中套件过滤器，nil = 不限
	Modes  []string // 命中模式过滤器，nil = 不限
}

// Matches 判定一条用例结果是否归入本检查点。
func (c Checkpoint) Matches(suite, mode string) bool {
	if len(c.Suites) > 0 && !slices.Contains(c.Suites, suite) {
		return false
	}
	if len(c.Modes) > 0 && !slices.Contains(c.Modes, mode) {
		return false
	}
	return true
}

// Info 检测项元数据，与 api.ProbeInfo 一一对应。
type Info struct {
	ID           string
	Name         string
	Description  string
	CostTier     string // cheap | medium | expensive
	Protocols    []string
	NeedsControl bool
	NeedsPricing bool
	CaseCount    int
	// RequestsPerCase 每用例请求数（toolschema 非流式+流式=2），前端据此估算请求量。
	RequestsPerCase int
	// SupportsMaxCases 是否受「用例数上限」参数影响；固定请求矩阵的检测项为 false。
	SupportsMaxCases bool
	// Checkpoints 评分检查点（至少一个，registry 启动时校验），声明序即评分板展示序。
	Checkpoints []Checkpoint
}

// Probe 一个检测项：元数据 + 槽位数预计算（创建任务时算 progress_total）+ 运行入口。
type Probe struct {
	Info      Info
	SlotCount func(p Params) int
	Run       func(ctx context.Context, in RunInput) error
}
