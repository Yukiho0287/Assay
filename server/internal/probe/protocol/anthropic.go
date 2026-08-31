package protocol

import (
	"io"
	"net/http"

	"github.com/Yukiho0287/assay/server/internal/probe"
)

// ProtocolAnthropicMessages Anthropic messages 协议 ID
const ProtocolAnthropicMessages = "anthropic_messages"

// anthropicVersion Anthropic 必带的版本头（与 connectivity 一致）
const anthropicVersion = "2023-06-01"

type anthropic struct{}

func init() { register(anthropic{}) }

type anthropicBody struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
	Stream    bool      `json:"stream"`
}

func (anthropic) ID() string   { return ProtocolAnthropicMessages }
func (anthropic) Path() string { return "/messages" }

func (anthropic) Auth(req *http.Request, apiKey string) {
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

func (anthropic) LoadBody(model, content string, maxTokens int) ([]byte, error) {
	return probe.MarshalNoEscape(anthropicBody{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  []message{{Role: "user", Content: content}},
		Stream:    true,
	})
}

func (anthropic) ScanStream(r io.Reader, onFirstToken func()) (Usage, error) {
	var u Usage
	first := false
	err := scanSSE(r, func(m map[string]any) {
		typ, _ := strField(m, "type")
		switch typ {
		case "message_start":
			// 初始 usage：input_tokens 全量给出，output_tokens 起始（后续在 message_delta 累计）
			msg, ok := objField(m, "message")
			if !ok {
				return
			}
			uv, ok := objField(msg, "usage")
			if !ok {
				return
			}
			if p, ok := intField(uv, "input_tokens"); ok {
				u.Prompt = p
				u.Ok = true
			}
		case "content_block_delta":
			if first {
				return
			}
			delta, ok := objField(m, "delta")
			if !ok {
				return
			}
			// 仅 text_delta 是文本内容；input_json_delta（工具入参）不算首 token
			if dt, _ := strField(delta, "type"); dt != "text_delta" {
				return
			}
			if txt, ok := strField(delta, "text"); ok && txt != "" {
				first = true
				if onFirstToken != nil {
					onFirstToken()
				}
			}
		case "message_delta":
			// 终帧顶层 usage 带累计 output_tokens
			uv, ok := objField(m, "usage")
			if !ok {
				return
			}
			if c, ok := intField(uv, "output_tokens"); ok {
				u.Completion = c
				u.Ok = true
			}
		}
	})
	return u, err
}
