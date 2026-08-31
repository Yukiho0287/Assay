package protocol

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLoadBodyGolden 锁死三协议请求体的字节级形态（byte-exact 铁律：payload 不得二次序列化）。
func TestLoadBodyGolden(t *testing.T) {
	cases := []struct {
		proto     string
		model     string
		content   string
		maxTokens int
		want      string
	}{
		{
			proto: ProtocolOpenAIChat, model: "gpt-4", content: "ping", maxTokens: 4,
			want: `{"model":"gpt-4","messages":[{"role":"user","content":"ping"}],"max_tokens":4,"stream":true,"stream_options":{"include_usage":true}}`,
		},
		{
			// max_output_tokens 低于下限 16 应被抬到 16
			proto: ProtocolOpenAIResponses, model: "gpt-4", content: "ping", maxTokens: 4,
			want: `{"model":"gpt-4","input":"ping","max_output_tokens":16,"stream":true}`,
		},
		{
			proto: ProtocolAnthropicMessages, model: "claude-3", content: "ping", maxTokens: 4,
			want: `{"model":"claude-3","max_tokens":4,"messages":[{"role":"user","content":"ping"}],"stream":true}`,
		},
		{
			// 特殊字符不做 HTML 转义（MarshalNoEscape 保字节保真）
			proto: ProtocolOpenAIChat, model: "m", content: "a<b>&c", maxTokens: 32,
			want: `{"model":"m","messages":[{"role":"user","content":"a<b>&c"}],"max_tokens":32,"stream":true,"stream_options":{"include_usage":true}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.proto+"/"+tc.model, func(t *testing.T) {
			c, ok := Get(tc.proto)
			if !ok {
				t.Fatalf("协议未注册: %s", tc.proto)
			}
			got, err := c.LoadBody(tc.model, tc.content, tc.maxTokens)
			if err != nil {
				t.Fatalf("LoadBody 出错: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("请求体不符\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// TestAuthHeaders 校验三协议认证头范式
func TestAuthHeaders(t *testing.T) {
	t.Run("openai bearer", func(t *testing.T) {
		req := httptest.NewRequest("POST", "http://x/v1/chat/completions", nil)
		mustGet(t, ProtocolOpenAIChat).Auth(req, "sk-123")
		if got := req.Header.Get("Authorization"); got != "Bearer sk-123" {
			t.Errorf("Authorization=%q", got)
		}
	})
	t.Run("anthropic x-api-key", func(t *testing.T) {
		req := httptest.NewRequest("POST", "http://x/v1/messages", nil)
		mustGet(t, ProtocolAnthropicMessages).Auth(req, "sk-ant")
		if got := req.Header.Get("x-api-key"); got != "sk-ant" {
			t.Errorf("x-api-key=%q", got)
		}
		if got := req.Header.Get("anthropic-version"); got != anthropicVersion {
			t.Errorf("anthropic-version=%q", got)
		}
		if req.Header.Get("Authorization") != "" {
			t.Error("anthropic 不应带 Authorization 头")
		}
	})
}

const chatStream = `data: {"choices":[{"delta":{"role":"assistant"}}]}

data: {"choices":[{"delta":{"content":""}}]}

data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`

const responsesCompleted = `data: {"type":"response.created","response":{"id":"resp_1"}}

data: {"type":"response.output_text.delta","delta":""}

data: {"type":"response.output_text.delta","delta":"Hi"}

data: {"type":"response.completed","response":{"usage":{"input_tokens":7,"output_tokens":3}}}
`

const responsesIncomplete = `data: {"type":"response.output_text.delta","delta":"Yo"}

data: {"type":"response.incomplete","response":{"usage":{"input_tokens":8,"output_tokens":16}}}
`

// 带真实 event: 行的 anthropic 六段式，兼验非 data: 行被跳过
const anthropicStream = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":12,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}

event: message_stop
data: {"type":"message_stop"}
`

// TestScanStream 校验流式 TTFT 首非空 delta 识别 + 尾帧 usage 提取（三协议 + responses 双终态）
func TestScanStream(t *testing.T) {
	cases := []struct {
		name           string
		proto          string
		stream         string
		wantFirstCalls int
		wantPrompt     int64
		wantCompletion int64
	}{
		{"chat include_usage", ProtocolOpenAIChat, chatStream, 1, 10, 5},
		{"responses completed", ProtocolOpenAIResponses, responsesCompleted, 1, 7, 3},
		{"responses incomplete", ProtocolOpenAIResponses, responsesIncomplete, 1, 8, 16},
		{"anthropic message_delta", ProtocolAnthropicMessages, anthropicStream, 1, 12, 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mustGet(t, tc.proto)
			calls := 0
			u, err := c.ScanStream(strings.NewReader(tc.stream), func() { calls++ })
			if err != nil {
				t.Fatalf("ScanStream 出错: %v", err)
			}
			if calls != tc.wantFirstCalls {
				t.Errorf("onFirstToken 调用 %d 次，期望 %d（空 delta 不得触发）", calls, tc.wantFirstCalls)
			}
			if !u.Ok {
				t.Fatal("usage.Ok 应为 true")
			}
			if u.Prompt != tc.wantPrompt || u.Completion != tc.wantCompletion {
				t.Errorf("usage = {%d,%d}，期望 {%d,%d}", u.Prompt, u.Completion, tc.wantPrompt, tc.wantCompletion)
			}
		})
	}
}

// TestScanStreamNoUsage 无 usage 尾帧时 Ok=false（缺失 ≠ 0）
func TestScanStreamNoUsage(t *testing.T) {
	const s = `data: {"choices":[{"delta":{"content":"hi"}}]}

data: [DONE]
`
	u, err := mustGet(t, ProtocolOpenAIChat).ScanStream(strings.NewReader(s), nil)
	if err != nil {
		t.Fatalf("出错: %v", err)
	}
	if u.Ok {
		t.Error("无 usage 帧时 Ok 应为 false")
	}
}

// TestScanStreamBadChunk 分片非 JSON 判传输层失败
func TestScanStreamBadChunk(t *testing.T) {
	const s = `data: {not json}
`
	_, err := mustGet(t, ProtocolOpenAIChat).ScanStream(strings.NewReader(s), nil)
	if err == nil {
		t.Error("坏损分片应返回 err")
	}
}

// TestGetFallback 空 ID 兜底 openai_chat
func TestGetFallback(t *testing.T) {
	c, ok := Get("")
	if !ok || c.ID() != ProtocolOpenAIChat {
		t.Errorf("空 ID 应兜底 openai_chat，得到 ok=%v id=%v", ok, c)
	}
	if _, ok := Get("nonexistent"); ok {
		t.Error("未知协议应返回 ok=false")
	}
}

func mustGet(t *testing.T, id string) Codec {
	t.Helper()
	c, ok := Get(id)
	if !ok {
		t.Fatalf("协议未注册: %s", id)
	}
	return c
}
