package stability

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Yukiho0287/assay/server/internal/probe/protocol"
)

// outcome 一次压测请求的原始观测（未落 Sample 的中间态）。
type outcome struct {
	Ok         bool
	HTTPStatus int
	ErrorClass string
	Error      string

	TTFB    time.Duration
	TTFT    time.Duration
	HasTTFT bool
	Total   time.Duration

	Usage  protocol.Usage
	Header http.Header // 所有响应都留存，供 RPM probe 读限速头（2xx 也带余量头）
}

// timingReader 包裹响应体，在首个非空 Read 打 TTFB 点，其余透传。
type timingReader struct {
	r     io.Reader
	start time.Time
	ttfb  time.Duration
	got   bool
}

func (t *timingReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if !t.got && n > 0 {
		t.ttfb = time.Since(t.start)
		t.got = true
	}
	return n, err
}

// doRequest 发一次最小压测请求并观测时序 + usage。
// TTFB 由 timingReader 打点、TTFT 由 codec 的 onFirstToken 打点、total 为整流耗时。
func doRequest(ctx context.Context, client *http.Client, codec protocol.Codec, baseURL, apiKey, model, content string, maxTokens, timeoutMs int) outcome {
	var o outcome
	body, err := codec.LoadBody(model, content, maxTokens)
	if err != nil {
		o.ErrorClass = ErrTransport
		o.Error = "构造请求失败: " + err.Error()
		return o
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+codec.Path(), bytes.NewReader(body))
	if err != nil {
		o.ErrorClass = ErrTransport
		o.Error = "构造请求失败: " + err.Error()
		return o
	}
	req.Header.Set("Content-Type", "application/json")
	codec.Auth(req, apiKey)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		o.ErrorClass = ErrTransport
		if errors.Is(err, context.DeadlineExceeded) {
			o.Error = "超时（" + strconv.Itoa(timeoutMs) + "ms 无响应）"
		} else {
			o.Error = "连接失败: " + truncateOneLine(err.Error())
		}
		return o
	}
	defer resp.Body.Close()

	o.HTTPStatus = resp.StatusCode
	o.Header = resp.Header // 限速头在成功响应上也可能带余量（x-ratelimit-remaining-*）
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		o.ErrorClass = classifyHTTP(resp.StatusCode)
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(snippet))
		if msg == "" {
			msg = resp.Status
		}
		o.Error = "HTTP " + strconv.Itoa(resp.StatusCode) + ": " + truncateOneLine(msg)
		return o
	}

	tr := &timingReader{r: resp.Body, start: start}
	usage, err := codec.ScanStream(tr, func() {
		o.TTFT = time.Since(start)
		o.HasTTFT = true
	})
	o.Total = time.Since(start)
	if tr.got {
		o.TTFB = tr.ttfb
	}
	if err != nil {
		o.ErrorClass = ErrStreamAnomaly
		o.Error = "读取响应流失败: " + truncateOneLine(err.Error())
		return o
	}
	o.Usage = usage
	// semantic_empty：200 但既无内容 delta 又无 completion token，等于空响应
	if !o.HasTTFT && (!usage.Ok || usage.Completion == 0) {
		o.ErrorClass = ErrSemanticEmpty
		o.Error = "HTTP 200 但响应无内容"
		return o
	}
	o.Ok = true
	return o
}

func classifyHTTP(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return ErrRateLimited
	case status >= 500:
		return ErrHTTP5xx
	case status >= 400:
		return ErrHTTP4xx
	default:
		return ErrHTTP4xx // 3xx 等非 2xx 归 4xx 类（罕见，不单开分类）
	}
}

// sampleFrom outcome → Sample：ok 才记延迟/计量，否则一律 NULL（<0/空）。
func sampleFrom(stage string, stageIndex, seq int, warmup bool, dispatchedAt time.Time, proto string, o outcome) Sample {
	s := Sample{
		Stage:        stage,
		StageIndex:   stageIndex,
		Seq:          seq,
		Protocol:     proto,
		DispatchedAt: dispatchedAt,
		Warmup:       warmup,
		Ok:           o.Ok,
		HTTPStatus:   o.HTTPStatus,
		TTFBms:       -1,
		TTFTms:       -1,
		TotalMs:      -1,
		InputTokens:  -1,
		OutputTokens: -1,
	}
	if o.Ok {
		s.TTFBms = int(o.TTFB.Milliseconds())
		if o.HasTTFT {
			s.TTFTms = int(o.TTFT.Milliseconds())
		}
		s.TotalMs = int(o.Total.Milliseconds())
		if o.Usage.Ok {
			s.InputTokens = int(o.Usage.Prompt)
			s.OutputTokens = int(o.Usage.Completion)
		}
	} else {
		s.ErrorClass = o.ErrorClass
		s.Error = o.Error
	}
	return s
}

func truncateOneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}
