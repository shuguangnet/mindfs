package usecase

import (
	"context"
	"strings"
	"time"

	"mindfs/server/internal/fs"
	"mindfs/server/internal/session"
	"mindfs/server/internal/usage"
)

// UsageServiceInput configures a report request.
type UsageServiceInput struct {
	// RootID restricts the report to one project; empty aggregates all roots.
	RootID string
	// Days is the window length ending today.
	Days int
}

// BuildUsageReport scans every relevant root and assembles the report.
func (s *Service) BuildUsageReport(ctx context.Context, in UsageServiceInput) (usage.Report, error) {
	if err := s.ensureRegistry(); err != nil {
		return usage.Report{}, err
	}
	days := in.Days
	if days <= 0 {
		days = usage.DefaultReportDays
	}
	if days > usage.MaxReportDays {
		days = usage.MaxReportDays
	}
	// Scan a wider window than requested so the previous-period comparison and
	// the daily series are both complete.
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	scanFrom := todayStart.AddDate(0, 0, -(2*days + 1))

	roots := s.usageRoots(in.RootID)
	rootLabels := make(map[string]string, len(roots))
	var records []session.UsageRecord
	scannedFiles := 0
	skippedFiles := 0
	skippedReason := ""
	partial := false

	for _, root := range roots {
		manager, err := s.Registry.GetSessionManager(root.ID)
		if err != nil {
			// One unreadable project must not blank the whole report.
			partial = true
			continue
		}
		result, err := manager.ScanUsage(ctx, root.ID, session.UsageScanOptions{After: scanFrom})
		if err != nil {
			partial = true
			continue
		}
		records = append(records, result.Records...)
		scannedFiles += result.ScannedFiles
		skippedFiles += result.SkippedFiles
		if result.SkippedReason != "" {
			skippedReason = result.SkippedReason
		}
		label := strings.TrimSpace(root.Name)
		if label == "" {
			label = root.ID
		}
		rootLabels[root.ID] = label
	}

	store := s.Registry.GetUsageStore()
	prices := usage.PriceTable{}
	budget := usage.Budget{}
	if store != nil {
		prices = store.Prices()
		budget = store.Budget()
	}

	return usage.Build(usage.BuildOptions{
		Records:       records,
		Prices:        prices,
		Budget:        budget,
		RootLabels:    rootLabels,
		Today:         now,
		Days:          days,
		ScannedFiles:  scannedFiles,
		SkippedFiles:  skippedFiles,
		SkippedReason: skippedReason,
		Partial:       partial,
	}), nil
}

// usageRoots resolves which roots to scan for a report.
func (s *Service) usageRoots(rootID string) []fs.RootInfo {
	trimmed := strings.TrimSpace(rootID)
	if trimmed != "" {
		if root, err := s.Registry.GetRoot(trimmed); err == nil {
			return []fs.RootInfo{root}
		}
		return nil
	}
	roots := s.Registry.ListRoots()
	out := make([]fs.RootInfo, 0, len(roots))
	for _, root := range roots {
		out = append(out, root)
	}
	return out
}

// GetUsagePreferences returns prices and budget.
func (s *Service) GetUsagePreferences() (usage.PriceTable, usage.Budget, error) {
	store := s.Registry.GetUsageStore()
	if store == nil {
		return usage.PriceTable{}, usage.Budget{}, nil
	}
	return store.Prices(), store.Budget(), nil
}

// SaveUsagePreferences replaces prices and budget.
func (s *Service) SaveUsagePreferences(prices usage.PriceTable, budget usage.Budget) error {
	store := s.Registry.GetUsageStore()
	if store == nil {
		return nil
	}
	return store.Save(prices, budget)
}
