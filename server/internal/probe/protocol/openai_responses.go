package protocol

import (
	"io"
	"net/http"

	"github.com/Yukiho0287/assay/server/internal/probe"
)

// ProtocolOpenAIResponses OpenAI Responses 协议 ID
const ProtocolOpenAIResponses = "openai_responses"

// responsesMinMaxTokens Responses API 的 max_output_tokens 下限
const responsesMinMaxTokens = 16

type openaiResponses struct{}

func init() { register(openaiResponses{}) }

type responsesBody struct {
	Model           string `json:"model"`
	Input           string `json:"input"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	Stream          bool   `json:"stream"`
}

func (openaiResponses) ID() string   { return ProtocolOpenAIResponses }
func (openaiResponses) Path() string { return "/responses" }

func (openaiResponses) Auth(req *http.Request, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
}

func (openaiResponses) LoadBody(model, content string, maxTokens int) ([]byte, error) {
	if maxTokens < responsesMinMaxTokens {
		maxTokens = responsesMinMaxTokens // 低于下限上游直接 400
	}
	return probe.MarshalNoEscape(responsesBody{
		Model:           model,
		Input:           content,
		MaxOutputTokens: maxTokens,
		Stream:          true,
	})
}

func (openaiResponses) ScanStream(r io.Reader, onFirstToken func()) (Usage, error) {
	var u Usage
	first := false
	err := scanSSE(r, func(m map[string]any) {
		typ, _ := strField(m, "type")
		switch typ {
		case "response.output_text.delta":
			if first {
				return
			}
			if txt, ok := strField(m, "delta"); ok && txt != "" {
				first = true
				if onFirstToken != nil {
					onFirstToken()
				}
			}
		case "response.completed", "response.incomplete":
			// max_output_tokens 打满时终态常是 incomplete 而非 completed，usage 同样带；
			// 两者都必须接受，否则小 max_tokens 压测大面积漏 usage。
			resp, ok := objField(m, "response")
			if !ok {
				return
			}
			uv, ok := objField(resp, "usage")
			if !ok {
				return
			}
			if p, ok := intField(uv, "input_tokens"); ok {
				u.Prompt = p
				u.Ok = true
			}
			if c, ok := intField(uv, "output_tokens"); ok {
				u.Completion = c
				u.Ok = true
			}
		}
	})
	return u, err
}
