// Package registry 检测项注册表：新增一类检测 = 在 all 里追加一项，不动框架。
package registry

import (
	"github.com/Yukiho0287/assay/server/internal/probe"
	"github.com/Yukiho0287/assay/server/internal/probe/tokenaccounting"
	"github.com/Yukiho0287/assay/server/internal/probe/toolschema"
)

var all = []probe.Probe{
	tokenaccounting.New(),
	toolschema.New(),
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
