package httpserver

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Yukiho0287/assay/server/internal/db"
)

// row 简写构造：只有评分关心的字段。
func row(probeID, suite string, line int, mode, status string) db.ListTaskCaseResultsRow {
	return db.ListTaskCaseResultsRow{Probe: probeID, Suite: suite, Line: int32(line), Mode: mode, Status: status}
}

// TestBuildReportTokenAccounting 锁死检查点匹配与加权数学：
// marginal 4/4 过（100 分 ×权2）、determinism 2/3 过（66.7 分 ×权2）、stream 1/2 过（50 分 ×权1），
// probe 分 = (100×2 + 66.7×2 + 50×1) / 5 = 76.68 → 76.7，单 probe 即总分，grade C。
func TestBuildReportTokenAccounting(t *testing.T) {
	task := db.GetTaskRow{ID: uuid.New(), Status: "succeeded", Probes: []string{"token_accounting"}}
	rows := []db.ListTaskCaseResultsRow{
		row("token_accounting", "marginal", 1, "non_stream", "passed"),
		row("token_accounting", "marginal", 2, "non_stream", "passed"),
		row("token_accounting", "marginal", 3, "non_stream", "passed"),
		row("token_accounting", "marginal", 4, "non_stream", "passed"),
		row("token_accounting", "determinism", 1, "non_stream", "passed"),
		row("token_accounting", "determinism", 2, "non_stream", "violated"),
		row("token_accounting", "determinism", 3, "non_stream", "passed"),
		row("token_accounting", "stream_consistency", 1, "non_stream", "passed"),
		row("token_accounting", "stream_consistency", 1, "stream", "rejected"),
	}
	r := buildReport(task, rows)

	if len(r.Probes) != 1 || len(r.Probes[0].Checkpoints) != 3 {
		t.Fatalf("期望 1 probe × 3 检查点，得到 %+v", r.Probes)
	}
	cps := r.Probes[0].Checkpoints
	if *cps[0].Score != 100 || cps[0].Total != 4 {
		t.Errorf("marginal 应 100 分/4 例，得 %v/%d", *cps[0].Score, cps[0].Total)
	}
	if *cps[1].Score != 66.7 {
		t.Errorf("determinism 应 66.7 分，得 %v", *cps[1].Score)
	}
	if *cps[2].Score != 50 || cps[2].Rejected != 1 {
		t.Errorf("stream 应 50 分（rejected 计失败），得 %v", *cps[2].Score)
	}
	if *r.Probes[0].Score != 76.7 || *r.Score != 76.7 {
		t.Errorf("加权分应 76.7，得 probe=%v total=%v", *r.Probes[0].Score, *r.Score)
	}
	if string(*r.Grade) != "C" {
		t.Errorf("76.7 分应为 C 级，得 %v", *r.Grade)
	}
	if r.Incomplete != nil {
		t.Errorf("succeeded 任务不应标 incomplete")
	}
}

// TestBuildReportCollectedExcluded collected 是未评估中间态而非结论，
// 不参与计数；任务非 succeeded 须标 incomplete。
func TestBuildReportCollectedExcluded(t *testing.T) {
	task := db.GetTaskRow{ID: uuid.New(), Status: "canceled", Probes: []string{"token_accounting"}}
	rows := []db.ListTaskCaseResultsRow{
		row("token_accounting", "marginal", 1, "non_stream", "passed"),
		row("token_accounting", "marginal", 2, "non_stream", "collected"),
		row("token_accounting", "marginal", 3, "non_stream", "collected"),
	}
	r := buildReport(task, rows)

	cp := r.Probes[0].Checkpoints[0]
	if cp.Total != 1 || *cp.Score != 100 {
		t.Errorf("collected 不应计入：期望 total=1 score=100，得 total=%d score=%v", cp.Total, cp.Score)
	}
	// determinism/stream 零采样：score 缺省、不参与加权，probe 分只由 marginal 决定
	if r.Probes[0].Checkpoints[1].Score != nil {
		t.Errorf("未采样检查点 score 应缺省")
	}
	if *r.Probes[0].Score != 100 {
		t.Errorf("probe 分应 100（只有 marginal 采样），得 %v", *r.Probes[0].Score)
	}
	if r.Incomplete == nil || !*r.Incomplete {
		t.Errorf("canceled 任务应标 incomplete")
	}
}

// TestBuildReportMultiProbe 总分 = 各检测项等权平均；未知检测项（已下架）不计分不崩溃。
func TestBuildReportMultiProbe(t *testing.T) {
	task := db.GetTaskRow{ID: uuid.New(), Status: "succeeded", Probes: []string{"token_accounting", "tool_call_json_schema", "gone_probe"}}
	rows := []db.ListTaskCaseResultsRow{
		// token_accounting 全过 → 100
		row("token_accounting", "marginal", 1, "non_stream", "passed"),
		row("token_accounting", "determinism", 1, "non_stream", "passed"),
		row("token_accounting", "stream_consistency", 1, "stream", "passed"),
		// toolschema：非流式 1/1 过、流式 0/1 过 → (100+0)/2 = 50
		row("tool_call_json_schema", "TestBasicTypes", 1, "non_stream", "passed"),
		row("tool_call_json_schema", "TestBasicTypes", 1, "stream", "violated"),
	}
	r := buildReport(task, rows)

	if len(r.Probes) != 3 {
		t.Fatalf("期望 3 个 probe 行，得 %d", len(r.Probes))
	}
	if *r.Probes[0].Score != 100 || *r.Probes[1].Score != 50 {
		t.Errorf("probe 分应 100/50，得 %v/%v", *r.Probes[0].Score, *r.Probes[1].Score)
	}
	if r.Probes[2].Score != nil || r.Probes[2].ProbeName != "gone_probe" {
		t.Errorf("下架 probe 应无分且名回退为 id，得 %+v", r.Probes[2])
	}
	if *r.Score != 75 {
		t.Errorf("总分应 (100+50)/2=75，得 %v", *r.Score)
	}
	if string(*r.Grade) != "C" {
		t.Errorf("75 分应为 C，得 %v", *r.Grade)
	}
}
