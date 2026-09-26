package sshops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSSHBinaryResolvesManagedAlias verifies that the real OpenSSH client
// resolves an alias materialized by MindFS (the `ssh <alias>` guarantee).
// ssh expands ~ via the passwd database rather than $HOME, so the generated
// user config is passed explicitly with -F (the Include inside uses an
// absolute path and therefore resolves identically without -F).
func TestSSHBinaryResolvesManagedAlias(t *testing.T) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh binary not available")
	}
	mgr, dir := newTestManager(t)
	keyPath := filepath.Join(dir, "id_test")
	if err := os.WriteFile(keyPath, []byte(testPrivateKey), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Save(SaveInput{Server: Server{
		Alias: "crunchbits", Host: "203.0.113.10", Port: 2222, User: "deploy",
		Auth: AuthKeyPath, KeyPath: keyPath, Enabled: true,
	}}); err != nil {
		t.Fatalf("save: %v", err)
	}

	t.Setenv("HOME", filepath.Join(dir, "home"))
	userConfig := filepath.Join(dir, "home", ".ssh", "config")
	out, err := exec.Command(sshPath, "-F", userConfig, "-G", "crunchbits").CombinedOutput()
	if err != nil {
		t.Fatalf("ssh -G: %v\n%s", err, out)
	}
	text := strings.ToLower(string(out))
	for _, want := range []string{
		"user deploy",
		"hostname 203.0.113.10",
		"port 2222",
		"identityfile " + strings.ToLower(keyPath),
		"identitiesonly yes",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ssh -G output missing %q:\n%s", want, out)
		}
	}
}

// TestSSHBinaryResolvesPasswordAlias: password-mode aliases still resolve
// (interactive password prompt happens at connect time, not config time).
func TestSSHBinaryResolvesPasswordAlias(t *testing.T) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh binary not available")
	}
	mgr, dir := newTestManager(t)
	if _, err := mgr.Save(passwordServer("web-1", "10.0.0.7")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(dir, "home"))
	userConfig := filepath.Join(dir, "home", ".ssh", "config")
	out, err := exec.Command(sshPath, "-F", userConfig, "-G", "web-1").CombinedOutput()
	if err != nil {
		t.Fatalf("ssh -G: %v\n%s", err, out)
	}
	text := strings.ToLower(string(out))
	if !strings.Contains(text, "hostname 10.0.0.7") || !strings.Contains(text, "user root") {
		t.Fatalf("ssh -G output wrong:\n%s", out)
	}
}
