package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mindfs/server/internal/backup"
	"mindfs/server/internal/fs"
	"mindfs/server/internal/session"
)

func newBackupTestHandler(t *testing.T) (*HTTPHandler, *backup.Manager, string, string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("HOME", base)
	configDir := filepath.Join(base, "config")
	projectDir := filepath.Join(base, "proj")
	for _, dir := range []string{configDir, filepath.Join(projectDir, ".mindfs", "sessions")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(configDir, "preferences.json"), []byte(`{"theme":"light"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".mindfs", "sessions", "1-aa.jsonl"), []byte("{\"seq\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := fs.NewRegistry(filepath.Join(configDir, "registry.json"))
	if _, err := registry.Upsert(projectDir); err != nil {
		t.Fatal(err)
	}
	manager := backup.NewManager(backup.AdaptRegistry(registry), backup.Options{
		ConfigDir:     configDir,
		MindfsVersion: "vtest",
		StagingBase:   t.TempDir(),
		Now:           func() time.Time { return time.Date(2026, 9, 26, 18, 0, 0, 0, time.UTC) },
	})
	handler := &HTTPHandler{AppContext: &AppContext{Dirs: registry, Backup: manager}}
	return handler, manager, configDir, projectDir
}

func doBackupJSON(t *testing.T, handler *HTTPHandler, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	return doSSHRequest(t, handler, method, path, body)
}

func TestBackupExportImportApplyAPI(t *testing.T) {
	handler, _, configDir, _ := newBackupTestHandler(t)

	// Export.
	rec, body := doBackupJSON(t, handler, http.MethodPost, "/api/backup/export", map[string]any{
		"passphrase": "api-round-trip", "include_uploads": true, "include_device_identity": true,
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "mindfs-backup-") {
		t.Fatalf("export: %d %v %s", rec.Code, body, rec.Header().Get("Content-Disposition"))
	}
	bundle := rec.Body.Bytes()

	// Import (multipart).
	req := httptest.NewRequest(http.MethodPost, "/api/backup/import", nil)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "bundle.mbk")
	fw.Write(bundle)
	mw.WriteField("passphrase", "api-round-trip")
	mw.Close()
	req = httptest.NewRequest(http.MethodPost, "/api/backup/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec = httptest.NewRecorder()
	handler.protectedEndpoint(passthroughHandler(handler, http.MethodPost, "/api/backup/import"))(rec, req)
	var preview backup.Preview
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &preview) != nil {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	if len(preview.Roots) != 1 || preview.Token == "" {
		t.Fatalf("preview: %+v", preview)
	}

	// Wrong passphrase.
	bad := &bytes.Buffer{}
	mw2 := multipart.NewWriter(bad)
	fw2, _ := mw2.CreateFormFile("file", "bundle.mbk")
	fw2.Write(bundle)
	mw2.WriteField("passphrase", "wrong-passphrase")
	mw2.Close()
	req = httptest.NewRequest(http.MethodPost, "/api/backup/import", bad)
	req.Header.Set("Content-Type", mw2.FormDataContentType())
	rec = httptest.NewRecorder()
	handler.protectedEndpoint(passthroughHandler(handler, http.MethodPost, "/api/backup/import"))(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong passphrase: %d %s", rec.Code, rec.Body.String())
	}

	// Apply: replace global.
	rec, body = doBackupJSON(t, handler, http.MethodPost, "/api/backup/apply", map[string]any{
		"token": preview.Token, "roots": []map[string]any{}, "global": "replace", "include_device_identity": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("apply: %d %v", rec.Code, body)
	}
	// preferences restored (same dir here, plus rollover backup created).
	entries, _ := os.ReadDir(configDir)
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "preferences.json.bak-") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected rollover backup in %v", entries)
	}
}

func TestBackupShortPassphraseAPI(t *testing.T) {
	handler, _, _, _ := newBackupTestHandler(t)
	rec, body := doBackupJSON(t, handler, http.MethodPost, "/api/backup/export", map[string]any{"passphrase": "short"})
	if rec.Code != http.StatusBadRequest || body["code"] != "passphrase_too_short" {
		t.Fatalf("short passphrase: %d %v", rec.Code, body)
	}
}

func TestSessionExportNativePi(t *testing.T) {
	handler, _, _, _ := newBackupTestHandler(t)
	app := handler.AppContext
	rootID := ""
	for _, root := range app.Dirs.List() {
		rootID = root.ID
	}
	manager, err := app.GetSessionManager(rootID)
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Create(context.Background(), session.CreateInput{Type: session.TypeChat, Agent: "pi", Name: "export me"})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.AddExchangeForAgentAt(context.Background(), created, "user", "帮我看看日志", "pi", "", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := manager.AddExchangeForAgentAt(context.Background(), created, "assistant", "看起来是端口占用", "pi", "", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}

	piDir := filepath.Join(t.TempDir(), "pisessions")
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", piDir)

	path := "/api/sessions/" + created.Key + "/export-native?root=" + rootID
	rec, body := doBackupJSON(t, handler, http.MethodPost, path, map[string]any{"mode": "write"})
	if rec.Code != http.StatusOK {
		t.Fatalf("export-native write: %d %v", rec.Code, body)
	}
	filePath := body["path"].(string)
	if !strings.HasPrefix(filePath, piDir) {
		t.Fatalf("path = %q, want under %q", filePath, piDir)
	}
	payload, err := os.ReadFile(filePath)
	if err != nil || !strings.Contains(string(payload), `"type":"message"`) {
		t.Fatalf("native file: %v %s", err, payload)
	}

	// Idempotent.
	rec, body = doBackupJSON(t, handler, http.MethodPost, path, map[string]any{"mode": "write"})
	if rec.Code != http.StatusOK || body["existed"] != true {
		t.Fatalf("idempotent: %d %v", rec.Code, body)
	}

	// Download mode.
	rec, _ = doBackupJSON(t, handler, http.MethodPost, path, map[string]any{"mode": "download"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "session") {
		t.Fatalf("download: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), ".jsonl") {
		t.Fatalf("disposition: %s", rec.Header().Get("Content-Disposition"))
	}

	// Unsupported agent.
	rec, body = doBackupJSON(t, handler, http.MethodPost, path, map[string]any{"agent": "codex"})
	if rec.Code != http.StatusBadRequest || body["code"] != "unsupported_agent" {
		t.Fatalf("unsupported agent: %d %v", rec.Code, body)
	}
}
