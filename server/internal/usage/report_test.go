package usage

import (
	"testing"
	"time"

	agenttypes "mindfs/server/internal/agent/types"
	"mindfs/server/internal/session"
)

func record(rootID, agent, model string, ts time.Time, input, output, cacheRead int) session.UsageRecord {
	usage := agenttypes.TokenUsage{InputTokens: input, OutputTokens: output}
	if cacheRead > 0 {
		usage.CacheReadTokens = intPtr(cacheRead)
	}
	return session.UsageRecord{
		RootID:     rootID,
		SessionKey: "s-" + rootID,
		Agent:      agent,
		Model:      model,
		Timestamp:  ts,
		Usage:      usage,
	}
}

func TestBuildAggregatesByDayAgentModelAndProject(t *testing.T) {
	loc := time.UTC
	today := time.Date(2026, time.September, 25, 12, 0, 0, 0, loc)

	records := []session.UsageRecord{
		record("mindfs", "pi", "deepseek-v4.1-flash", today.Add(-1*time.Hour), 1_000_000, 100_000, 0),
		record("mindfs", "pi", "deepseek-v4.1-flash", today.Add(-26*time.Hour), 500_000, 50_000, 0),
		record("qbot", "codex", "gpt-5.6-sol", today.Add(-2*time.Hour), 2_000_000, 200_000, 0),
	}

	report := Build(BuildOptions{
		Records:    records,
		Prices:     PriceTable{"deepseek-v4.1-flash": {Input: 1, Output: 2}, "gpt-5.6-sol": {Input: 3, Output: 15}},
		RootLabels: map[string]string{"mindfs": "MindFS", "qbot": "QQ Bot"},
		Today:      today,
		Days:       7,
	})

	if report.Days != 7 || report.From != "2026-09-19" || report.To != "2026-09-25" {
		t.Fatalf("window = %s..%s days=%d", report.From, report.To, report.Days)
	}
	if report.RecordCount != 3 {
		t.Fatalf("RecordCount = %d, want 3", report.RecordCount)
	}
	if report.Totals.Turns != 3 {
		t.Fatalf("Totals.Turns = %d, want 3", report.Totals.Turns)
	}
	if len(report.ByDay) != 7 {
		t.Fatalf("ByDay length = %d, want 7 (no gaps)", len(report.ByDay))
	}
	// Two distinct days carry usage.
	nonEmpty := 0
	for _, day := range report.ByDay {
		if day.Turns > 0 {
			nonEmpty++
		}
	}
	if nonEmpty != 2 {
		t.Fatalf("days with usage = %d, want 2", nonEmpty)
	}

	if len(report.ByAgent) != 2 {
		t.Fatalf("ByAgent = %#v", report.ByAgent)
	}
	// codex spent 2M*3 + 200k*15 = 9.0, pi spent 1M*1+100k*2 + 500k*1+50k*2 = 1.8
	if report.ByAgent[0].Key != "codex" {
		t.Fatalf("top agent = %q, want codex (highest cost first)", report.ByAgent[0].Key)
	}
	if diff := report.ByAgent[0].CostUSD - 9.0; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("codex cost = %v, want 9", report.ByAgent[0].CostUSD)
	}

	if len(report.ByModel) != 2 {
		t.Fatalf("ByModel = %#v", report.ByModel)
	}
	if len(report.ByProject) != 2 {
		t.Fatalf("ByProject = %#v", report.ByProject)
	}
	if report.ByProject[0].Label != "QQ Bot" {
		t.Fatalf("top project label = %q, want QQ Bot", report.ByProject[0].Label)
	}

	// Today only: the 26h-old record must be excluded.
	if report.Today.Turns != 2 {
		t.Fatalf("Today.Turns = %d, want 2", report.Today.Turns)
	}
}

func TestBuildCountsUnpricedModelsSeparately(t *testing.T) {
	today := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	report := Build(BuildOptions{
		Records: []session.UsageRecord{
			record("mindfs", "pi", "known-model", today, 1_000_000, 0, 0),
			record("mindfs", "pi", "mystery-model", today, 1_000_000, 0, 0),
		},
		Prices: PriceTable{"known-model": {Input: 1, Output: 1}},
		Today:  today,
		Days:   1,
	})

	if report.Totals.UnpricedRuns != 1 {
		t.Fatalf("UnpricedRuns = %d, want 1", report.Totals.UnpricedRuns)
	}
	if len(report.UnpricedModels) != 1 || report.UnpricedModels[0] != "mystery-model" {
		t.Fatalf("UnpricedModels = %#v", report.UnpricedModels)
	}
	// Priced models still contribute even when others are unknown.
	if diff := report.Totals.CostUSD - 1.0; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("CostUSD = %v, want 1", report.Totals.CostUSD)
	}
	if !report.PriceConfigured {
		t.Fatal("PriceConfigured should be true")
	}
}

func TestBuildPreviousWindowTrend(t *testing.T) {
	today := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	records := []session.UsageRecord{
		// Inside the 3-day window.
		record("mindfs", "pi", "m", today.Add(-24*time.Hour), 1_000_000, 0, 0),
		// Inside the previous 3-day window.
		record("mindfs", "pi", "m", today.Add(-4*24*time.Hour), 2_000_000, 0, 0),
		// Older than both windows.
		record("mindfs", "pi", "m", today.Add(-30*24*time.Hour), 9_000_000, 0, 0),
	}
	report := Build(BuildOptions{
		Records: records,
		Prices:  PriceTable{"m": {Input: 1, Output: 1}},
		Today:   today,
		Days:    3,
	})

	if diff := report.Totals.CostUSD - 1.0; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("window cost = %v, want 1", report.Totals.CostUSD)
	}
	if diff := report.PreviousTotals.CostUSD - 2.0; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("previous cost = %v, want 2", report.PreviousTotals.CostUSD)
	}
	if report.Totals.Turns != 1 || report.PreviousTotals.Turns != 1 {
		t.Fatalf("turns window=%d previous=%d", report.Totals.Turns, report.PreviousTotals.Turns)
	}
}

func TestBuildBudgetState(t *testing.T) {
	today := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	report := Build(BuildOptions{
		Records: []session.UsageRecord{
			record("mindfs", "pi", "m", today, 5_000_000, 0, 0),
		},
		Prices: PriceTable{"m": {Input: 2, Output: 2}},
		Budget: Budget{DailyUSD: 20, NotifyPercent: 80},
		Today:  today,
		Days:   1,
	})

	if diff := report.BudgetUsed - 10.0; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("BudgetUsed = %v, want 10", report.BudgetUsed)
	}
	if diff := report.BudgetRatio - 0.5; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("BudgetRatio = %v, want 0.5", report.BudgetRatio)
	}
	if report.BudgetExceeded {
		t.Fatal("BudgetExceeded should be false below the limit")
	}
}

func TestBudgetAlertLevels(t *testing.T) {
	base := Report{To: "2026-09-25", Budget: Budget{DailyUSD: 10, NotifyPercent: 80, NotifyOnExceed: true}}

	under := base
	under.BudgetUsed = 5
	if _, _, _, ok := BudgetAlert(under); ok {
		t.Fatal("under threshold must not alert")
	}

	warn := base
	warn.BudgetUsed = 8
	title, _, eventID, ok := BudgetAlert(warn)
	if !ok || title != "接近预算" {
		t.Fatalf("warning alert = %q ok=%v", title, ok)
	}
	if eventID != warningEventID("2026-09-25") {
		t.Fatalf("warning eventID = %q", eventID)
	}
	if n := BudgetNotification(title, "body", eventID); n.RequireInteraction {
		t.Fatal("warning must not require interaction")
	}

	over := base
	over.BudgetUsed = 12
	title, _, eventID, ok = BudgetAlert(over)
	if !ok || title != "超预算" {
		t.Fatalf("exceeded alert = %q ok=%v", title, ok)
	}
	if eventID != exceededEventID("2026-09-25") {
		t.Fatalf("exceeded eventID = %q", eventID)
	}
	if n := BudgetNotification(title, "body", eventID); !n.RequireInteraction {
		t.Fatal("exceeded alert should require interaction")
	}

	// Disabling the exceed notification suppresses only that level.
	over.Budget.NotifyOnExceed = false
	if _, _, _, ok := BudgetAlert(over); ok {
		t.Fatal("exceed alert must respect NotifyOnExceed")
	}

	// No budget means no alerts at all.
	nobudget := base
	nobudget.Budget.DailyUSD = 0
	nobudget.BudgetUsed = 100
	if _, _, _, ok := BudgetAlert(nobudget); ok {
		t.Fatal("unset budget must not alert")
	}
}

func TestBuildClampsDayWindow(t *testing.T) {
	today := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	report := Build(BuildOptions{Today: today, Days: 10_000})
	if report.Days != MaxReportDays {
		t.Fatalf("Days = %d, want clamp to %d", report.Days, MaxReportDays)
	}
	if len(report.ByDay) != MaxReportDays {
		t.Fatalf("ByDay length = %d, want %d", len(report.ByDay), MaxReportDays)
	}

	defaulted := Build(BuildOptions{Today: today})
	if defaulted.Days != DefaultReportDays {
		t.Fatalf("Days = %d, want default %d", defaulted.Days, DefaultReportDays)
	}
}

func TestBuildFacetsPartialScan(t *testing.T) {
	report := Build(BuildOptions{
		Today:         time.Now(),
		Days:          1,
		Partial:       true,
		SkippedFiles:  2,
		SkippedReason: "line_too_long",
		ScannedFiles:  10,
	})
	if !report.Partial || report.SkippedFiles != 2 || report.SkippedReason != "line_too_long" {
		t.Fatalf("partial facets not reported: %#v", report)
	}
	if report.ScannedFileCount != 10 {
		t.Fatalf("ScannedFileCount = %d", report.ScannedFileCount)
	}
}
