// Package usage aggregates token consumption and estimated cost across every
// managed project, agent and model.
//
// Token usage is read from the session exchange logs (the source of truth for
// historical turns). Prices come from a user-editable table because most
// MindFS deployments run through custom gateways whose rates the upstream
// catalogs do not know.
package usage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	agenttypes "mindfs/server/internal/agent/types"
	"mindfs/server/internal/config"
)

// Price is the per-million-token rate for one model, in USD.
type Price struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// Budget configures the daily spend guardrail.
type Budget struct {
	// DailyUSD is the soft limit; 0 disables the budget entirely.
	DailyUSD float64 `json:"daily_usd"`
	// NotifyPercent triggers a warning once this share of the limit is spent.
	NotifyPercent int `json:"notify_percent,omitempty"`
	// NotifyOnExceed also sends a notification when the limit is crossed.
	NotifyOnExceed bool `json:"notify_on_exceed,omitempty"`
}

const (
	defaultNotifyPercent = 80
	// NoPriceModel is the bucket used when a model has no configured rate.
	NoPriceModel = ""
)

// PriceTable maps model identifiers to rates. Lookups are forgiving: an exact
// match wins, then a case-insensitive match, then the longest prefix match so
// `9779/gpt-5.6-sol` can be covered by `gpt-5.6-sol` or `9779/`.
type PriceTable map[string]Price

// Store persists prices and the budget to disk.
type Store struct {
	mu     sync.RWMutex
	path   string
	prices PriceTable
	budget Budget
}

// Preferences is the on-disk shape.
type Preferences struct {
	Prices PriceTable `json:"prices,omitempty"`
	Budget Budget     `json:"budget,omitempty"`
}

func storePath() (string, error) {
	dir, err := config.MindFSConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "usage.json"), nil
}

// LoadStore reads prices/budget from the MindFS config directory. A missing
// file yields an empty store, not an error.
func LoadStore() (*Store, error) {
	path, err := storePath()
	if err != nil {
		return nil, err
	}
	store := &Store{path: path, prices: PriceTable{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	var prefs Preferences
	if err := json.Unmarshal(raw, &prefs); err != nil {
		return nil, err
	}
	store.prices = normalizePriceTable(prefs.Prices)
	store.budget = normalizeBudget(prefs.Budget)
	return store, nil
}

// Prices returns a copy of the price table.
func (s *Store) Prices() PriceTable {
	if s == nil {
		return PriceTable{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePriceTable(s.prices)
}

// Budget returns the configured budget.
func (s *Store) Budget() Budget {
	if s == nil {
		return Budget{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.budget
}

// Save replaces the price table and budget.
func (s *Store) Save(prices PriceTable, budget Budget) error {
	if s == nil {
		return errors.New("usage store not configured")
	}
	normalizedPrices := normalizePriceTable(prices)
	normalizedBudget := normalizeBudget(budget)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prices = normalizedPrices
	s.budget = normalizedBudget
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return errors.New("usage store path not configured")
	}
	payload, err := json.MarshalIndent(Preferences{Prices: s.prices, Budget: s.budget}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(payload, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(s.path)
		if retryErr := os.Rename(tmp, s.path); retryErr != nil {
			return err
		}
	}
	return nil
}

func normalizePriceTable(prices PriceTable) PriceTable {
	out := make(PriceTable, len(prices))
	for key, price := range prices {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		out[name] = normalizePrice(price)
	}
	return out
}

func normalizePrice(price Price) Price {
	return Price{
		Input:      maxFloat(0, price.Input),
		Output:     maxFloat(0, price.Output),
		CacheRead:  maxFloat(0, price.CacheRead),
		CacheWrite: maxFloat(0, price.CacheWrite),
	}
}

func normalizeBudget(budget Budget) Budget {
	if budget.DailyUSD < 0 {
		budget.DailyUSD = 0
	}
	if budget.NotifyPercent <= 0 || budget.NotifyPercent > 100 {
		budget.NotifyPercent = defaultNotifyPercent
	}
	return budget
}

func clonePriceTable(prices PriceTable) PriceTable {
	out := make(PriceTable, len(prices))
	for key, price := range prices {
		out[key] = price
	}
	return out
}

// Lookup resolves the rate for a model name. It tries the raw name, a
// case-insensitive match, then progressively shorter prefixes split on `/`, `:`
// and `-`. The second return value reports whether a rate was found.
func (s *PriceTable) Lookup(model string) (Price, bool) {
	if s == nil {
		return Price{}, false
	}
	table := *s
	name := strings.TrimSpace(model)
	if name == "" {
		return Price{}, false
	}
	if price, ok := table[name]; ok {
		return price, true
	}
	lower := strings.ToLower(name)
	for key, price := range table {
		if strings.ToLower(key) == lower {
			return price, true
		}
	}
	// Longest-prefix match so a family or gateway entry can cover many models.
	bestKey := ""
	bestPrice := Price{}
	for key, price := range table {
		if !strings.HasSuffix(key, "/") && !strings.HasSuffix(key, "*") {
			continue
		}
		prefix := strings.TrimSuffix(strings.TrimSuffix(key, "*"), "/")
		if !strings.HasPrefix(lower, strings.ToLower(prefix)) {
			continue
		}
		if len(prefix) > len(bestKey) {
			bestKey = prefix
			bestPrice = price
		}
	}
	if bestKey != "" {
		return bestPrice, true
	}
	return Price{}, false
}

// CostFor computes the USD cost of one usage record. The boolean reports
// whether a price was known; a false result means the tokens are counted but
// the cost is unknown.
func (p PriceTable) CostFor(model string, usage *agenttypes.TokenUsage) (float64, bool) {
	if usage == nil {
		return 0, false
	}
	price, ok := p.Lookup(model)
	if !ok {
		return 0, false
	}
	return price.Cost(usage), true
}

// Cost applies the rate to a usage record.
func (p Price) Cost(usage *agenttypes.TokenUsage) float64 {
	if usage == nil {
		return 0
	}
	cacheRead := derefInt(usage.CacheReadTokens)
	cacheWrite := derefInt(usage.CacheWriteTokens)
	// InputTokens is the logical input size, which already includes cache reads
	// and writes for backends that report them (see the claude/codex/ACP
	// normalizers). Charge the cached portions at their own rate and the
	// remainder at the standard input rate, so a cache hit is never billed
	// twice.
	uncachedInput := usage.InputTokens - cacheRead - cacheWrite
	if uncachedInput < 0 {
		uncachedInput = 0
	}
	readRate := p.CacheRead
	if readRate == 0 {
		readRate = p.Input
	}
	writeRate := p.CacheWrite
	if writeRate == 0 {
		writeRate = p.Input
	}
	cost := float64(uncachedInput) / 1_000_000 * p.Input
	cost += float64(cacheRead) / 1_000_000 * readRate
	cost += float64(cacheWrite) / 1_000_000 * writeRate
	cost += float64(usage.OutputTokens) / 1_000_000 * p.Output
	return cost
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	if *value < 0 {
		return 0
	}
	return *value
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ModelNames returns the configured model keys, sorted, for UI pickers.
func (s *PriceTable) ModelNames() []string {
	if s == nil {
		return nil
	}
	names := make([]string, 0, len(*s))
	for name := range *s {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
