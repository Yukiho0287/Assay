package protocol

import (
	"io"
	"net/http"

	"github.com/Yukiho0287/assay/server/internal/probe"
)

// ProtocolOpenAIChat OpenAI chat/completions 协议 ID
const ProtocolOpenAIChat = "openai_chat"

type openaiChat struct{}

func init() { register(openaiChat{}) }

// chatStreamOptions 显式要求流式尾帧带 usage（否则 chat 流不返回计量）
type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatBody struct {
	Model         string            `json:"model"`
	Messages      []message         `json:"messages"`
	MaxTokens     int               `json:"max_tokens"`
	Stream        bool              `json:"stream"`
	StreamOptions chatStreamOptions `json:"stream_options"`
}

func (openaiChat) ID() string   { return ProtocolOpenAIChat }
func (openaiChat) Path() string { return "/chat/completions" }

func (openaiChat) Auth(req *http.Request, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
}

func (openaiChat) LoadBody(model, content string, maxTokens int) ([]byte, error) {
	return probe.MarshalNoEscape(chatBody{
		Model:         model,
		Messages:      []message{{Role: "user", Content: content}},
		MaxTokens:     maxTokens,
		Stream:        true,
		StreamOptions: chatStreamOptions{IncludeUsage: true},
	})
}

func (openaiChat) ScanStream(r io.Reader, onFirstToken func()) (Usage, error) {
	var u Usage
	first := false
	err := scanSSE(r, func(m map[string]any) {
		// 尾帧 usage：include_usage 生效时末帧 choices 为空、usage 非空
		if uv, ok := objField(m, "usage"); ok {
			if p, ok := intField(uv, "prompt_tokens"); ok {
				u.Prompt = p
				u.Ok = true
			}
			if c, ok := intField(uv, "completion_tokens"); ok {
				u.Completion = c
				u.Ok = true
			}
		}
		if first {
			return
		}
		// 首个非空 delta.content → TTFT
		choices, ok := m["choices"].([]any)
		if !ok || len(choices) == 0 {
			return
		}
		c0, ok := choices[0].(map[string]any)
		if !ok {
			return
		}
		delta, ok := objField(c0, "delta")
		if !ok {
			return
		}
		if txt, ok := strField(delta, "content"); ok && txt != "" {
			first = true
			if onFirstToken != nil {
				onFirstToken()
			}
		}
	})
	return u, err
}
