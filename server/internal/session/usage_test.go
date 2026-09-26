package session

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
)

func writeExchangeLog(t *testing.T, manager *Manager, key string, exchanges []Exchange) {
	t.Helper()
	path := manager.ExchangeLogAbsolutePath(key)
	if path == "" {
		t.Fatalf("no exchange path for %s", key)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, exchange := range exchanges {
		if err := encoder.Encode(exchange); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}
}

func TestScanUsageCollectsOnlyAgentTurnsWithUsage(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := NewManager(root)
	ctx := context.Background()
	created, err := manager.Create(ctx, CreateInput{Type: TypeChat, Name: "usage"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	base := time.Date(2026, time.September, 20, 10, 0, 0, 0, time.UTC)
	cacheRead := 500
	writeExchangeLog(t, manager, created.Key, []Exchange{
		{Role: "user", Content: "question", Timestamp: base},
		{Role: "agent", Agent: "codex", Model: "gpt-5.6-sol", Timestamp: base.Add(time.Minute),
			TokenUsage: &agenttypes.TokenUsage{InputTokens: 1000, OutputTokens: 100, CacheReadTokens: &cacheRead}},
		// Agent turn without usage telemetry must be skipped.
		{Role: "agent", Agent: "codex", Model: "gpt-5.6-sol", Timestamp: base.Add(2 * time.Minute)},
		// A user turn that somehow carries usage must not be billed as an agent turn.
		{Role: "user", Content: "next", Timestamp: base.Add(3 * time.Minute),
			TokenUsage: &agenttypes.TokenUsage{InputTokens: 5, OutputTokens: 5}},
	})

	result, err := manager.ScanUsage(ctx, "mindfs", UsageScanOptions{})
	if err != nil {
		t.Fatalf("ScanUsage: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records = %d, want 1 (%#v)", len(result.Records), result.Records)
	}
	record := result.Records[0]
	if record.RootID != "mindfs" || record.SessionKey != created.Key || record.Agent != "codex" {
		t.Fatalf("record = %#v", record)
	}
	if record.Usage.InputTokens != 1000 || record.Usage.OutputTokens != 100 {
		t.Fatalf("usage = %#v", record.Usage)
	}
	if record.SessionName != "usage" {
		t.Fatalf("SessionName = %q, want usage", record.SessionName)
	}
	if result.SkippedFiles != 0 {
		t.Fatalf("unexpected skipped files: %#v", result)
	}
}

func TestScanUsageFiltersByTimeWindow(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := NewManager(root)
	ctx := context.Background()
	created, err := manager.Create(ctx, CreateInput{Type: TypeChat, Name: "window"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC)
	writeExchangeLog(t, manager, created.Key, []Exchange{
		{Role: "agent", Agent: "codex", Timestamp: now.Add(-48 * time.Hour), TokenUsage: &agenttypes.TokenUsage{InputTokens: 1}},
		{Role: "agent", Agent: "codex", Timestamp: now, TokenUsage: &agenttypes.TokenUsage{InputTokens: 2}},
		{Role: "agent", Agent: "codex", Timestamp: now.Add(48 * time.Hour), TokenUsage: &agenttypes.TokenUsage{InputTokens: 3}},
	})

	result, err := manager.ScanUsage(ctx, "mindfs", UsageScanOptions{
		After:  now.Add(-time.Hour),
		Before: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("ScanUsage: %v", err)
	}
	if len(result.Records) != 1 || result.Records[0].Usage.InputTokens != 2 {
		t.Fatalf("records = %#v", result.Records)
	}
}

func TestScanUsageFallsBackToSessionAgentAndModel(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := NewManager(root)
	ctx := context.Background()
	created, err := manager.Create(ctx, CreateInput{Type: TypeChat, Name: "fallback"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := manager.UpdateRuntimeConfig(ctx, created.Key, RuntimeConfigPatch{
		Agent: stringPtr("pi"),
		Model: stringPtr("deepseek-v4.1-flash"),
	}); err != nil {
		t.Fatalf("UpdateRuntimeConfig: %v", err)
	}
	writeExchangeLog(t, manager, created.Key, []Exchange{
		{Role: "agent", Timestamp: time.Now().UTC(), TokenUsage: &agenttypes.TokenUsage{InputTokens: 7}},
	})

	result, err := manager.ScanUsage(ctx, "mindfs", UsageScanOptions{})
	if err != nil {
		t.Fatalf("ScanUsage: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(result.Records))
	}
	if result.Records[0].Agent != "pi" || result.Records[0].Model != "deepseek-v4.1-flash" {
		t.Fatalf("fallback record = %#v", result.Records[0])
	}
}

// A malformed line must not abort the scan or lose the valid lines around it.
func TestScanUsageToleratesMalformedLines(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := NewManager(root)
	ctx := context.Background()
	created, err := manager.Create(ctx, CreateInput{Type: TypeChat, Name: "malformed"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := manager.ExchangeLogAbsolutePath(created.Key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	valid, err := json.Marshal(Exchange{
		Role: "agent", Agent: "codex", Timestamp: time.Now().UTC(),
		TokenUsage: &agenttypes.TokenUsage{InputTokens: 42},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	content := strings.Join([]string{
		"{ this is not json",
		"",
		`{"role":"agent","token_usage":{"inputTokens":1},"timestamp":"0001-01-01T00:00:00Z"}`,
		string(valid),
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := manager.ScanUsage(ctx, "mindfs", UsageScanOptions{})
	if err != nil {
		t.Fatalf("ScanUsage: %v", err)
	}
	if len(result.Records) != 1 || result.Records[0].Usage.InputTokens != 42 {
		t.Fatalf("records = %#v", result.Records)
	}
}

func TestScanUsageSkipsCommandSessions(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := NewManager(root)
	ctx := context.Background()
	created, err := manager.Create(ctx, CreateInput{Type: TypeCommand, Name: "shell"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	writeExchangeLog(t, manager, created.Key, []Exchange{
		{Role: "agent", Timestamp: time.Now().UTC(), TokenUsage: &agenttypes.TokenUsage{InputTokens: 99}},
	})

	result, err := manager.ScanUsage(ctx, "mindfs", UsageScanOptions{})
	if err != nil {
		t.Fatalf("ScanUsage: %v", err)
	}
	if len(result.Records) != 0 {
		t.Fatalf("command session usage was billed: %#v", result.Records)
	}
}

func stringPtr(value string) *string { return &value }
