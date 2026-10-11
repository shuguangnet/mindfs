package piexport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMungeDirName(t *testing.T) {
	cases := map[string]string{
		"/root/mindfs":                "--root-mindfs--",
		"/tmp":                        "--tmp--",
		"/root/.mindfs/scratch/probe": "--root-.mindfs-scratch-probe--",
		`C:\Users\dev\project`:        "--C--Users-dev-project--",
		"/root/AiBackup":              "--root-AiBackup--",
		"/root/herdr-web":             "--root-herdr-web--",
	}
	for in, want := range cases {
		if got := MungeDirName(in); got != want {
			t.Errorf("MungeDirName(%q) = %q, want %q", in, got, want)
		}
	}
}

func sampleSession() Session {
	return Session{
		Cwd:       "/root/mindfs",
		SessionID: "01a0de85-a7b7-72df-be0c-4a25ed39cb07",
		CreatedAt: time.Date(2026, 9, 26, 14, 39, 9, 0, time.UTC),
		Exchanges: []Exchange{
			{Role: "user", Content: "帮我看下这个报错", Timestamp: time.Date(2026, 9, 26, 14, 39, 10, 0, time.UTC)},
			{Role: "assistant", Content: "这是配置问题，修改如下…", Timestamp: time.Date(2026, 9, 26, 14, 40, 0, 0, time.UTC)},
			{Role: "system", Content: "ignored", Timestamp: time.Now()}, // filtered
			{Role: "user", Content: "", Timestamp: time.Now()},          // filtered
		},
	}
}

type rawEntry struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	ParentID  *string `json:"parentId"`
	Timestamp string  `json:"timestamp"`
	Message   struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

func TestGenerateStructure(t *testing.T) {
	payload, id, err := Generate(sampleSession())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if id != "01a0de85-a7b7-72df-be0c-4a25ed39cb07" {
		t.Fatalf("id = %q, want binding id preserved", id)
	}
	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	if len(lines) != 3 { // header + 2 messages
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	var header map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("header json: %v", err)
	}
	if header["type"] != "session" || header["version"].(float64) != 3 {
		t.Fatalf("bad header: %v", header)
	}
	if header["cwd"] != "/root/mindfs" {
		t.Fatalf("cwd = %v", header["cwd"])
	}

	var prevParent *string
	var prevID string
	for i, raw := range lines[1:] {
		var entry rawEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			t.Fatalf("entry %d json: %v", i, err)
		}
		if entry.Type != "message" {
			t.Fatalf("entry %d type = %q", i, entry.Type)
		}
		if i == 0 && entry.ParentID != nil {
			t.Fatalf("first message parentId should be null")
		}
		if i > 0 {
			if entry.ParentID == nil || *entry.ParentID != prevID {
				t.Fatalf("entry %d parentId chain broken", i)
			}
		}
		if len(entry.ID) != 8 {
			t.Fatalf("entry id %q not 8 chars", entry.ID)
		}
		wantRole := "user"
		if i == 1 {
			wantRole = "assistant"
		}
		if entry.Message.Role != wantRole {
			t.Fatalf("entry %d role = %q", i, entry.Message.Role)
		}
		if len(entry.Message.Content) != 1 || entry.Message.Content[0].Type != "text" {
			t.Fatalf("entry %d content shape unexpected", i)
		}
		prevID = entry.ID
		prevParent = entry.ParentID
	}
	_ = prevParent
}

func TestGenerateRequiresContent(t *testing.T) {
	if _, _, err := Generate(Session{Cwd: "/tmp", Exchanges: nil}); err == nil {
		t.Fatal("expected error for empty session")
	}
	if _, _, err := Generate(Session{Cwd: "", Exchanges: []Exchange{{Role: "user", Content: "hi"}}}); err == nil {
		t.Fatal("expected error for missing cwd")
	}
	if _, _, err := Generate(Session{Cwd: "/tmp", SessionID: "bad id!", Exchanges: []Exchange{{Role: "user", Content: "hi"}}}); err == nil {
		t.Fatal("expected error for invalid session id")
	}
}

func TestNewUUIDv7Shape(t *testing.T) {
	id := NewUUIDv7()
	if len(id) != 36 || id[14] != '7' || (id[19] != '8' && id[19] != '9' && id[19] != 'a' && id[19] != 'b') {
		t.Fatalf("uuidv7 shape invalid: %q", id)
	}
	a, b := NewUUIDv7(), NewUUIDv7()
	if a == b {
		t.Fatalf("ids not unique")
	}
}

func TestWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(sessionDirEnv, filepath.Join(dir, "sessions"))

	res, err := Write(sampleSession())
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if res.Existed {
		t.Fatal("first write should not report existed")
	}
	info, err := os.Stat(res.Path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", info.Mode().Perm())
	}
	if filepath.Base(res.Dir) != "--root-mindfs--" {
		t.Fatalf("dir = %q", res.Dir)
	}
	if !strings.HasSuffix(res.Path, "_01a0de85-a7b7-72df-be0c-4a25ed39cb07.jsonl") {
		t.Fatalf("file name = %q", res.Path)
	}

	// Idempotent rewrite.
	res2, err := Write(sampleSession())
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !res2.Existed || res2.Path != res.Path {
		t.Fatalf("rewrite should be idempotent: %+v vs %+v", res2, res)
	}

	// FindExisting locates it.
	if path, ok := FindExisting("/root/mindfs", "01a0de85-a7b7-72df-be0c-4a25ed39cb07"); !ok || path != res.Path {
		t.Fatalf("FindExisting = %q %v", path, ok)
	}
}
