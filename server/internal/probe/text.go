package probe

import "strings"

// TruncateMessage 用例消息统一口径（移植 KVV _truncate）：换行压成 \n 字面量，超 500 字符截断。
func TruncateMessage(s string) string {
	return TruncateRunes(strings.ReplaceAll(s, "\n", "\\n"), 500)
}

// TruncateRunes 按 rune 截断（多字节安全），尾部补省略号。
func TruncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit-3]) + "..."
}
