package tokenaccounting

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Yukiho0287/assay/server/internal/probe"
)

// usageData 一次响应的 usage 三元组 + 缓存计数。
type usageData struct {
	Prompt     int64
	Completion int64
	Total      int64
	Cached     int64  // -1 = 渠道未返回缓存计数；仅记录不断言（缓存计量属第三梯队）
	Raw        string // usage 对象原始 JSON（json.Number 回写，数字字面量无损）
}

// parseUsage 校验 usage 对象含完整三元组整数。
// 返回 violation 非空 = 结构不合规（200 但计量数据缺损，是被测行为）。
func parseUsage(v any) (usageData, string) {
	m, ok := v.(map[string]any)
	if !ok {
		return usageData{}, "usage 缺失或不是对象"
	}
	raw, err := probe.MarshalNoEscape(m)
	if err != nil {
		raw = nil
	}
	u := usageData{Cached: -1, Raw: string(raw)}
	for _, f := range []struct {
		name string
		dst  *int64
	}{
		{"prompt_tokens", &u.Prompt},
		{"completion_tokens", &u.Completion},
		{"total_tokens", &u.Total},
	} {
		n, ok := intField(m, f.name)
		if !ok {
			return u, "usage." + f.name + " 缺失或不是整数; usage=" + probe.TruncateRunes(u.Raw, 200)
		}
		*f.dst = n
	}
	// cached：OpenAI 官方在 prompt_tokens_details.cached_tokens，部分渠道放顶层 cached_tokens
	if d, ok := m["prompt_tokens_details"].(map[string]any); ok {
		if n, ok := intField(d, "cached_tokens"); ok {
			u.Cached = n
		}
	}
	if u.Cached < 0 {
		if n, ok := intField(m, "cached_tokens"); ok {
			u.Cached = n
		}
	}
	return u, ""
}

func intField(m map[string]any, key string) (int64, bool) {
	n, ok := m[key].(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	if err != nil {
		return 0, false
	}
	return i, true
}

// extractNonStreamUsage 从非流式响应提取 usage。
// 返回 (usage, violation, err)：err 非空 = 传输/解析层失败（判 rejected）。
func extractNonStreamUsage(body io.Reader) (usageData, string, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxResponseBytes))
	if err != nil {
		return usageData{}, "", err
	}
	doc, err := probe.DecodeUseNumber(data)
	if err != nil {
		return usageData{}, "", fmt.Errorf("响应体不是 JSON: %w", err)
	}
	m, _ := doc.(map[string]any)
	u, violation := parseUsage(m["usage"])
	return u, violation, nil
}

// extractStreamUsage 读 SSE 流取最后一个非空 usage（OpenAI 规范放终帧，部分渠道逐帧带）。
func extractStreamUsage(body io.Reader) (usageData, string, error) {
	var last any
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
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
			// 分片坏损属传输层问题，判 rejected（与 toolschema 口径一致）
			return usageData{}, "", fmt.Errorf("流式分片不是 JSON: %w", err)
		}
		if m, ok := chunk.(map[string]any); ok {
			if uv, ok := m["usage"].(map[string]any); ok {
				last = uv
			}
		}
	}
	if err := sc.Err(); err != nil {
		return usageData{}, "", err
	}
	if last == nil {
		return usageData{}, "流式响应未包含 usage（stream_options.include_usage 未生效）", nil
	}
	u, violation := parseUsage(last)
	return u, violation, nil
}
