package usage

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"mindfs/server/internal/session"
)

// Totals is a token/cost rollup.
type Totals struct {
	Turns        int     `json:"turns"`
	Input        int     `json:"input"`
	Output       int     `json:"output"`
	CacheRead    int     `json:"cache_read"`
	CacheWrite   int     `json:"cache_write"`
	TotalTokens  int     `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	UnpricedRuns int     `json:"unpriced_runs"`
}

// Add accumulates a single turn.
func (t *Totals) Add(usage UsageView) {
	t.Turns++
	t.Input += usage.Input
	t.Output += usage.Output
	t.CacheRead += usage.CacheRead
	t.CacheWrite += usage.CacheWrite
	t.TotalTokens += usage.Total
	t.CostUSD += usage.CostUSD
	if !usage.Priced {
		t.UnpricedRuns++
	}
}

// UsageView is one turn with its resolved cost.
type UsageView struct {
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
	Total      int
	CostUSD    float64
	Priced     bool
}

// DayBucket is one calendar day of usage.
type DayBucket struct {
	Date string `json:"date"`
	Totals
}

// GroupBucket is a usage rollup for one agent, model or project.
type GroupBucket struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// RootID is set for project buckets.
	RootID string `json:"root_id,omitempty"`
	Totals
}

// Report is the full usage report returned to the UI.
type Report struct {
	// Range describes the requested window in inclusive local-date terms.
	From string `json:"from"`
	To   string `json:"to"`
	// Days is the number of days in the window.
	Days int `json:"days"`
	// Totals covers the whole window.
	Totals Totals `json:"totals"`
	// Today and WindowBudget support the budget indicator.
	Today      Totals  `json:"today"`
	Budget     Budget  `json:"budget"`
	BudgetUsed float64 `json:"budget_used_usd"`
	// BudgetRatio is spend divided by the limit; 0 when no budget is set.
	BudgetRatio float64 `json:"budget_ratio"`
	// BudgetExceeded is true once spend reaches the limit.
	BudgetExceeded bool `json:"budget_exceeded"`

	ByDay     []DayBucket   `json:"by_day"`
	ByAgent   []GroupBucket `json:"by_agent"`
	ByModel   []GroupBucket `json:"by_model"`
	ByProject []GroupBucket `json:"by_project"`

	// PreviousTotals covers the equally sized window immediately before this
	// one so the UI can show a trend.
	PreviousTotals Totals `json:"previous_totals"`

	// Partial is true when some exchange logs could not be fully read.
	Partial          bool     `json:"partial,omitempty"`
	SkippedFiles     int      `json:"skipped_files,omitempty"`
	SkippedReason    string   `json:"skipped_reason,omitempty"`
	UnpricedModels   []string `json:"unpriced_models,omitempty"`
	PriceConfigured  bool     `json:"price_configured"`
	RecordCount      int      `json:"record_count"`
	ScannedFileCount int      `json:"scanned_file_count"`
}

// BuildOptions carries everything needed to assemble a report.
type BuildOptions struct {
	// Records are the scanned turns (all roots combined).
	Records []session.UsageRecord
	// Prices resolves per-model rates.
	Prices PriceTable
	// Budget is the configured guardrail.
	Budget Budget
	// RootLabels maps root id to a display name.
	RootLabels map[string]string
	// Today is the reference "now" so tests can pin the clock.
	Today time.Time
	// Days is the window length, including today.
	Days int
	// Scan metadata, passed through to the report.
	ScannedFiles  int
	SkippedFiles  int
	SkippedReason string
	Partial       bool
}

// MaxReportDays bounds the requested window so a bad query cannot trigger an
// unbounded scan.
const MaxReportDays = 365

// DefaultReportDays is the window used when the caller does not specify one.
const DefaultReportDays = 30

// Build assembles the report from pre-scanned records.
func Build(opts BuildOptions) Report {
	days := opts.Days
	if days <= 0 {
		days = DefaultReportDays
	}
	if days > MaxReportDays {
		days = MaxReportDays
	}
	reference := opts.Today
	if reference.IsZero() {
		reference = time.Now()
	}
	location := reference.Location()

	// Window: the last `days` calendar days ending today, inclusive.
	todayStart := time.Date(reference.Year(), reference.Month(), reference.Day(), 0, 0, 0, 0, location)
	windowStart := todayStart.AddDate(0, 0, -(days - 1))
	windowEnd := todayStart.AddDate(0, 0, 1)
	previousStart := windowStart.AddDate(0, 0, -days)

	report := Report{
		From:             windowStart.Format(dateLayout),
		To:               todayStart.Format(dateLayout),
		Days:             days,
		Budget:           normalizeBudget(opts.Budget),
		Partial:          opts.Partial,
		SkippedFiles:     opts.SkippedFiles,
		SkippedReason:    opts.SkippedReason,
		ScannedFileCount: opts.ScannedFiles,
	}
	report.PriceConfigured = len(opts.Prices) > 0

	dayTotals := make(map[string]*Totals, days)
	// Pre-seed every day in the window so the chart has no gaps.
	dayOrder := make([]string, 0, days)
	for i := 0; i < days; i++ {
		key := windowStart.AddDate(0, 0, i).Format(dateLayout)
		dayOrder = append(dayOrder, key)
		dayTotals[key] = &Totals{}
	}

	agentTotals := map[string]*Totals{}
	agentLabels := map[string]string{}
	modelTotals := map[string]*Totals{}
	modelLabels := map[string]string{}
	projectTotals := map[string]*Totals{}
	projectLabels := map[string]string{}
	unpriced := map[string]bool{}

	for _, record := range opts.Records {
		view := opts.Prices.view(record)
		if !view.Priced {
			unpriced[displayModel(record)] = true
		}
		local := record.Timestamp.In(location)
		dateKey := local.Format(dateLayout)

		if local.Before(windowStart) {
			if !local.Before(previousStart) {
				report.PreviousTotals.Add(view)
			}
			continue
		}
		if !local.Before(windowEnd) {
			// Future timestamps are excluded from the window but still counted
			// in the overall totals so nothing silently disappears.
		}
		if local.Before(windowEnd) {
			report.RecordCount++
			report.Totals.Add(view)
			if bucket := dayTotals[dateKey]; bucket != nil {
				bucket.Add(view)
			}
			if dateKey == report.To {
				report.Today.Add(view)
			}
			addToGroup(agentTotals, agentLabels, agentKey(record), agentLabel(record), view)
			modelKey := displayModel(record)
			addToGroup(modelTotals, modelLabels, modelKey, modelKey, view)
			projectKey := strings.TrimSpace(record.RootID)
			if projectKey != "" {
				label := strings.TrimSpace(opts.RootLabels[projectKey])
				if label == "" {
					label = projectKey
				}
				addToGroup(projectTotals, projectLabels, projectKey, label, view)
			}
		}
	}

	report.ByDay = make([]DayBucket, 0, len(dayOrder))
	for _, key := range dayOrder {
		report.ByDay = append(report.ByDay, DayBucket{Date: key, Totals: *dayTotals[key]})
	}
	report.ByAgent = sortedGroups(agentTotals, agentLabels, "")
	report.ByModel = sortedGroups(modelTotals, modelLabels, "")
	// Models without a configured rate are reported first-class so the numbers
	// are never mistaken for the whole bill.
	report.ByProject = sortedGroups(projectTotals, projectLabels, "")
	report.UnpricedModels = sortedKeys(unpriced)

	if report.Budget.DailyUSD > 0 {
		report.BudgetUsed = report.Today.CostUSD
		report.BudgetRatio = report.BudgetUsed / report.Budget.DailyUSD
		report.BudgetExceeded = report.BudgetUsed >= report.Budget.DailyUSD
	}
	return report
}

const dateLayout = "2006-01-02"

func (prices PriceTable) view(record session.UsageRecord) UsageView {
	usage := record.Usage
	cacheRead := derefInt(usage.CacheReadTokens)
	cacheWrite := derefInt(usage.CacheWriteTokens)
	view := UsageView{
		Input:      usage.InputTokens,
		Output:     usage.OutputTokens,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
		Total:      usage.InputTokens + usage.OutputTokens,
	}
	model := displayModel(record)
	if price, ok := prices.Lookup(model); ok {
		view.CostUSD = price.Cost(&usage)
		view.Priced = true
	}
	return view
}

// displayModel prefers the raw model id, falling back to the display name so a
// turn is never dropped from the model breakdown.
func displayModel(record session.UsageRecord) string {
	if model := strings.TrimSpace(record.Model); model != "" {
		return model
	}
	if name := strings.TrimSpace(record.ModelDisplayName); name != "" {
		return name
	}
	return "(unknown)"
}

func agentKey(record session.UsageRecord) string {
	if agent := strings.TrimSpace(record.Agent); agent != "" {
		return agent
	}
	return "(unknown)"
}

func agentLabel(record session.UsageRecord) string {
	return agentKey(record)
}

func addToGroup(totals map[string]*Totals, labels map[string]string, key, label string, view UsageView) {
	bucket := totals[key]
	if bucket == nil {
		bucket = &Totals{}
		totals[key] = bucket
		labels[key] = label
	}
	bucket.Add(view)
}

// sortedGroups orders buckets by cost, then tokens, so the biggest spenders
// lead the table.
func sortedGroups(totals map[string]*Totals, labels map[string]string, rootIDKey string) []GroupBucket {
	out := make([]GroupBucket, 0, len(totals))
	for key, bucket := range totals {
		item := GroupBucket{Key: key, Label: labels[key], Totals: *bucket}
		if item.Label == "" {
			item.Label = key
		}
		if rootIDKey != "" {
			item.RootID = key
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CostUSD != out[j].CostUSD {
			return out[i].CostUSD > out[j].CostUSD
		}
		if out[i].TotalTokens != out[j].TotalTokens {
			return out[i].TotalTokens > out[j].TotalTokens
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// BudgetAlert describes the notification that should be sent for a report, if
// any. It returns false when nothing needs to be announced.
//
// Alerts are keyed by local date and threshold so the same warning is only
// pushed once per day.
func BudgetAlert(report Report) (title string, body string, eventID string, ok bool) {
	if report.Budget.DailyUSD <= 0 {
		return "", "", "", false
	}
	spent := report.BudgetUsed
	limit := report.Budget.DailyUSD
	percent := int(spent / limit * 100)

	if spent >= limit {
		if !report.Budget.NotifyOnExceed {
			return "", "", "", false
		}
		return "超预算", budgetBody(spent, limit, percent), exceededEventID(report.To), true
	}
	threshold := report.Budget.NotifyPercent
	if threshold <= 0 {
		return "", "", "", false
	}
	if percent >= threshold {
		return "接近预算", budgetBody(spent, limit, percent), warningEventID(report.To), true
	}
	return "", "", "", false
}

// EventID builders are exported so the notification layer and tests agree on the
// dedupe key without duplicating the format.
func exceededEventID(date string) string { return "usage.budget.exceeded:" + date }
func warningEventID(date string) string  { return "usage.budget.warning:" + date }

// BudgetNotification is the transport-ready form of a budget alert.
func BudgetNotification(alertTitle, alertBody, eventID string) Notification {
	return Notification{
		Title:              "MindFS · " + strings.TrimSpace(alertTitle),
		Body:               strings.TrimSpace(alertBody),
		Tag:                eventID,
		Type:               "usage.budget",
		URL:                "./?view=usage",
		RequireInteraction: strings.HasPrefix(eventID, "usage.budget.exceeded:"),
	}
}

// Notification carries a ready-to-send budget alert without depending on the
// notify package.
type Notification struct {
	Title              string
	Body               string
	Tag                string
	Type               string
	URL                string
	RequireInteraction bool
}

func budgetBody(spent, limit float64, percent int) string {
	return "今日已用 " + formatUSD(spent) + " / " + formatUSD(limit) +
		"（" + itoa(percent) + "%），" + "剩余 " + formatUSD(maxFloat(0, limit-spent))
}

func formatUSD(value float64) string {
	return "$" + trimFloat(value)
}

// trimFloat renders a float with 2-4 decimals, avoiding trailing zeros.
func trimFloat(value float64) string {
	text := strconv.FormatFloat(value, 'f', 4, 64)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" || text == "-" {
		return "0"
	}
	// Pad to at least two decimals so currency reads consistently.
	if !strings.Contains(text, ".") {
		return text + ".00"
	}
	decimals := len(text) - strings.Index(text, ".") - 1
	for decimals < 2 {
		text += "0"
		decimals++
	}
	return text
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
