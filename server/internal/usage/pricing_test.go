package usage

import (
	"path/filepath"
	"testing"

	agenttypes "mindfs/server/internal/agent/types"
)

func intPtr(value int) *int { return &value }

func TestPriceCostSplitsCachedInput(t *testing.T) {
	price := Price{Input: 3, Output: 15, CacheRead: 0.3}

	// InputTokens is the logical input total and already contains the cached
	// portion, so charging the full input rate for all of it would double-bill.
	cost := price.Cost(&agenttypes.TokenUsage{
		InputTokens:     1_000_000,
		OutputTokens:    1_000_000,
		CacheReadTokens: intPtr(800_000),
	})
	want := 200_000.0/1_000_000*3 + 800_000.0/1_000_000*0.3 + 15
	if diff := cost - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("cost = %v, want %v", cost, want)
	}
}

func TestPriceCostFallsBackToInputRateWithoutCacheRate(t *testing.T) {
	price := Price{Input: 2, Output: 10}
	cost := price.Cost(&agenttypes.TokenUsage{
		InputTokens:     500_000,
		OutputTokens:    100_000,
		CacheReadTokens: intPtr(400_000),
	})
	want := 500_000.0/1_000_000*2 + 100_000.0/1_000_000*10
	if diff := cost - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("cost = %v, want %v", cost, want)
	}
}

func TestPriceCostChargesCacheWrites(t *testing.T) {
	price := Price{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}
	cost := price.Cost(&agenttypes.TokenUsage{
		InputTokens:      1_000_000,
		CacheReadTokens:  intPtr(600_000),
		CacheWriteTokens: intPtr(300_000),
	})
	want := 100_000.0/1_000_000*3 + 600_000.0/1_000_000*0.3 + 300_000.0/1_000_000*3.75
	if diff := cost - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("cost = %v, want %v", cost, want)
	}
}

func TestPriceCostNeverChargesNegativeUncachedInput(t *testing.T) {
	price := Price{Input: 3, Output: 15, CacheRead: 0.3}
	// A backend that reports cache tokens greater than the logical input must
	// not produce a negative uncached charge.
	cost := price.Cost(&agenttypes.TokenUsage{
		InputTokens:     100,
		CacheReadTokens: intPtr(500),
	})
	if cost < 0 {
		t.Fatalf("cost = %v, want non-negative", cost)
	}
}

func TestPriceTableLookup(t *testing.T) {
	table := PriceTable{
		"gpt-5.6-sol":  {Input: 1},
		"GLM-5.3":      {Input: 2},
		"9779/":        {Input: 3},
		"openrouter/*": {Input: 4},
	}
	cases := []struct {
		model string
		want  float64
		ok    bool
	}{
		{"gpt-5.6-sol", 1, true},
		{"9779/gpt-5.6-sol", 3, true}, // longest gateway prefix wins
		{"9779/anything", 3, true},
		{"glm-5.3", 2, true},        // case-insensitive
		{"openrouter/x/y", 4, true}, // wildcard suffix
		{"unknown-model", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := table.Lookup(tc.model)
		if ok != tc.ok {
			t.Errorf("Lookup(%q) ok = %v, want %v", tc.model, ok, tc.ok)
			continue
		}
		if ok && got.Input != tc.want {
			t.Errorf("Lookup(%q).Input = %v, want %v", tc.model, got.Input, tc.want)
		}
	}
}

func TestPriceTableCostForReportsUnknownPrice(t *testing.T) {
	table := PriceTable{"known": {Input: 1, Output: 1}}
	if _, ok := table.CostFor("known", &agenttypes.TokenUsage{InputTokens: 1}); !ok {
		t.Fatal("known model should report a price")
	}
	if _, ok := table.CostFor("mystery", &agenttypes.TokenUsage{InputTokens: 1}); ok {
		t.Fatal("unknown model must report no price rather than a free turn")
	}
	if _, ok := table.CostFor("known", nil); ok {
		t.Fatal("nil usage must not report a price")
	}
}

func TestStoreSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	store, err := LoadStore()
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if len(store.Prices()) != 0 {
		t.Fatal("fresh store should have no prices")
	}

	if err := store.Save(PriceTable{"gpt-5.6-sol": {Input: 1.25, Output: 10}}, Budget{DailyUSD: 25}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := LoadStore()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	_prices := reloaded.Prices()
	price, ok := _prices.Lookup("gpt-5.6-sol")
	if !ok || price.Input != 1.25 || price.Output != 10 {
		t.Fatalf("reloaded price = %#v ok=%v", price, ok)
	}
	budget := reloaded.Budget()
	if budget.DailyUSD != 25 {
		t.Fatalf("budget = %#v", budget)
	}
	if budget.NotifyPercent != defaultNotifyPercent {
		t.Fatalf("NotifyPercent = %d, want default %d", budget.NotifyPercent, defaultNotifyPercent)
	}
	_ = filepath.Join
}

func TestStoreNormalizesInvalidBudget(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	store, err := LoadStore()
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if err := store.Save(PriceTable{"m": {Input: -5, Output: -1}}, Budget{DailyUSD: -3, NotifyPercent: 400}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	_prices := store.Prices()
	price, _ := _prices.Lookup("m")
	if price.Input != 0 || price.Output != 0 {
		t.Fatalf("negative prices should clamp to 0, got %#v", price)
	}
	budget := store.Budget()
	if budget.DailyUSD != 0 {
		t.Fatalf("negative budget should clamp to 0, got %v", budget.DailyUSD)
	}
	if budget.NotifyPercent != defaultNotifyPercent {
		t.Fatalf("out-of-range percent should reset to default, got %d", budget.NotifyPercent)
	}
}
