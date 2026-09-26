package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	agenttypes "mindfs/server/internal/agent/types"
)

// UsageRecord is one billable agent turn, flattened with the owning session's
// metadata so callers can aggregate by day, project, agent or model.
type UsageRecord struct {
	RootID           string
	SessionKey       string
	SessionName      string
	SessionType      string
	TaskID           string
	Agent            string
	Model            string
	ModelDisplayName string
	Timestamp        time.Time
	Usage            agenttypes.TokenUsage
}

// UsageScanOptions bounds and filters a usage scan.
type UsageScanOptions struct {
	// After/Before filter by turn timestamp; zero means unbounded.
	After  time.Time
	Before time.Time
}

// UsageScanResult reports the scan outcome, including which files were skipped
// so the UI can disclose that the totals are partial instead of quietly
// under-reporting.
type UsageScanResult struct {
	Records       []UsageRecord
	ScannedFiles  int
	SkippedFiles  int
	SkippedReason string
}

// maxUsageRecordBytes bounds a single exchange line. The majority of log volume
// is tool output, which is irrelevant for usage accounting, so oversized entries
// are skipped rather than buffered.
const maxUsageRecordBytes = 4 * 1024 * 1024

// usageScanConcurrency bounds parallel log reads across sessions.
const usageScanConcurrency = 8

// ScanUsage reads every session's exchange log and returns the token usage of
// each agent turn. Non-agent roles and turns without usage telemetry are
// skipped, which is the common case for user messages.
func (m *Manager) ScanUsage(ctx context.Context, rootID string, opts UsageScanOptions) (UsageScanResult, error) {
	metas, err := m.ListMetas(ctx)
	if err != nil {
		return UsageScanResult{}, err
	}
	type job struct {
		meta    *Session
		absPath string
	}
	jobs := make([]job, 0, len(metas))
	result := UsageScanResult{}
	for _, meta := range metas {
		if meta == nil || meta.Type == TypeCommand {
			continue
		}
		absPath := m.ExchangeLogAbsolutePath(meta.Key)
		if strings.TrimSpace(absPath) == "" {
			continue
		}
		jobs = append(jobs, job{meta: meta, absPath: absPath})
	}
	if len(jobs) == 0 {
		return result, nil
	}

	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		sem  = make(chan struct{}, usageScanConcurrency)
		skip = map[string]int{}
	)
	for _, item := range jobs {
		if err := ctx.Err(); err != nil {
			wg.Wait()
			return result, err
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(item job) {
			defer wg.Done()
			defer func() { <-sem }()
			records, files, skipped, reason := scanUsageFile(item.absPath, rootID, item.meta, opts)
			mu.Lock()
			defer mu.Unlock()
			result.Records = append(result.Records, records...)
			result.ScannedFiles += files
			if skipped {
				result.SkippedFiles++
				if reason != "" {
					skip[reason]++
				}
			}
		}(item)
	}
	wg.Wait()

	if len(skip) > 0 {
		result.SkippedReason = dominantSkipReason(skip)
	}
	return result, nil
}

// scanUsageFile reads one exchange log. The boolean reports whether the file
// could not be read in full.
func scanUsageFile(path, rootID string, meta *Session, opts UsageScanOptions) ([]UsageRecord, int, bool, string) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, false, ""
		}
		log.Printf("[usage] scan.open.error session=%s path=%s err=%v", meta.Key, path, err)
		return nil, 0, true, "open_failed"
	}
	defer file.Close()

	var records []UsageRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 256*1024), maxUsageRecordBytes)
	truncated := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry Exchange
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.TokenUsage == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(entry.Role), "agent") && !strings.EqualFold(strings.TrimSpace(entry.Role), "assistant") {
			continue
		}
		if entry.Timestamp.IsZero() {
			continue
		}
		ts := entry.Timestamp.UTC()
		if !opts.After.IsZero() && ts.Before(opts.After.UTC()) {
			continue
		}
		if !opts.Before.IsZero() && ts.After(opts.Before.UTC()) {
			continue
		}
		records = append(records, UsageRecord{
			RootID:           rootID,
			SessionKey:       meta.Key,
			SessionName:      meta.Name,
			SessionType:      meta.Type,
			TaskID:           meta.TaskID,
			Agent:            strings.TrimSpace(firstNonEmptyString(entry.Agent, meta.Agent)),
			Model:            strings.TrimSpace(firstNonEmptyString(entry.Model, meta.Model)),
			ModelDisplayName: strings.TrimSpace(entry.ModelDisplayName),
			Timestamp:        ts,
			Usage:            *entry.TokenUsage,
		})
	}
	if err := scanner.Err(); err != nil {
		// bufio reports ErrTooLong for an oversized line; keep what was parsed
		// and flag the file so the report can disclose it.
		log.Printf("[usage] scan.read.partial session=%s path=%s err=%v", meta.Key, path, err)
		truncated = true
	}
	return records, 1, truncated, "line_too_long"
}

func dominantSkipReason(counts map[string]int) string {
	best := ""
	bestCount := 0
	for reason, count := range counts {
		if count > bestCount {
			best, bestCount = reason, count
		}
	}
	return best
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
