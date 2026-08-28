package toolschema

import (
	"fmt"
	"testing"
)

// 金标测试：选例结果必须与 KVV 官方 schema-report.json（commit 3dad65a）逐项相等。
// 这是移植正确性的实现闸门——任何算法偏差都会在这里以计数差异暴露。

func TestSelectionGolden(t *testing.T) {
	cases := SelectedCases()

	if got := len(cases); got != 204 {
		t.Fatalf("入选用例数 = %d, 官方金标 = 204", got)
	}

	gotReasons := map[string]int{}
	for _, c := range cases {
		gotReasons[c.Reason]++
	}
	wantReasons := map[string]int{
		"object_parameter_schema":  122,
		"string_parameter_schema":  18,
		"array_parameter_schema":   15,
		"anyOf_parameter_schema":   12,
		"number_parameter_schema":  12,
		"union_parameter_schema":   10,
		"integer_parameter_schema": 4,
		"boolean_parameter_schema": 4,
		"null_parameter_schema":    3,
		"empty_parameter_schema":   2,
		"ref_parameter_schema":     1,
		"explicit_tool_keyword":    1,
	}
	for reason, want := range wantReasons {
		if gotReasons[reason] != want {
			t.Errorf("selection_reason %q = %d, 金标 = %d", reason, gotReasons[reason], want)
		}
	}
	for reason, got := range gotReasons {
		if _, known := wantReasons[reason]; !known {
			t.Errorf("出现金标之外的 selection_reason %q（%d 个）", reason, got)
		}
	}
}

func TestSelectionSkips(t *testing.T) {
	// 官方固定跳过的 8 行（212 - 8 = 204）
	skipped := map[string][]int{
		"TestEnforcerCases":    {3, 4, 24, 25, 27, 28},
		"TestRangeConstraints": {3},
		"TestReferences":       {7},
	}
	selected := map[string]bool{}
	for _, c := range SelectedCases() {
		selected[fmt.Sprintf("%s:%d", c.Suite, c.Line)] = true
	}
	for suite, lines := range skipped {
		for _, line := range lines {
			if selected[fmt.Sprintf("%s:%d", suite, line)] {
				t.Errorf("%s 第 %d 行应被跳过却入选了", suite, line)
			}
		}
	}
	// 抽查跳过行的邻行确实入选（证明不是整套件误跳）
	for _, id := range []string{"TestEnforcerCases:2", "TestEnforcerCases:5", "TestRangeConstraints:2", "TestReferences:6"} {
		if !selected[id] {
			t.Errorf("%s 应入选却缺失", id)
		}
	}
}

func TestWrappedSchemasCompile(t *testing.T) {
	// 全部包装后 schema 必须能被 jsonschema/v6 以 Draft 2020-12 编译
	//（正则若含 RE2 不支持的构造会在此暴露）
	for _, c := range SelectedCases() {
		if _, err := compileSchema(c.Schema); err != nil {
			t.Errorf("%s:%d 编译失败: %v", c.Suite, c.Line, err)
		}
	}
}

func TestWrappedSchemaShape(t *testing.T) {
	// 语料中所有用例都是 dict schema → 包装后必为 {"type":"object", required:["value"], properties.value, additionalProperties:false}
	for _, c := range SelectedCases() {
		m, ok := c.Schema.(map[string]any)
		if !ok {
			t.Fatalf("%s:%d 包装后不是 dict（语料出现非 dict 用例，需复核移植假设）", c.Suite, c.Line)
		}
		props, ok := m["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s:%d 包装后缺 properties", c.Suite, c.Line)
		}
		if _, ok := props["value"]; !ok {
			t.Errorf("%s:%d 包装后缺 properties.value", c.Suite, c.Line)
		}
		if ap, _ := m["additionalProperties"].(bool); ap {
			t.Errorf("%s:%d additionalProperties 应为 false", c.Suite, c.Line)
		}
	}
}
