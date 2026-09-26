package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mindfs/server/internal/secretbox"
	"mindfs/server/internal/sshops"
)

func newSSHTestHandler(t *testing.T) (*HTTPHandler, *sshops.Manager) {
	t.Helper()
	dir := t.TempDir()
	box, err := secretbox.LoadOrGenerate(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatalf("master key: %v", err)
	}
	store := sshops.NewStoreAt(filepath.Join(dir, "ssh-servers.json"), box)
	if err := os.MkdirAll(filepath.Join(dir, "home"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager, err := sshops.NewManager(store, sshops.Options{
		HomeDir: filepath.Join(dir, "home"),
		KeysDir: filepath.Join(dir, "keys"),
	})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	handler := &HTTPHandler{AppContext: &AppContext{SSHOps: manager}}
	return handler, manager
}

func doSSHRequest(t *testing.T, handler *HTTPHandler, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		blob, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(blob)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.protectedEndpoint(passthroughHandler(handler, method, path))(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// passthroughHandler resolves the real route handler from the chi router by
// performing a lookup through the public Routes() mux.
func passthroughHandler(handler *HTTPHandler, method, path string) http.HandlerFunc {
	router := handler.Routes()
	return func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r)
	}
}

func TestSSHServerAPIWorkflow(t *testing.T) {
	handler, manager := newSSHTestHandler(t)

	// Empty list.
	rec, body := doSSHRequest(t, handler, http.MethodGet, "/api/ssh-servers", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status: %d body=%v", rec.Code, body)
	}

	// Create invalid server: alias with space.
	rec, body = doSSHRequest(t, handler, http.MethodPost, "/api/ssh-servers", sshops.SaveInput{
		Server: sshops.Server{Alias: "bad alias", Host: "10.0.0.1", User: "root", Auth: sshops.AuthPassword},
	})
	if rec.Code != http.StatusBadRequest || body["code"] != sshops.ErrCodeInvalidAlias {
		t.Fatalf("invalid alias: %d %v", rec.Code, body)
	}

	// Create valid server (closed local port for a fast dial failure).
	rec, body = doSSHRequest(t, handler, http.MethodPost, "/api/ssh-servers", sshops.SaveInput{
		Server:      sshops.Server{Alias: "crunchbits", Host: "127.0.0.1", Port: 1, User: "root", Auth: sshops.AuthPassword, Enabled: true},
		NewPassword: "hunter2",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %v", rec.Code, body)
	}
	serverBody, _ := body["server"].(map[string]any)
	if serverBody == nil || serverBody["alias"] != "crunchbits" || serverBody["has_password"] != true {
		t.Fatalf("save body: %v", body)
	}
	id, _ := serverBody["id"].(string)
	if id == "" {
		t.Fatal("missing id")
	}

	// List must not contain secrets.
	rec, body = doSSHRequest(t, handler, http.MethodGet, "/api/ssh-servers", nil)
	blob, _ := json.Marshal(body)
	if strings.Contains(string(blob), "hunter2") {
		t.Fatal("secret leaked in list")
	}
	reuse, _ := body["reuse"].(map[string]any)
	passwords, _ := reuse["passwords"].([]any)
	if len(passwords) != 1 {
		t.Fatalf("reuse passwords: %v", reuse)
	}

	// Duplicate alias conflict.
	rec, body = doSSHRequest(t, handler, http.MethodPost, "/api/ssh-servers", sshops.SaveInput{
		Server: sshops.Server{Alias: "crunchbits", Host: "10.0.0.9", User: "root", Auth: sshops.AuthPassword, Enabled: true},
	})
	if rec.Code != http.StatusConflict || body["code"] != sshops.ErrCodeAliasConflict {
		t.Fatalf("conflict: %d %v", rec.Code, body)
	}

	// Test dial against a closed port (classified failure, not error).
	rec, body = doSSHRequest(t, handler, http.MethodPost, "/api/ssh-servers/"+id+"/test", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("test: %d %v", rec.Code, body)
	}
	if body["ok"] != false {
		t.Fatalf("test body: %v", body)
	}

	// Export no-secrets.
	rec, body = doSSHRequest(t, handler, http.MethodPost, "/api/ssh-servers/export", map[string]string{"mode": "no-secrets"})
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %v", rec.Code, body)
	}
	if body["secrets"] != nil {
		t.Fatalf("no-secrets export should not carry secrets: %v", body["secrets"])
	}

	// Delete.
	rec, body = doSSHRequest(t, handler, http.MethodDelete, "/api/ssh-servers/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %v", rec.Code, body)
	}

	// 404 afterwards.
	rec, body = doSSHRequest(t, handler, http.MethodDelete, "/api/ssh-servers/"+id, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete: %d %v", rec.Code, body)
	}
	_ = manager
}

func TestSSHServerAPINilManager(t *testing.T) {
	handler := &HTTPHandler{AppContext: &AppContext{}}
	rec, body := doSSHRequest(t, handler, http.MethodGet, "/api/ssh-servers", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("nil manager list should return empty ok: %d %v", rec.Code, body)
	}
	servers, _ := body["servers"].([]any)
	if len(servers) != 0 {
		t.Fatalf("servers: %v", servers)
	}
	rec, _ = doSSHRequest(t, handler, http.MethodPost, "/api/ssh-servers", sshops.SaveInput{})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil manager save: %d", rec.Code)
	}
}

func TestSSHServerFSListingAPI(t *testing.T) {
	handler, manager := newSSHTestHandler(t)
	homeListing, err := manager.ListFS("")
	if err != nil {
		t.Fatal(err)
	}
	home := homeListing.Path
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec, body := doSSHRequest(t, handler, http.MethodGet, "/api/ssh-servers/fs?path=~/.ssh", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("fs list: %d %v", rec.Code, body)
	}
	entries, _ := body["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries: %v", entries)
	}
	entry := entries[0].(map[string]any)
	if entry["name"] != "id_ed25519" || entry["is_dir"] != false || entry["looks_like_key"] != true {
		t.Fatalf("entry: %v", entry)
	}
	if body["home"] != home {
		t.Fatalf("home: %v", body["home"])
	}

	rec, body = doSSHRequest(t, handler, http.MethodGet, "/api/ssh-servers/fs?path=/nonexistent-dir-xyz", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing dir should be 400: %d %v", rec.Code, body)
	}
}
