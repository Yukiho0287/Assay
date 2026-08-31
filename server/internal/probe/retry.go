package probe

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// maxBackoff 指数退避封顶：即使多次连败也不把等待拖到不可感知
	maxBackoff = 30 * time.Second
	// maxRetryAfter Retry-After 采纳上限：防病态上游用超长头把重试钉死
	maxRetryAfter = 60 * time.Second
)

// BackoffDelay 第 attempt 次失败后的指数退避时长：1s 起倍增，封顶 30s。
// attempt 从 1 计（首次失败 → 1s）。
func BackoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 { // 1s<<5 已超封顶，直接返回避免移位溢出隐患
		return maxBackoff
	}
	d := time.Second << (attempt - 1)
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

// RetryAfter 解析响应头 Retry-After（RFC 9110：整数秒或 HTTP-date），
// 钳制到 [0, 60s]；缺失或无法解析返回 false。0s 是合法值（上游示意立即重试）。
func RetryAfter(h http.Header) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0, false
		}
		return min(time.Duration(secs)*time.Second, maxRetryAfter), true
	}
	if t, err := http.ParseTime(v); err == nil {
		return min(max(time.Until(t), 0), maxRetryAfter), true
	}
	return 0, false
}

// RetryDelay 一次失败后的重试等待：有可解析的 Retry-After 用之（尊重上游节流），
// 否则按尝试次数指数退避。对一切被重试的状态统一生效，不为 429 单开分支。
func RetryDelay(attempt int, h http.Header) time.Duration {
	if d, ok := RetryAfter(h); ok {
		return d
	}
	return BackoffDelay(attempt)
}

// SleepUntil 睡到 deadline，可被 ctx 取消中断；deadline 已过期时立即返回。
// 采用 deadline 制而非时长制：toolschema 的轮次屏障能天然吸收等待
// （下一轮开始时 deadline 多已过期，零空转），槽内顺序执行则等同 sleep。
func SleepUntil(ctx context.Context, deadline time.Time) error {
	d := time.Until(deadline)
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// FmtDelay 等待时长的用户可读形式：整秒显示（向上取整），亚秒显示毫秒。
func FmtDelay(d time.Duration) string {
	if d < time.Second {
		return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
	}
	secs := (d + time.Second - 1) / time.Second
	return strconv.FormatInt(int64(secs), 10) + "s"
}
