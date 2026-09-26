package sshops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mindfs/server/internal/secretbox"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "master.key")
	box, err := secretbox.LoadOrGenerate(keyPath)
	if err != nil {
		t.Fatalf("master key: %v", err)
	}
	store := NewStoreAt(filepath.Join(dir, "ssh-servers.json"), box)
	mgr, err := NewManager(store, Options{
		HomeDir: filepath.Join(dir, "home"),
		KeysDir: filepath.Join(dir, "keys"),
	})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	return mgr, dir
}

func passwordServer(alias, host string) SaveInput {
	return SaveInput{Server: Server{
		Alias: alias, Host: host, Port: 22, User: "root", Auth: AuthPassword, Enabled: true,
	}, NewPassword: "hunter2"}
}

func TestSaveStoresSecretsEncrypted(t *testing.T) {
	mgr, dir := newTestManager(t)
	res, err := mgr.Save(passwordServer("crunchbits", "203.0.113.10"))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if res.Server.Alias != "crunchbits" || !res.Server.HasPassword {
		t.Fatalf("unexpected public server: %+v", res.Server)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "ssh-servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2") {
		t.Fatal("plaintext password leaked into store file")
	}
	var file map[string]any
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
}

func TestListSanitizedAndReuse(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.Save(passwordServer("web-1", "10.0.0.1")); err != nil {
		t.Fatal(err)
	}
	in := SaveInput{Server: Server{
		Alias: "db", Host: "10.0.0.2", Port: 22, User: "admin", Auth: AuthKeyPath,
		KeyPath: "/home/me/.ssh/id_ed25519", Enabled: true,
	}}
	if _, err := mgr.Save(in); err != nil {
		t.Fatal(err)
	}
	list, err := mgr.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Servers) != 2 {
		t.Fatalf("want 2 servers, got %d", len(list.Servers))
	}
	for _, srv := range list.Servers {
		blob, _ := json.Marshal(srv)
		if strings.Contains(string(blob), "hunter2") {
			t.Fatal("password leaked in list response")
		}
	}
	if len(list.Reuse.KeyPaths) != 1 || list.Reuse.KeyPaths[0] != "/home/me/.ssh/id_ed25519" {
		t.Fatalf("key path reuse list wrong: %+v", list.Reuse.KeyPaths)
	}
	if len(list.Reuse.Passwords) != 1 || list.Reuse.Passwords[0].Alias != "web-1" {
		t.Fatalf("password reuse list wrong: %+v", list.Reuse.Passwords)
	}
	if list.Materialize == nil || !list.Materialize.Included {
		t.Fatalf("materialize info wrong: %+v", list.Materialize)
	}
}

func TestSaveAliasConflictAndNormalization(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.Save(passwordServer("web-1", "10.0.0.1")); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Save(passwordServer("WEB-1", "10.0.0.2")); err == nil {
		t.Fatal("expected alias conflict (case-insensitive)")
	}
	res, err := mgr.Save(passwordServer("CrunchBits", "10.0.0.3"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Server.Alias != "crunchbits" {
		t.Fatalf("alias not normalized: %q", res.Server.Alias)
	}
}

func TestSaveUpdateRequiresOrKeepsSecret(t *testing.T) {
	mgr, _ := newTestManager(t)
	res, _ := mgr.Save(passwordServer("web-1", "10.0.0.1"))

	// Update without touching secrets keeps stored password.
	update := SaveInput{Server: Server{
		ID: res.Server.ID, Alias: "web-1", Host: "10.0.0.1", Port: 2222,
		User: "root", Auth: AuthPassword, Enabled: true,
	}}
	updated, err := mgr.Save(update)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !updated.Server.HasPassword || updated.Server.Port != 2222 {
		t.Fatalf("update result wrong: %+v", updated.Server)
	}

	// Fresh password-mode create without secret fails.
	bad := SaveInput{Server: Server{Alias: "fresh", Host: "10.0.0.9", User: "r", Auth: AuthPassword, Enabled: true}}
	if _, err := mgr.Save(bad); err == nil {
		t.Fatal("expected secret required error")
	}
}

func TestReusePasswordFromOtherServer(t *testing.T) {
	mgr, _ := newTestManager(t)
	res, _ := mgr.Save(passwordServer("web-1", "10.0.0.1"))
	reuse := SaveInput{Server: Server{
		Alias: "web-2", Host: "10.0.0.2", Port: 22, User: "root", Auth: AuthPassword, Enabled: true,
	}, ReusePasswordFrom: res.Server.ID}
	out, err := mgr.Save(reuse)
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if !out.Server.HasPassword {
		t.Fatal("reused password not stored")
	}
}

func TestDelete(t *testing.T) {
	mgr, _ := newTestManager(t)
	res, _ := mgr.Save(passwordServer("gone", "10.0.0.1"))
	if err := mgr.Delete(res.Server.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := mgr.Delete(res.Server.ID); err == nil {
		t.Fatal("expected not found")
	}
	list, _ := mgr.List()
	if len(list.Servers) != 0 {
		t.Fatalf("server not removed: %+v", list.Servers)
	}
}

const testPrivateKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAABFwAAAAdzc2gtcn
NhAAAAAwEAAQAAAQEAtc0vLB3n6e5h8mJ7F8Wvh1c1X8IhnF0cFCHNSdfB4JDpTaAGMHMw
V/a4nsBLO4E9cUGvS5nYLCZiWRl5Hi7RBsiDt7MzSpLY5KcXa+mDNYmqfD1npGiyaRzsJZ
MwAAA8hWjOcrVoznKwAAAAdzc2gtcnNhAAABAQC1zS8sHefp7mHyYnsXxa+HVzVfwiGcXR
wUIc1J18HgkOlNoAYwczBX9riewEs7gT1xQa9LmdgsJmJZGXkeLtEGyIO3szNKktjkpxdr
6YM1iap8XWekaLJpHOwlkzAAAAAwEAAQAAAQBRwvJ5DG8CBcg3IhI30M2cZCgQYI1Lznzc
F4H6BdiwJBp6uUQVLfAAOaNGS2wTrWc3iOqkLDZJRIl1GWs0LUIcqFTISkUIiB6nw0PbLf
X4D5AAAAgQC8G4dSpI4wu4YKrU9aSGkt+8EOF9Pm4BNTZMCzG0FrrjOkCQrUJH6SXUDORL
AAAAgQDmTnHyLpGS9kzoDRlBBQnstQFBB1OTkiSDTjBPeOobiWA5vLPnnNpXTHguLLZrDj
BAAAIE+hUH0pnBFtbmiQsIQeBzDapQBNA+bunik1tQI+UMIBHIuJYY=
-----END OPENSSH PRIVATE KEY-----`

func TestInlineKeyMaterialization(t *testing.T) {
	mgr, dir := newTestManager(t)
	in := SaveInput{Server: Server{
		Alias: "keyed", Host: "10.0.0.5", Port: 22, User: "deploy", Auth: AuthKeyInline, Enabled: true,
	}, NewKeyContent: testPrivateKey}
	res, err := mgr.Save(in)
	if err != nil {
		t.Fatalf("save inline: %v", err)
	}
	if res.Server.KeySource != KeySourceManaged {
		t.Fatalf("key source: %q", res.Server.KeySource)
	}
	keyPath := filepath.Join(dir, "keys", res.Server.ID+".key")
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("managed key not materialized: %v", err)
	}
	if !strings.Contains(string(data), "OPENSSH PRIVATE KEY") {
		t.Fatal("managed key content wrong")
	}
	info, err := os.Stat(keyPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("managed key perms: %v %v", info, err)
	}

	conf, err := os.ReadFile(filepath.Join(dir, "home", ".ssh", "config.d", "mindfs-servers.conf"))
	if err != nil {
		t.Fatalf("managed conf missing: %v", err)
	}
	text := string(conf)
	if !strings.Contains(text, "Host keyed") || !strings.Contains(text, "HostName 10.0.0.5") || !strings.Contains(text, "IdentitiesOnly yes") {
		t.Fatalf("host block wrong:\n%s", text)
	}
	if !strings.Contains(text, filepath.Join(dir, "keys", res.Server.ID+".key")) {
		t.Fatalf("identity file not referenced:\n%s", text)
	}

	userConf, err := os.ReadFile(filepath.Join(dir, "home", ".ssh", "config"))
	if err != nil {
		t.Fatalf("user config missing: %v", err)
	}
	if !strings.Contains(string(userConf), "Include") || !strings.Contains(string(userConf), "mindfs-servers.conf") {
		t.Fatalf("include missing:\n%s", userConf)
	}

	// Delete removes the managed key file.
	if err := mgr.Delete(res.Server.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("managed key file not cleaned up")
	}
}

func TestMaterializeIdempotentInclude(t *testing.T) {
	mgr, dir := newTestManager(t)
	if _, err := mgr.Save(passwordServer("a", "10.0.0.1")); err != nil {
		t.Fatal(err)
	}
	userConfPath := filepath.Join(dir, "home", ".ssh", "config")
	managedConfPath := filepath.Join(dir, "home", ".ssh", "config.d", "mindfs-servers.conf")
	first, _ := os.ReadFile(managedConfPath)
	if !strings.Contains(string(first), "Host a") {
		t.Fatalf("first server missing from managed conf")
	}
	if _, err := mgr.Save(passwordServer("b", "10.0.0.2")); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(userConfPath)
	count := 0
	for _, line := range strings.Split(string(second), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Include ") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("include duplicated: %d\n%s", count, second)
	}
	managedSecond, _ := os.ReadFile(managedConfPath)
	if !strings.Contains(string(managedSecond), "Host b") {
		t.Fatalf("second server missing from managed conf after rewrite:\n%s", managedSecond)
	}
}
