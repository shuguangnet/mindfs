package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"mindfs/server/internal/backupbundle"
	"mindfs/server/internal/fs"
)

type fakeRegistry struct {
	roots   []fs.RootInfo
	upserts []string
}

func (r *fakeRegistry) ListRoots() []fs.RootInfo { return r.roots }

func (r *fakeRegistry) UpsertWithMetaLocation(root, metaLocation string) (fs.RootInfo, error) {
	r.upserts = append(r.upserts, root)
	for i, existing := range r.roots {
		if strings.EqualFold(filepath.Clean(existing.RootPath), filepath.Clean(root)) {
			updated := existing
			updated.RootPath = filepath.Clean(root)
			updated.MetaLocation = metaLocation
			r.roots[i] = updated
			return updated, nil
		}
	}
	info := fs.NewRootInfo(filepath.Base(root), filepath.Base(root), filepath.Clean(root))
	info.MetaLocation = metaLocation
	r.roots = append(r.roots, info)
	return info, nil
}

// buildSourceInstance creates a config dir + one project root with sessions,
// tasks, uploads, and a real session-list.db.
func buildSourceInstance(t *testing.T) (configDir, projectDir string, reg *fakeRegistry) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("HOME", base)
	configDir = filepath.Join(base, "config")
	projectDir = filepath.Join(base, "proj")
	homeMeta := filepath.Join(base, ".mindfs")

	for _, dir := range []string{
		filepath.Join(projectDir, ".mindfs", "sessions"),
		filepath.Join(projectDir, ".mindfs", "tasks"),
		filepath.Join(projectDir, ".mindfs", "upload"),
		filepath.Join(homeMeta, "other-root", "sessions"),
		filepath.Join(base, "elsewhere"),
		configDir,
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(configDir, "preferences.json"), `{"theme":"dark"}`)
	write(filepath.Join(configDir, "device.json"), `{"device":"secret-identity"}`)
	write(filepath.Join(configDir, "ssh-servers.json"), `{"servers":[]}`)
	write(filepath.Join(configDir, "old.json.bak-20260101"), `stale`)
	write(filepath.Join(projectDir, ".mindfs", "sessions", "1000-aaaa.jsonl"), `{"seq":1,"role":"user","content":"hi"}`)
	write(filepath.Join(projectDir, ".mindfs", "sessions", "1000-aaaa.aux.jsonl"), `{"seq":1}`)
	write(filepath.Join(projectDir, ".mindfs", "file-meta.json"), `{"files":{}}`)
	write(filepath.Join(projectDir, ".mindfs", "upload", "pic.png"), "png")
	write(filepath.Join(projectDir, ".mindfs", "tasks", "task-kanban.db"), "not a real db but not .db-checkable? actually .db suffix triggers check")

	// A real sqlite db with the session schema.
	dbPath := filepath.Join(projectDir, ".mindfs", "sessions", "session-list.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (
		key TEXT PRIMARY KEY, type TEXT NOT NULL DEFAULT '', parent_session_key TEXT NOT NULL DEFAULT '',
		parent_tool_call_id TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '', task_id TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '', agent TEXT NOT NULL DEFAULT '', mode TEXT NOT NULL DEFAULT '', effort TEXT NOT NULL DEFAULT '',
		fast_service TEXT NOT NULL DEFAULT '', shell TEXT NOT NULL DEFAULT '', plan_mode TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL DEFAULT '', related_files_json TEXT NOT NULL DEFAULT '', related_worktree_json TEXT NOT NULL DEFAULT '',
		last_context_window_total_tokens INTEGER NOT NULL DEFAULT 0, last_context_window_model_context_window INTEGER NOT NULL DEFAULT 0,
		pinned_at TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '', closed_at TEXT NOT NULL DEFAULT ''
	); CREATE TABLE session_agent_bindings (
		session_key TEXT NOT NULL, agent TEXT NOT NULL, agent_session_id TEXT NOT NULL DEFAULT '',
		agent_ctx_seq INTEGER NOT NULL DEFAULT 0, external_source_path TEXT NOT NULL DEFAULT '',
		external_source_offset INTEGER NOT NULL DEFAULT 0, external_source_mtime_ns INTEGER NOT NULL DEFAULT 0
	); INSERT INTO sessions (key, name, agent) VALUES ('1000-aaaa', 'first', 'pi');
		INSERT INTO session_agent_bindings (session_key, agent, agent_session_id) VALUES ('1000-aaaa', 'pi', 'native-1');`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	// A second root whose metadata lives under the home meta dir.
	write(filepath.Join(homeMeta, "other-root", "sessions", "2000-bbbb.jsonl"), `{"seq":1}`)

	reg = &fakeRegistry{roots: []fs.RootInfo{
		{ID: "proj", Name: "proj", RootPath: projectDir},
		{ID: "other-root", Name: "other", RootPath: filepath.Join(base, "elsewhere"), MetaLocation: fs.MetaLocationHome},
	}}
	return configDir, projectDir, reg
}

func newTestManager(t *testing.T, reg *fakeRegistry, configDir string) *Manager {
	t.Helper()
	return NewManager(reg, Options{
		ConfigDir:     configDir,
		MindfsVersion: "vtest",
		StagingBase:   t.TempDir(),
		Now:           func() time.Time { return time.Date(2026, 9, 26, 18, 0, 0, 0, time.UTC) },
	})
}

func TestExportImportRoundTripMerge(t *testing.T) {
	configDir, _, reg := buildSourceInstance(t)
	m := newTestManager(t, reg, configDir)

	bundle, err := m.Export(ExportRequest{Passphrase: "round-trip-pass", IncludeUploads: true, IncludeDeviceIdentity: true})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	defer bundle.Cleanup()
	raw, err := io.ReadAll(bundle.File)
	if err != nil {
		t.Fatal(err)
	}

	// Manifest sanity.
	if len(bundle.Manifest.Roots) != 2 {
		t.Fatalf("manifest roots = %+v", bundle.Manifest.Roots)
	}
	if bundle.Manifest.Global.FileCount < 2 {
		t.Fatalf("global files = %+v", bundle.Manifest.Global)
	}

	// Fresh instance: new config dir, new registry, same project path.
	targetConfig := filepath.Join(t.TempDir(), "config")
	targetReg := &fakeRegistry{}
	m2 := NewManager(targetReg, Options{ConfigDir: targetConfig, MindfsVersion: "vtest", StagingBase: t.TempDir()})

	preview, err := m2.Import(bytes.NewReader(raw), "round-trip-pass")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(preview.Roots) != 2 {
		t.Fatalf("preview roots = %+v", preview.Roots)
	}
	var projRow PreviewRoot
	for _, row := range preview.Roots {
		if row.ID == "proj" {
			projRow = row
		}
	}
	if projRow.Status != statusNew {
		t.Fatalf("proj status = %q", projRow.Status)
	}

	// Apply: merge proj root (path exists since buildSourceInstance made it),
	// replace global with device identity.
	actions := make([]ApplyRoot, 0, len(preview.Roots))
	for _, row := range preview.Roots {
		actions = append(actions, ApplyRoot{ID: row.ID, Action: actionMerge})
	}
	result, err := m2.Apply(ApplyRequest{Token: preview.Token, Roots: actions, Global: actionReplace, IncludeDeviceIdentity: true})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !result.ReloadNeeded || result.GlobalFilesReplaced < 2 {
		t.Fatalf("apply result = %+v", result)
	}

	// Sessions merged into the target db.
	mergedDB := filepath.Join(reg.roots[0].RootPath, ".mindfs", "sessions", "session-list.db")
	db, err := sql.Open("sqlite", "file:"+mergedDB+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n); err != nil || n != 1 {
		db.Close()
		t.Fatalf("sessions after merge = %d err=%v", n, err)
	}
	var binding string
	if err := db.QueryRow("SELECT agent_session_id FROM session_agent_bindings WHERE session_key='1000-aaaa'").Scan(&binding); err != nil || binding != "native-1" {
		db.Close()
		t.Fatalf("binding = %q err=%v", binding, err)
	}
	db.Close()

	// Global restored with device identity and preferences.
	prefs, err := os.ReadFile(filepath.Join(targetConfig, "preferences.json"))
	if err != nil || !strings.Contains(string(prefs), "dark") {
		t.Fatalf("preferences not restored: %v %s", err, prefs)
	}
	if _, err := os.Stat(filepath.Join(targetConfig, "device.json")); err != nil {
		t.Fatalf("device identity not restored: %v", err)
	}

	// Token is single-use.
	if _, err := m2.Apply(ApplyRequest{Token: preview.Token, Global: actionSkip}); err == nil {
		t.Fatal("apply should fail after use")
	}
}

func TestExportExcludesDeviceIdentityWhenDisabled(t *testing.T) {
	configDir, _, reg := buildSourceInstance(t)
	m := newTestManager(t, reg, configDir)
	bundle, err := m.Export(ExportRequest{Passphrase: "round-trip-pass", IncludeDeviceIdentity: false})
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Cleanup()
	payload := decryptToTar(t, bundle.File.Name(), "round-trip-pass")
	if strings.Contains(payload, "global/device.json") {
		t.Fatal("device.json should be excluded")
	}
	if !strings.Contains(payload, "global/preferences.json") {
		t.Fatal("preferences.json should be included")
	}
}

func TestWrongPassphraseRejected(t *testing.T) {
	configDir, _, reg := buildSourceInstance(t)
	m := newTestManager(t, reg, configDir)
	bundle, err := m.Export(ExportRequest{Passphrase: "round-trip-pass"})
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Cleanup()
	raw, _ := os.ReadFile(bundle.File.Name())
	m2 := newTestManager(t, reg, configDir)
	if _, err := m2.Import(bytes.NewReader(raw), "wrong-passphrase"); err == nil {
		t.Fatal("expected passphrase error")
	}
}

func decryptToTar(t *testing.T, path, passphrase string) string {
	t.Helper()
	raw, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	rc, err := backupbundle.Open(raw, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	gz, err := gzip.NewReader(rc)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}
	return strings.Join(names, "\n")
}

func TestPathOverrideRegistersNewLocation(t *testing.T) {
	configDir, _, reg := buildSourceInstance(t)
	m := newTestManager(t, reg, configDir)
	bundle, err := m.Export(ExportRequest{Passphrase: "round-trip-pass"})
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Cleanup()
	raw, _ := os.ReadFile(bundle.File.Name())

	// Import on a machine where the project moved elsewhere.
	movedDir := t.TempDir()
	targetReg := &fakeRegistry{} // empty
	m2 := NewManager(targetReg, Options{ConfigDir: filepath.Join(t.TempDir(), "config"), MindfsVersion: "vtest", StagingBase: t.TempDir()})
	preview, err := m2.Import(bytes.NewReader(raw), "round-trip-pass")
	if err != nil {
		t.Fatal(err)
	}
	var actions []ApplyRoot
	for _, row := range preview.Roots {
		if row.ID == "proj" {
			actions = append(actions, ApplyRoot{ID: row.ID, Action: actionReplace, PathOverride: movedDir})
		}
	}
	result, err := m2.Apply(ApplyRequest{Token: preview.Token, Roots: actions, Global: actionSkip})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(targetReg.upserts) == 0 {
		t.Fatal("path_override should register the root")
	}
	if !dirExists(filepath.Join(movedDir, ".mindfs", "sessions")) {
		t.Fatal("metadata should land under the overridden path")
	}
	_ = result
}

func TestExpiredTokenRejected(t *testing.T) {
	configDir, _, reg := buildSourceInstance(t)
	m := newTestManager(t, reg, configDir)
	bundle, err := m.Export(ExportRequest{Passphrase: "round-trip-pass"})
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Cleanup()
	raw, _ := os.ReadFile(bundle.File.Name())

	preview, err := m.Import(bytes.NewReader(raw), "round-trip-pass")
	if err != nil {
		t.Fatal(err)
	}
	// Fast-forward the clock.
	m.now = func() time.Time { return time.Date(2026, 9, 26, 19, 0, 0, 0, time.UTC) }
	if _, err := m.Apply(ApplyRequest{Token: preview.Token, Global: actionSkip}); err == nil {
		t.Fatal("expired token should fail")
	}
}
