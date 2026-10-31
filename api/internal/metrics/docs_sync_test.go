// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package metrics_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lusoris/SnatchArr/api/internal/metrics"
)

type docMetric struct {
	Name       string
	Type       string
	Labels     []string
	LeaderOnly bool
}

var metricNameRegex = regexp.MustCompile(`\b(snatcharr_[a-zA-Z0-9_]+)\b`)

func parseDocTable(scanner *bufio.Scanner, stopHeader string) ([]docMetric, error) {
	var list []docMetric
	inTable := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if stopHeader != "" && strings.HasPrefix(line, stopHeader) {
			break
		}
		if !strings.HasPrefix(line, "|") {
			if inTable {
				break
			}
			continue
		}
		parts := splitMarkdownRow(line)
		if len(parts) < 3 || parts[0] == "Metric" || strings.HasPrefix(parts[0], "---") {
			inTable = true
			continue
		}
		inTable = true
		dm := docMetric{
			Name:   cleanDocField(parts[0]),
			Type:   cleanDocField(parts[1]),
			Labels: parseDocLabels(cleanDocField(parts[2])),
		}
		if len(parts) >= 4 {
			dm.LeaderOnly = strings.EqualFold(cleanDocField(parts[3]), "yes")
		}
		list = append(list, dm)
	}
	return list, scanner.Err()
}

func splitMarkdownRow(line string) []string {
	trimmed := strings.Trim(line, "|")
	rawParts := strings.Split(trimmed, "|")
	parts := make([]string, len(rawParts))
	for i, p := range rawParts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

func cleanDocField(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "`")
	s = strings.TrimSuffix(s, "`")
	return strings.TrimSpace(s)
}

func parseDocLabels(s string) []string {
	if s == "" || s == "none" || s == "-" {
		return []string{}
	}
	raw := strings.Split(s, ",")
	labels := make([]string, 0, len(raw))
	for _, l := range raw {
		cl := cleanDocField(l)
		if cl != "" {
			labels = append(labels, cl)
		}
	}
	slices.Sort(labels)
	return labels
}

func readMetricsDoc(path string) (map[string]docMetric, map[string]docMetric, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open docs: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	apiMap := make(map[string]docMetric)
	workerMap := make(map[string]docMetric)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "## API Metrics") {
			items, err := parseDocTable(scanner, "## ")
			if err != nil {
				return nil, nil, err
			}
			for _, item := range items {
				apiMap[item.Name] = item
			}
		}
		if strings.HasPrefix(line, "## Worker Metrics") {
			items, err := parseDocTable(scanner, "## ")
			if err != nil {
				return nil, nil, err
			}
			for _, item := range items {
				workerMap[item.Name] = item
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("read docs: %w", err)
	}
	return apiMap, workerMap, nil
}

func populateRegistry(m *metrics.Metrics) {
	m.LoopTicked("planner", time.Second, time.Now(), nil)
	m.LoopTicked("planner", time.Second, time.Now(), errors.New("tick failed"))
	m.PlanFailed("sonarr-main")
	m.RunsQueued(1)
	m.RunLeased("missing")
	m.RunFinished("done")
	m.WorkerEvent("search_dispatched")
	m.EventsUnconfirmed(1)
	m.BudgetRequested(metrics.BudgetGranted)
	m.SearchesDispatched("sonarr-main", "missing", 1)
	m.SearchDispatchedAt("sonarr-main", time.Now())
	m.SetInstanceLastRunFinished("sonarr-main", "done", time.Now())
	m.SetInstanceRunInterval("sonarr-main", 1800)
	m.SetStaminaUsed("sonarr-main", 10)
	m.SetStaminaRemaining("sonarr-main", 40)
	m.SetRuns("queued", 1)
	m.SetSeerrRequestsPending(3)
	m.RunLeaseWait("missing", 5)
	m.PlannerDeferred(metrics.DeferralScheduleClosed)
}

func gatherCodeMetrics(m *metrics.Metrics, reg *prometheus.Registry) (map[string][]string, error) {
	populateRegistry(m)
	mfs, err := reg.Gather()
	if err != nil {
		return nil, fmt.Errorf("gather: %w", err)
	}
	codeMetrics := make(map[string][]string)
	for _, mf := range mfs {
		name := mf.GetName()
		var labels []string
		if len(mf.GetMetric()) > 0 {
			for _, lp := range mf.GetMetric()[0].GetLabel() {
				labels = append(labels, lp.GetName())
			}
		}
		slices.Sort(labels)
		codeMetrics[name] = labels
	}
	return codeMetrics, nil
}

func checkMetricsSync(codeMetrics map[string][]string, docMetrics map[string]docMetric) error {
	for name, codeLabels := range codeMetrics {
		doc, ok := docMetrics[name]
		if !ok {
			return fmt.Errorf("metric %q is registered in code but not documented in docs/metrics.md", name)
		}
		if !slices.Equal(codeLabels, doc.Labels) {
			return fmt.Errorf("metric %q has labels %v in code, but %v in docs/metrics.md", name, codeLabels, doc.Labels)
		}
	}
	for name := range docMetrics {
		if _, ok := codeMetrics[name]; !ok {
			return fmt.Errorf("metric %q is documented in docs/metrics.md but not registered in code", name)
		}
	}
	return nil
}

func findDocsPath(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "..", "docs", "metrics.md"),
		filepath.Join("docs", "metrics.md"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Fatal("cannot find docs/metrics.md from test execution directory")
	return ""
}

func findDashboardPath(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "..", "deploy", "helm", "snatcharr", "dashboards", "snatcharr.json"),
		filepath.Join("deploy", "helm", "snatcharr", "dashboards", "snatcharr.json"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Fatal("cannot find snatcharr.json dashboard from test execution directory")
	return ""
}

// Positive: every API metric registered in code is documented with matching labels, and vice versa.
func TestDocsSync_APIMetrics(t *testing.T) {
	t.Parallel()
	docPath := findDocsPath(t)
	apiDocs, _, err := readMetricsDoc(docPath)
	if err != nil {
		t.Fatalf("readMetricsDoc(%s) = %v", docPath, err)
	}
	if len(apiDocs) == 0 {
		t.Fatal("readMetricsDoc parsed 0 API metrics")
	}

	reg := prometheus.NewRegistry()
	m, err := metrics.New(reg)
	if err != nil {
		t.Fatalf("metrics.New = %v", err)
	}

	codeMetrics, err := gatherCodeMetrics(m, reg)
	if err != nil {
		t.Fatalf("gatherCodeMetrics = %v", err)
	}

	if err := checkMetricsSync(codeMetrics, apiDocs); err != nil {
		t.Errorf("API metrics / docs out of sync: %v", err)
	}
}

// Negative & boundary: planted cases prove the checker detects missing metrics, extra metrics, and label mismatches.
func TestDocsSync_APIMetrics_PlantedNegatives(t *testing.T) {
	t.Parallel()
	validCode := map[string][]string{
		"snatcharr_runs": {"status"},
		"snatcharr_test": {},
	}
	validDoc := map[string]docMetric{
		"snatcharr_runs": {Name: "snatcharr_runs", Labels: []string{"status"}},
		"snatcharr_test": {Name: "snatcharr_test", Labels: []string{}},
	}

	t.Run("planted missing in docs", func(t *testing.T) {
		t.Parallel()
		incompleteDoc := map[string]docMetric{
			"snatcharr_test": validDoc["snatcharr_test"],
		}
		err := checkMetricsSync(validCode, incompleteDoc)
		if err == nil || !strings.Contains(err.Error(), "not documented") {
			t.Errorf("checkMetricsSync() = %v, want 'not documented' error", err)
		}
	})

	t.Run("planted extra in docs", func(t *testing.T) {
		t.Parallel()
		extraDoc := map[string]docMetric{
			"snatcharr_runs":  validDoc["snatcharr_runs"],
			"snatcharr_test":  validDoc["snatcharr_test"],
			"snatcharr_ghost": {Name: "snatcharr_ghost", Labels: []string{}},
		}
		err := checkMetricsSync(validCode, extraDoc)
		if err == nil || !strings.Contains(err.Error(), "not registered in code") {
			t.Errorf("checkMetricsSync() = %v, want 'not registered in code' error", err)
		}
	})

	t.Run("planted label mismatch", func(t *testing.T) {
		t.Parallel()
		wrongLabelDoc := map[string]docMetric{
			"snatcharr_runs": {Name: "snatcharr_runs", Labels: []string{"wrong"}},
			"snatcharr_test": validDoc["snatcharr_test"],
		}
		err := checkMetricsSync(validCode, wrongLabelDoc)
		if err == nil || !strings.Contains(err.Error(), "has labels") {
			t.Errorf("checkMetricsSync() = %v, want label mismatch error", err)
		}
	})
}

func extractPanelTargets(panel map[string]any) []string {
	rawTargets, ok := panel["targets"].([]any)
	if !ok {
		return nil
	}
	var exprs []string
	for _, rawTarget := range rawTargets {
		if target, ok := rawTarget.(map[string]any); ok {
			if expr, ok := target["expr"].(string); ok && expr != "" {
				exprs = append(exprs, expr)
			}
		}
	}
	return exprs
}

func extractDashboardMetrics(data []byte) ([]string, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("unmarshal dashboard: %w", err)
	}
	var queue []map[string]any
	if rawPanels, ok := root["panels"].([]any); ok {
		for _, rawP := range rawPanels {
			if panel, ok := rawP.(map[string]any); ok {
				queue = append(queue, panel)
			}
		}
	}
	var exprs []string
	for len(queue) > 0 {
		panel := queue[0]
		queue = queue[1:]
		exprs = append(exprs, extractPanelTargets(panel)...)
		if rawNested, ok := panel["panels"].([]any); ok {
			for _, rawP := range rawNested {
				if sub, ok := rawP.(map[string]any); ok {
					queue = append(queue, sub)
				}
			}
		}
	}
	metricSet := make(map[string]struct{})
	for _, expr := range exprs {
		matches := metricNameRegex.FindAllString(expr, -1)
		for _, m := range matches {
			base := strings.TrimSuffix(m, "_bucket")
			base = strings.TrimSuffix(base, "_sum")
			base = strings.TrimSuffix(base, "_count")
			metricSet[base] = struct{}{}
		}
	}
	names := make([]string, 0, len(metricSet))
	for n := range metricSet {
		names = append(names, n)
	}
	slices.Sort(names)
	return names, nil
}

func checkDashboardMetrics(data []byte, allDocs map[string]docMetric) error {
	names, err := extractDashboardMetrics(data)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New("dashboard contains zero snatcharr metrics")
	}
	for _, name := range names {
		if _, ok := allDocs[name]; !ok {
			return fmt.Errorf("dashboard metric %q is not documented in docs/metrics.md", name)
		}
	}
	return nil
}

// Positive: every metric used in any panel expression of snatcharr.json is documented in docs/metrics.md.
func TestDocsSync_DashboardMetrics(t *testing.T) {
	t.Parallel()
	docPath := findDocsPath(t)
	apiDocs, workerDocs, err := readMetricsDoc(docPath)
	if err != nil {
		t.Fatalf("readMetricsDoc = %v", err)
	}
	allDocs := make(map[string]docMetric, len(apiDocs)+len(workerDocs))
	maps.Copy(allDocs, apiDocs)
	maps.Copy(allDocs, workerDocs)

	dashPath := findDashboardPath(t)
	data, err := os.ReadFile(dashPath)
	if err != nil {
		t.Fatalf("read dashboard %s: %v", dashPath, err)
	}

	if err := checkDashboardMetrics(data, allDocs); err != nil {
		t.Errorf("Dashboard uses undocumented metrics: %v", err)
	}
}

// Negative: planted testdata/invalid_dashboard.json with an unknown metric fails the check.
func TestDocsSync_DashboardMetrics_PlantedNegative(t *testing.T) {
	t.Parallel()
	docPath := findDocsPath(t)
	apiDocs, workerDocs, err := readMetricsDoc(docPath)
	if err != nil {
		t.Fatalf("readMetricsDoc = %v", err)
	}
	allDocs := make(map[string]docMetric, len(apiDocs)+len(workerDocs))
	maps.Copy(allDocs, apiDocs)
	maps.Copy(allDocs, workerDocs)

	invalidPath := filepath.Join("testdata", "invalid_dashboard.json")
	data, err := os.ReadFile(invalidPath)
	if err != nil {
		t.Fatalf("read %s: %v", invalidPath, err)
	}

	err = checkDashboardMetrics(data, allDocs)
	if err == nil {
		t.Fatal("checkDashboardMetrics(invalid_dashboard.json) succeeded, want failure on planted unknown metric")
	}
	if !strings.Contains(err.Error(), "snatcharr_nonexistent_planted_defect_total") {
		t.Errorf("error = %v, want mention of snatcharr_nonexistent_planted_defect_total", err)
	}
}
