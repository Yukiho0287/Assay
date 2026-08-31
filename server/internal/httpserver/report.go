package httpserver

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/Yukiho0287/assay/server/internal/api"
	"github.com/Yukiho0287/assay/server/internal/db"
	"github.com/Yukiho0287/assay/server/internal/probe"
	"github.com/Yukiho0287/assay/server/internal/probe/registry"
	"github.com/Yukiho0287/assay/server/internal/version"
)

// isTerminalTask 任务是否已结束（评分与导出仅对终态任务开放：运行中评分会随采集漂移，
// 给出一个之后会变的"结论"违背证据链）。
func isTerminalTask(status string) bool {
	return status == "succeeded" || status == "failed" || status == "canceled"
}

func (h *handlers) GetQualityTaskReport(w http.ResponseWriter, r *http.Request, id api.IdPath) {
	if _, ok := h.requirePerm(w, r, "quality"); !ok {
		return
	}
	task, rows, ok := h.loadTerminalTaskWithResults(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, buildReport(task, rows))
}

func (h *handlers) ExportQualityTask(w http.ResponseWriter, r *http.Request, id api.IdPath, params api.ExportQualityTaskParams) {
	if _, ok := h.requirePerm(w, r, "quality"); !ok {
		return
	}
	task, rows, ok := h.loadTerminalTaskWithResults(w, r, id)
	if !ok {
		return
	}
	report := buildReport(task, rows)
	// 文件名带任务 id 前 8 位，多任务导出不互相覆盖
	stem := fmt.Sprintf("assay-report-%s", task.ID.String()[:8])

	switch params.Format {
	case api.Junit:
		out, err := buildJUnit(task, rows, report)
		if err != nil {
			h.internalError(w, "渲染 JUnit 报告失败", err)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", stem+".xml"))
		w.Write([]byte(xml.Header))
		w.Write(out)
	case api.Json:
		apiTask, err := taskToAPI(task)
		if err != nil {
			h.internalError(w, "任务数据损坏", err)
			return
		}
		// 导出自带聚合统计，脱离平台可独立审计
		if agg, err := h.q.AggregateTaskCaseResults(r.Context(), id); err == nil {
			apiTask.Stats = buildStats(agg)
		}
		results := make([]api.QualityCaseResult, 0, len(rows))
		for _, row := range rows {
			results = append(results, caseResultToAPI(row))
		}
		export := api.QualityExport{
			Tool:    "assay",
			Version: version.Version,
			Task:    apiTask,
			Report:  report,
			Results: results,
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", stem+".json"))
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(export); err != nil {
			h.log.Error("写出 JSON 导出失败", "err", err)
		}
	default:
		writeJSON(w, http.StatusBadRequest, api.Error{Error: "未知导出格式"})
	}
}

// loadTerminalTaskWithResults 读任务 + 终态守卫 + 全量用例行，report/export 共用。
func (h *handlers) loadTerminalTaskWithResults(w http.ResponseWriter, r *http.Request, id api.IdPath) (db.GetTaskRow, []db.ListTaskCaseResultsRow, bool) {
	task, ok := h.loadQualityTask(w, r, id)
	if !ok {
		return db.GetTaskRow{}, nil, false
	}
	if !isTerminalTask(task.Status) {
		writeJSON(w, http.StatusConflict, api.Error{Error: "任务尚未结束，暂无评分报告"})
		return db.GetTaskRow{}, nil, false
	}
	rows, err := h.q.ListTaskCaseResults(r.Context(), db.ListTaskCaseResultsParams{TaskID: id})
	if err != nil {
		h.internalError(w, "读取用例结果失败", err)
		return db.GetTaskRow{}, nil, false
	}
	return task, rows, true
}

// buildReport 从用例行即时计算评分板：检查点得分 = 命中用例 passed 占比，
// 检测项得分 = 检查点按权重加权平均，总分 = 各检测项等权平均。
// collected 行（仅取消的任务可能残留）不是判定结论，不参与计数——导出的原始行里仍完整保留。
func buildReport(task db.GetTaskRow, rows []db.ListTaskCaseResultsRow) api.QualityReport {
	report := api.QualityReport{
		TaskId:      task.ID,
		Status:      api.TaskStatus(task.Status),
		Probes:      make([]api.ProbeScore, 0, len(task.Probes)),
		GeneratedAt: time.Now().UTC(),
	}
	if task.Status != "succeeded" {
		v := true
		report.Incomplete = &v
	}

	var probeScoreSum float64
	var probeScoreN int
	for _, probeID := range task.Probes {
		ps := api.ProbeScore{ProbeId: probeID, ProbeName: probeID}
		var cps []probe.Checkpoint
		if p, ok := registry.Get(probeID); ok {
			ps.ProbeName = p.Info.Name
			cps = p.Info.Checkpoints
		}
		ps.Checkpoints = make([]api.CheckpointScore, 0, len(cps))

		var weighted, weightSum float64
		for _, cp := range cps {
			cs := api.CheckpointScore{Id: cp.ID, Name: cp.Name, Weight: float32(cp.Weight)}
			for _, row := range rows {
				if row.Probe != probeID || !cp.Matches(row.Suite, row.Mode) {
					continue
				}
				switch row.Status {
				case probe.StatusPassed:
					cs.Passed++
				case probe.StatusRejected:
					cs.Rejected++
				case probe.StatusViolated:
					cs.Violated++
				default:
					continue // collected：未评估，不计入
				}
				cs.Total++
			}
			if cs.Total > 0 {
				score := round1(float64(cs.Passed) / float64(cs.Total) * 100)
				cs.Score = &score
				weighted += cp.Weight * float64(score)
				weightSum += cp.Weight
			}
			ps.Checkpoints = append(ps.Checkpoints, cs)
		}
		if weightSum > 0 {
			score := round1(weighted / weightSum)
			ps.Score = &score
			probeScoreSum += float64(score)
			probeScoreN++
		}
		report.Probes = append(report.Probes, ps)
	}

	if probeScoreN > 0 {
		overall := round1(probeScoreSum / float64(probeScoreN))
		report.Score = &overall
		grade := gradeOf(float64(overall))
		report.Grade = &grade
	}
	return report
}

func round1(v float64) float32 {
	return float32(math.Round(v*10) / 10)
}

func gradeOf(score float64) api.QualityReportGrade {
	switch {
	case score >= 95:
		return api.A
	case score >= 80:
		return api.B
	case score >= 60:
		return api.C
	default:
		return api.D
	}
}

// ——— JUnit XML 渲染（知识库落地形态要求：KVV 式评分板 + junit）———

type junitTestsuites struct {
	XMLName    xml.Name         `xml:"testsuites"`
	Name       string           `xml:"name,attr"`
	Tests      int              `xml:"tests,attr"`
	Failures   int              `xml:"failures,attr"`
	Errors     int              `xml:"errors,attr"`
	Skipped    int              `xml:"skipped,attr"`
	Time       string           `xml:"time,attr"`
	Properties []junitProperty  `xml:"properties>property"`
	Suites     []junitTestsuite `xml:"testsuite"`
}

type junitTestsuite struct {
	Name       string          `xml:"name,attr"`
	Tests      int             `xml:"tests,attr"`
	Failures   int             `xml:"failures,attr"`
	Errors     int             `xml:"errors,attr"`
	Skipped    int             `xml:"skipped,attr"`
	Time       string          `xml:"time,attr"`
	Properties []junitProperty `xml:"properties>property"`
	Cases      []junitTestcase `xml:"testcase"`
}

type junitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type junitTestcase struct {
	Classname string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitVerdict `xml:"failure,omitempty"`
	Error     *junitVerdict `xml:"error,omitempty"`
	Skipped   *junitVerdict `xml:"skipped,omitempty"`
}

type junitVerdict struct {
	Message string `xml:"message,attr,omitempty"`
	Type    string `xml:"type,attr,omitempty"`
	Body    string `xml:",chardata"`
}

// buildJUnit 一个检测项渲染为一个 testsuite；violated→failure、rejected→error、
// collected（仅取消任务残留）→skipped。latency 汇总进 time 属性（秒）。
func buildJUnit(task db.GetTaskRow, rows []db.ListTaskCaseResultsRow, report api.QualityReport) ([]byte, error) {
	var target probe.Target
	if err := json.Unmarshal(task.Target, &target); err != nil {
		return nil, fmt.Errorf("解析任务快照: %w", err)
	}

	root := junitTestsuites{
		Name: "assay-quality",
		Properties: []junitProperty{
			{Name: "assay.taskId", Value: task.ID.String()},
			{Name: "assay.version", Value: version.Version},
			{Name: "assay.channel", Value: target.ChannelName},
			{Name: "assay.baseUrl", Value: target.BaseURL},
			{Name: "assay.model", Value: target.Model},
			{Name: "assay.taskStatus", Value: task.Status},
		},
	}
	if report.Score != nil {
		root.Properties = append(root.Properties,
			junitProperty{Name: "assay.score", Value: fmt.Sprintf("%.1f", *report.Score)},
			junitProperty{Name: "assay.grade", Value: string(*report.Grade)},
		)
	}

	var rootMs int
	for _, ps := range report.Probes {
		suite := junitTestsuite{Name: ps.ProbeId}
		if ps.Score != nil {
			suite.Properties = append(suite.Properties,
				junitProperty{Name: "assay.probeScore", Value: fmt.Sprintf("%.1f", *ps.Score)})
		}
		for _, cs := range ps.Checkpoints {
			if cs.Score != nil {
				suite.Properties = append(suite.Properties, junitProperty{
					Name:  fmt.Sprintf("assay.checkpoint.%s", cs.Id),
					Value: fmt.Sprintf("%.1f (weight=%g)", *cs.Score, cs.Weight),
				})
			}
		}

		var totalMs int
		for _, row := range rows {
			if row.Probe != ps.ProbeId {
				continue
			}
			tc := junitTestcase{
				Classname: row.Probe,
				Name:      fmt.Sprintf("%s L%d [%s]", row.Suite, row.Line, row.Mode),
			}
			if row.LatencyMs.Valid {
				tc.Time = fmt.Sprintf("%.3f", float64(row.LatencyMs.Int32)/1000)
				totalMs += int(row.LatencyMs.Int32)
			}
			switch row.Status {
			case probe.StatusViolated:
				tc.Failure = &junitVerdict{Type: "violated", Message: firstLine(row.Message), Body: row.Message}
				suite.Failures++
			case probe.StatusRejected:
				tc.Error = &junitVerdict{Type: "rejected", Message: firstLine(row.Message), Body: row.Message}
				suite.Errors++
			case probe.StatusCollected:
				tc.Skipped = &junitVerdict{Message: "已采集·未评估（任务被取消）"}
				suite.Skipped++
			}
			suite.Tests++
			suite.Cases = append(suite.Cases, tc)
		}
		suite.Time = fmt.Sprintf("%.3f", float64(totalMs)/1000)
		root.Tests += suite.Tests
		root.Failures += suite.Failures
		root.Errors += suite.Errors
		root.Skipped += suite.Skipped
		rootMs += totalMs
		root.Suites = append(root.Suites, suite)
	}
	root.Time = fmt.Sprintf("%.3f", float64(rootMs)/1000)

	return xml.MarshalIndent(root, "", "  ")
}

// firstLine junit 属性位截断到首行且至多 200 字符（按 rune 数防止劈开多字节），完整信息在元素体内。
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	n := 0
	for i := range s {
		if n == 200 {
			return s[:i]
		}
		n++
	}
	return s
}
