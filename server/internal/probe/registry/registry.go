// Package registry 检测项注册表：新增一类检测 = 在 all 里追加一项，不动框架。
package registry

import (
	"fmt"

	"github.com/Yukiho0287/assay/server/internal/probe"
	"github.com/Yukiho0287/assay/server/internal/probe/tokenaccounting"
	"github.com/Yukiho0287/assay/server/internal/probe/toolschema"
)

var all = []probe.Probe{
	tokenaccounting.New(),
	toolschema.New(),
}

// init fail-fast：检测项元数据缺陷在进程启动时暴露，绝不等到出报告才发现评分算不出来。
func init() {
	for _, p := range all {
		if len(p.Info.Checkpoints) == 0 {
			panic(fmt.Sprintf("检测项 %q 未声明评分检查点", p.Info.ID))
		}
		seen := map[string]bool{}
		for _, c := range p.Info.Checkpoints {
			if c.Weight <= 0 {
				panic(fmt.Sprintf("检测项 %q 检查点 %q 权重须为正数", p.Info.ID, c.ID))
			}
			if seen[c.ID] {
				panic(fmt.Sprintf("检测项 %q 检查点 ID %q 重复", p.Info.ID, c.ID))
			}
			seen[c.ID] = true
		}
	}
}

// All 返回全部检测项（注册序即展示序，便宜的排前面）。
func All() []probe.Probe {
	return all
}

// Get 按 ID 查检测项。
func Get(id string) (probe.Probe, bool) {
	for _, p := range all {
		if p.Info.ID == id {
			return p, true
		}
	}
	return probe.Probe{}, false
}
