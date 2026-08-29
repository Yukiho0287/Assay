// Package tokenaccounting 实现「Token 计量检定」检测项：
// 测渠道返回的 usage 计数是否数学自洽（恒等式 / 纯 ASCII 上限 / 确定性 / 恒定边际率），
// 不与任何官方值对比，零对照、近零成本。算法移植自 KVV 验证专项的 marginal_rate.py
// （知识库 §5 第一层自洽性 + §3.2 分词器三铁律）。
package tokenaccounting

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	// step 边际率实验的字符步长；levels 层数（4000/8000/12000/16000 字符）。
	// 与 marginal_rate.py 常量一致：步长需远大于聊天模板开销，差分才是纯信号。
	step   = 4000
	levels = 4
	// sampleSeed 样本流种子。与 KVV 脚本（"kvv-marginal-%d"）不同源，
	// 保证本平台样本独立可复现，单测有 sha256 金标锁死。
	sampleSeed = "assay-marginal-%d"
)

// sampleText 确定性纯 ASCII 样本流：base64(sha256(seed-i)) 逐块拼接后截断到 n 字符。
// base64 字母表是 ASCII 子集，天然满足「纯 ASCII 上限」断言的前提。
func sampleText(n int) string {
	var b strings.Builder
	b.Grow(n + 44)
	for i := 0; b.Len() < n; i++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf(sampleSeed, i)))
		b.WriteString(base64.StdEncoding.EncodeToString(sum[:]))
	}
	return b.String()[:n]
}

// init 断言样本流纯 ASCII（Fail-Fast：前提破坏则整套断言口径失效，阻止启动）。
func init() {
	for _, c := range sampleText(step * levels) {
		if c > 127 {
			panic("tokenaccounting 样本流含非 ASCII 字符")
		}
	}
}
