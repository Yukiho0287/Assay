package tokenaccounting

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// 金标锁死样本流：长度、纯 ASCII、前缀性质、sha256。
// 此测试失败 = 样本生成被改动，历史报告的 pt 数字将不可比——改样本必须是有意的版本变更。
func TestSampleTextGolden(t *testing.T) {
	full := sampleText(step * levels)
	if len(full) != step*levels {
		t.Fatalf("长度 = %d, want %d", len(full), step*levels)
	}
	for i := 0; i < len(full); i++ {
		if full[i] > 127 {
			t.Fatalf("位置 %d 出现非 ASCII 字节 0x%02x", i, full[i])
		}
	}
	// 前缀性质：短样本必须是长样本的前缀（边际率差分的前提）
	for i := 1; i < levels; i++ {
		if sampleText(step*i) != full[:step*i] {
			t.Fatalf("S[:%d] 不是全量样本的前缀", step*i)
		}
	}
	sum := sha256.Sum256([]byte(full))
	const want = "287aaab13cd20f95effeb12313b3f53040ed4e8be5a99ed010f1702c131c9c2a"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("sha256 = %s, want %s", got, want)
	}
}

// 槽位矩阵锁死：9 槽、键 (suite,line,mode) 唯一、长度档位正确。
func TestSlotDefs(t *testing.T) {
	defs := slotDefs()
	if len(defs) != 9 {
		t.Fatalf("槽位数 = %d, want 9", len(defs))
	}
	seen := map[string]bool{}
	for _, d := range defs {
		key := d.suite + "/" + string(rune('0'+d.line)) + "/" + d.mode
		if seen[key] {
			t.Fatalf("槽位键重复: %s", key)
		}
		seen[key] = true
	}
	if defs[0].chars != step || defs[3].chars != step*levels {
		t.Fatalf("marginal 档位错误: %d..%d", defs[0].chars, defs[3].chars)
	}
}
