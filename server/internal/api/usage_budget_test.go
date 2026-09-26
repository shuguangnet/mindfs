package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agenttypes "mindfs/server/internal/agent/types"
	rootfs "mindfs/server/internal/fs"
	"mindfs/server/internal/session"
	"mindfs/server/internal/usage"
)

// buildUsageAppContext wires just enough of AppContext to exercise the budget
// path end to end: a real project with a real exchange log.
func buildUsageAppContext(t *testing.T) (*AppContext, *session.Manager, *session.Session) {
	t.Helper()
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("HOME", configDir)

	projectDir := t.TempDir()
	registry := rootfs.NewRegistry(filepath.Join(configDir, "registry.json"))
	root, err := registry.Upsert(projectDir)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	store, err := usage.LoadStore()
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	app := &AppContext{Dirs: registry, Usage: store}
	manager, err := app.GetSessionManager(root.ID)
	if err != nil {
		t.Fatalf("GetSessionManager: %v", err)
	}
	created, err := manager.Create(context.Background(), session.CreateInput{Type: session.TypeChat, Name: "budget"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return app, manager, created
}

func appendUsageTurn(t *testing.T, manager *session.Manager, key, model string, input, output int, at time.Time) {
	t.Helper()
	path := manager.ExchangeLogAbsolutePath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	payload, err := json.Marshal(session.Exchange{
		Role: "agent", Agent: "codex", Model: model, Timestamp: at,
		TokenUsage: &agenttypes.TokenUsage{InputTokens: input, OutputTokens: output},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer file.Close()
	if _, err := file.Write(append(payload, '\n')); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

// The daily budget must be evaluated from real exchange logs and surface as a
// crossing alert once spend reaches the configured limit.
func TestCheckUsageBudgetReportsExceeded(t *testing.T) {
	app, manager, created := buildUsageAppContext(t)

	// $2 per 1M input tokens; spend $5 against a $1 budget.
	appendUsageTurn(t, manager, created.Key, "priced-model", 2_500_000, 0, time.Now().UTC())
	if err := app.Usage.Save(usage.PriceTable{"priced-model": {Input: 2, Output: 2}}, usage.Budget{
		DailyUSD:       1,
		NotifyPercent:  80,
		NotifyOnExceed: true,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	report, err := app.CheckUsageBudget(context.Background())
	if err != nil {
		t.Fatalf("CheckUsageBudget: %v", err)
	}
	if diff := report.BudgetUsed - 5.0; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("BudgetUsed = %v, want 5", report.BudgetUsed)
	}
	if !report.BudgetExceeded {
		t.Fatalf("BudgetExceeded = false, report = %#v", report.Totals)
	}

	_, _, eventID, ok := usage.BudgetAlert(report)
	if !ok {
		t.Fatal("an exceeded budget must produce an alert")
	}
	if !strings.HasPrefix(eventID, "usage.budget.exceeded:") {
		t.Fatalf("eventID = %q, want the exceeded key", eventID)
	}
}

// Below the threshold nothing is announced, and the warning level uses its own
// key so it can be sent on the same day as the exceeded alert.
func TestCheckUsageBudgetWarningLevel(t *testing.T) {
	app, manager, created := buildUsageAppContext(t)
	appendUsageTurn(t, manager, created.Key, "priced-model", 450_000, 0, time.Now().UTC())
	if err := app.Usage.Save(usage.PriceTable{"priced-model": {Input: 1, Output: 1}}, usage.Budget{
		DailyUSD:       1,
		NotifyPercent:  40,
		NotifyOnExceed: true,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	report, err := app.CheckUsageBudget(context.Background())
	if err != nil {
		t.Fatalf("CheckUsageBudget: %v", err)
	}
	if report.BudgetExceeded {
		t.Fatalf("BudgetExceeded = true at %v of %v", report.BudgetUsed, report.Budget.DailyUSD)
	}
	_, _, eventID, ok := usage.BudgetAlert(report)
	if !ok {
		t.Fatal("45% of a 40% threshold must alert")
	}
	if !strings.HasPrefix(eventID, "usage.budget.warning:") {
		t.Fatalf("eventID = %q, want the warning key", eventID)
	}
}

// With no budget configured the report must still build, without alerting.
func TestCheckUsageBudgetWithoutBudget(t *testing.T) {
	app, manager, created := buildUsageAppContext(t)
	appendUsageTurn(t, manager, created.Key, "priced-model", 1_000_000, 0, time.Now().UTC())

	report, err := app.CheckUsageBudget(context.Background())
	if err != nil {
		t.Fatalf("CheckUsageBudget: %v", err)
	}
	if report.Budget.DailyUSD != 0 {
		t.Fatalf("budget = %#v, want unset", report.Budget)
	}
	if _, _, _, ok := usage.BudgetAlert(report); ok {
		t.Fatal("no budget must mean no alert")
	}
	// The scan itself must still have found the turn.
	if report.Totals.Turns != 1 {
		t.Fatalf("Turns = %d, want 1", report.Totals.Turns)
	}
}
