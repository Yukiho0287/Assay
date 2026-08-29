package probe

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// DecodeUseNumber 解码 JSON，数字保留为 json.Number（回写时按原文字面量输出，无精度损失）。
func DecodeUseNumber(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	// 拒绝尾随内容（"1} junk" 这类应视为解析失败，与 Python json.loads 一致）
	if dec.More() {
		return nil, fmt.Errorf("JSON 后有多余内容")
	}
	return v, nil
}

// MarshalNoEscape 序列化且不做 HTML 转义（< > & 原样输出，保字节保真），末尾不带换行。
func MarshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
