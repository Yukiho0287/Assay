// Package toolschema 实现「工具调用 JSON Schema 遵从」检测项：
// KVV（MoonshotAI/Kimi-Vendor-Verifier, MIT, commit 3dad65a）tool_call_json_schema 组的 Go 移植。
// 本文件移植 validator.py 的选例/包装算法；语料见 corpus/（byte-exact，勿改一字节）。
//
// 保真度说明（与 Python 原版的已知差异，均经金标测试验证不影响选例结果）：
//   - Go map 序列化按键名排序，Python 按插入序 —— 语义相同的 schema，仅键顺序不同；
//   - Go 拒绝 NaN/Infinity JSON 字面量（更严格）—— 语料已核验不含此类字面量；
//   - 全程 json.Number 解码，数字按原文字面量回写，无 float64 精度损失。
package toolschema

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed corpus/*/valid.jsonl
var corpusFS embed.FS

// toolKeywords 显式工具关键词（validator.py TOOL_KEYWORDS）：
// schema 序列化小写后含任一子串即判为 explicit_tool_keyword。
var toolKeywords = []string{"tool", "function", "tool_call", "function_call", "arguments", "parameters"}

// Case 一个入选用例。Schema 是包装 + 剥 default 后的最终形态，发送与本地校验共用同一对象。
type Case struct {
	Suite  string
	Line   int    // 套件文件内行号，1 起（Python enumerate(start=1)），与 KVV 官方报告对齐
	Reason string // selection_reason
	Schema any    // 语料中均为 map[string]any；非 dict 原样保留（Python 同行为，金标证实语料不触发）
}

var (
	selectedOnce  sync.Once
	selectedCases []Case
	selectedErr   error
)

// SelectedCases 返回全部入选用例（首次调用时从内嵌语料加载并分类，之后复用）。
// 语料内嵌编译期固定，加载失败属程序性错误，Fail-Fast panic 阻止启动。
func SelectedCases() []Case {
	selectedOnce.Do(func() {
		selectedCases, selectedErr = loadCases()
	})
	if selectedErr != nil {
		panic("toolschema 语料加载失败: " + selectedErr.Error())
	}
	return selectedCases
}

// loadCases 移植 validator.py load_cases：套件目录按名排序，逐行分类。
func loadCases() ([]Case, error) {
	entries, err := corpusFS.ReadDir("corpus")
	if err != nil {
		return nil, err
	}
	var cases []Case
	for _, e := range entries { // fs.ReadDir 保证按文件名排序，与 Python sorted() 一致
		if !e.IsDir() {
			continue
		}
		data, err := corpusFS.ReadFile("corpus/" + e.Name() + "/valid.jsonl")
		if err != nil {
			return nil, fmt.Errorf("套件 %s: %w", e.Name(), err)
		}
		for i, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw) // Python .strip()；空行仍占行号
			if line == "" {
				continue
			}
			c, ok := classifyCase(line)
			if !ok {
				continue
			}
			c.Suite = e.Name()
			c.Line = i + 1
			cases = append(cases, c)
		}
	}
	return cases, nil
}

// classifyCase 移植 validator.py classify_case：解析 → 跳过判定 → 选例理由 → 包装 → 剥 default。
// 返回 ok=false 表示该行跳过（unsupported_by_transport）。
func classifyCase(line string) (Case, bool) {
	schema, err := decodeUseNumber([]byte(line))
	if err != nil {
		return Case{}, false // 解析失败 → 跳过
	}

	if m, isDict := schema.(map[string]any); isDict {
		// 超长字符串约束（minLength > 1000）：生成成本过高，跳过
		if t, _ := m["type"].(string); t == "string" && numberValue(m["minLength"]) > 1000 {
			return Case{}, false
		}
		// 顶层 properties 键含前后空白/空串/两字符转义字面量（\n 等）：传输层不可靠，跳过
		if hasExoticPropertyKeys(m) {
			return Case{}, false
		}
		// 无终止条件的递归 $ref：无法生成有限实例，跳过
		if hasRecursiveRefWithoutTermination(m) {
			return Case{}, false
		}
	}

	reason := selectionReason(schema, line)
	wrapped := wrapSchemaAsParameterProperty(schema)
	wrapped = stripKeywordRecursive(wrapped, "default")
	return Case{Reason: reason, Schema: wrapped}, true
}

// selectionReason 判定顺序：explicit_tool_keyword → object_parameter_schema → schema_shape。
func selectionReason(schema any, rawLine string) string {
	// Python 用 json.dumps(schema).lower() 做子串匹配；这里用 Go 序列化（键序不同但
	// 关键词是键名/值子串匹配，与键顺序无关，结果一致——金标分布逐项相等为证）
	serialized, err := marshalNoEscape(schema)
	if err != nil {
		serialized = []byte(rawLine)
	}
	lowered := strings.ToLower(string(serialized))
	for _, kw := range toolKeywords {
		if strings.Contains(lowered, kw) {
			return "explicit_tool_keyword"
		}
	}
	if isObjectParameterSchema(schema) {
		return "object_parameter_schema"
	}
	return schemaShape(schema)
}

// isObjectParameterSchema：type 为 "object"（或 type 列表含 "object"）且 properties 是 dict。
func isObjectParameterSchema(schema any) bool {
	m, ok := schema.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := m["properties"].(map[string]any); !ok {
		return false
	}
	switch t := m["type"].(type) {
	case string:
		return t == "object"
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok && s == "object" {
				return true
			}
		}
	}
	return false
}

// schemaShape 移植 validator.py _schema_shape：按形态归类选例理由。
func schemaShape(schema any) string {
	m, ok := schema.(map[string]any)
	if !ok {
		// Python 返回 type(schema).__name__；语料不触发此分支（金标分布为证），命名仅求稳定
		return fmt.Sprintf("%T_parameter_schema", schema)
	}
	if len(m) == 0 {
		return "empty_parameter_schema"
	}
	switch t := m["type"].(type) {
	case []any:
		_ = t
		return "union_parameter_schema"
	case string:
		return t + "_parameter_schema"
	}
	for _, kw := range []string{"anyOf", "oneOf", "allOf"} {
		if _, ok := m[kw]; ok {
			return kw + "_parameter_schema"
		}
	}
	if _, ok := m["$ref"]; ok {
		return "ref_parameter_schema"
	}
	return "schema_parameter_schema"
}

// hasExoticPropertyKeys：仅检查顶层 properties 的键。
// 判据：键 != strip 后的键、空键、或含两字符转义字面量 \n \t \r \b \f（语料里是字面反斜杠+字母）。
func hasExoticPropertyKeys(m map[string]any) bool {
	props, ok := m["properties"].(map[string]any)
	if !ok {
		return false
	}
	for key := range props {
		if key == "" || key != strings.TrimSpace(key) {
			return true
		}
		for _, seq := range []string{`\n`, `\t`, `\r`, `\b`, `\f`} {
			if strings.Contains(key, seq) {
				return true
			}
		}
	}
	return false
}

// collectRefs 深度收集所有 $ref 字符串值（Python _collect_refs；非字符串 $ref 语料不存在，忽略）。
func collectRefs(obj any) []string {
	var refs []string
	switch v := obj.(type) {
	case map[string]any:
		if r, ok := v["$ref"].(string); ok {
			refs = append(refs, r)
		}
		for k, val := range v {
			if k == "$ref" {
				continue // 上面已计入；值是字符串无子结构
			}
			refs = append(refs, collectRefs(val)...)
		}
	case []any:
		for _, item := range v {
			refs = append(refs, collectRefs(item)...)
		}
	}
	return refs
}

// hasRecursiveRefWithoutTermination：#/$defs/X 自引用、无原始类型/枚举等终止出口、
// 且递归出现在 required 属性里 → 无法生成有限实例。
func hasRecursiveRefWithoutTermination(m map[string]any) bool {
	defs, _ := m["$defs"].(map[string]any)
	for _, ref := range collectRefs(m) {
		if !strings.HasPrefix(ref, "#/$defs/") {
			continue
		}
		parts := strings.Split(ref, "/")
		defSchema, ok := defs[parts[len(parts)-1]]
		if !ok {
			continue
		}
		selfRef := false
		for _, inner := range collectRefs(defSchema) {
			if inner == ref {
				selfRef = true
				break
			}
		}
		if !selfRef || hasNonRecursiveOption(defSchema) {
			continue
		}
		ds, ok := defSchema.(map[string]any)
		if !ok {
			continue
		}
		required := map[string]bool{}
		if reqList, ok := ds["required"].([]any); ok {
			for _, r := range reqList {
				if s, ok := r.(string); ok {
					required[s] = true
				}
			}
		}
		props, _ := ds["properties"].(map[string]any)
		for propName, propSchema := range props {
			if !required[propName] {
				continue
			}
			for _, inner := range collectRefs(propSchema) {
				if inner == ref {
					return true
				}
			}
		}
	}
	return false
}

// hasNonRecursiveOption 移植 _has_non_recursive_option。
// 注意 Python 语义：anyOf/oneOf 存在时立即 return any(...)，不再落到 enum/values 检查。
func hasNonRecursiveOption(obj any) bool {
	switch v := obj.(type) {
	case map[string]any:
		if isPrimitiveType(v["type"]) {
			return true
		}
		if a, ok := v["anyOf"]; ok {
			return anyNonRecursive(a)
		}
		if o, ok := v["oneOf"]; ok {
			return anyNonRecursive(o)
		}
		if _, ok := v["enum"]; ok {
			return true
		}
		for _, val := range v {
			if hasNonRecursiveOption(val) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if hasNonRecursiveOption(item) {
				return true
			}
		}
	}
	return false
}

func anyNonRecursive(v any) bool {
	list, ok := v.([]any)
	if !ok {
		return false
	}
	for _, item := range list {
		if hasNonRecursiveOption(item) {
			return true
		}
	}
	return false
}

func isPrimitiveType(t any) bool {
	switch v := t.(type) {
	case string:
		return v == "string" || v == "number" || v == "integer" || v == "boolean" || v == "null"
	case []any:
		for _, x := range v {
			if isPrimitiveType(x) {
				return true
			}
		}
	}
	return false
}

// wrapSchemaAsParameterProperty 把用例 schema 包装成工具参数 schema：
// {"type":"object","required":["value"],"additionalProperties":false,"properties":{"value":<原 schema>}}。
// 原 schema 里对根的 "#" 引用改写为 #/$defs/__case_schema（否则包装后 "#" 指向包装层，语义就错了）。
func wrapSchemaAsParameterProperty(schema any) any {
	m, isDict := schema.(map[string]any)
	if !isDict {
		return schema // 非 dict 原样返回（Python 同行为）
	}
	wrapped := map[string]any{
		"type":                 "object",
		"required":             []any{"value"},
		"additionalProperties": false,
	}

	rootRef := false
	for _, ref := range collectRefs(m) {
		if ref == "#" {
			rootRef = true
			break
		}
	}
	if rootRef {
		defs := map[string]any{}
		if d, ok := m["$defs"].(map[string]any); ok {
			for k, v := range d {
				defs[k] = v
			}
		}
		defName := "__case_schema"
		for {
			if _, exists := defs[defName]; !exists {
				break
			}
			defName = "_" + defName
		}
		target := "#/$defs/" + defName
		rewritten := rewriteRootRefs(m, target).(map[string]any)
		if rd, ok := rewritten["$defs"].(map[string]any); ok {
			for k, v := range rd {
				defs[k] = v
			}
		}
		inner := map[string]any{}
		for k, v := range rewritten {
			if k != "$defs" && k != "$id" {
				inner[k] = v
			}
		}
		defs[defName] = inner
		wrapped["properties"] = map[string]any{"value": map[string]any{"$ref": target}}
		wrapped["$defs"] = defs
	} else {
		inner := map[string]any{}
		for k, v := range m {
			if k != "$defs" && k != "$id" {
				inner[k] = v
			}
		}
		wrapped["properties"] = map[string]any{"value": inner}
		if d, ok := m["$defs"]; ok {
			wrapped["$defs"] = d
		}
	}
	if id, ok := m["$id"]; ok {
		wrapped["$id"] = id
	}
	return wrapped
}

// rewriteRootRefs 只把值恰好为 "#" 的 $ref 替换成 target，其余原样递归复制。
func rewriteRootRefs(obj any, target string) any {
	switch v := obj.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			if k == "$ref" && val == "#" {
				out[k] = target
			} else {
				out[k] = rewriteRootRefs(val, target)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = rewriteRootRefs(item, target)
		}
		return out
	default:
		return obj
	}
}

// stripKeywordRecursive 递归剥除指定键（KVV 用于剥 "default"，含 properties 里同名属性键——原版行为如此）。
func stripKeywordRecursive(obj any, keyword string) any {
	switch v := obj.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			if k == keyword {
				continue
			}
			out[k] = stripKeywordRecursive(val, keyword)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = stripKeywordRecursive(item, keyword)
		}
		return out
	default:
		return obj
	}
}

// numberValue 宽容取数值（json.Number → float64，其余 0）。
func numberValue(v any) float64 {
	if n, ok := v.(json.Number); ok {
		if f, err := n.Float64(); err == nil {
			return f
		}
	}
	return 0
}

// decodeUseNumber 解码 JSON，数字保留为 json.Number（回写时按原文字面量输出，无精度损失）。
func decodeUseNumber(data []byte) (any, error) {
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

// marshalNoEscape 序列化且不做 HTML 转义（< > & 原样输出，保字节保真），末尾不带换行。
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// compileSchema 用 Draft 2020-12 编译包装后 schema（语料已核验无 $schema 键，默认草案即 2020-12，
// 与 Python jsonschema Draft202012Validator 对齐；format 断言两边默认都关）。
// 每用例独立 Compiler，避免不同用例的 $id 在同一注册表里冲突。
//
// 宽松降级：语料 TestID 有 3 例根 $id 带 fragment（如 "#user"），2020-12 元校验拒绝，
// 但 Python jsonschema 不主动 check_schema、照常校验。对齐方式：编译失败且根有 $id 时
// 去掉根 $id 重试——仅影响本地校验器，发送给渠道的 schema 保持原样（这 3 例无内部 $ref，
// $id 对校验语义零贡献，金标编译测试为此背书）。
func compileSchema(schema any) (*jsonschema.Schema, error) {
	sch, err := compileOnce(schema)
	if err == nil {
		return sch, nil
	}
	m, ok := schema.(map[string]any)
	if !ok {
		return nil, err
	}
	if _, hasID := m["$id"]; !hasID {
		return nil, err
	}
	stripped := make(map[string]any, len(m))
	for k, v := range m {
		if k != "$id" {
			stripped[k] = v
		}
	}
	return compileOnce(stripped)
}

func compileOnce(schema any) (*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	if err := c.AddResource("http://assay.local/case.json", schema); err != nil {
		return nil, err
	}
	return c.Compile("http://assay.local/case.json")
}
