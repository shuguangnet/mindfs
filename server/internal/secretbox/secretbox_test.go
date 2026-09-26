package secretbox

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func newTestKey(t *testing.T) *MasterKey {
	t.Helper()
	path := filepath.Join(t.TempDir(), "k.key")
	m, err := LoadOrGenerate(path)
	if err != nil {
		t.Fatalf("LoadOrGenerate: %v", err)
	}
	return m
}

func TestSealOpenRoundTrip(t *testing.T) {
	m := newTestKey(t)
	env, err := m.Seal("rec-1", []byte("hunter2"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := m.Open("rec-1", env)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, []byte("hunter2")) {
		t.Fatalf("got %q", got)
	}
}

func TestOpenWrongAAD(t *testing.T) {
	m := newTestKey(t)
	env, _ := m.Seal("rec-1", []byte("secret"))
	if _, err := m.Open("rec-2", env); err == nil {
		t.Fatal("expected AAD mismatch error")
	}
}

func TestOpenWithDifferentKey(t *testing.T) {
	a := newTestKey(t)
	b := newTestKey(t)
	env, _ := a.Seal("rec-1", []byte("secret"))
	if _, err := b.Open("rec-1", env); err == nil {
		t.Fatal("expected wrong-key error")
	}
}

func TestWipe(t *testing.T) {
	b := []byte{1, 2, 3}
	Wipe(b)
	for _, v := range b {
		if v != 0 {
			t.Fatal("wipe left bytes")
		}
	}
}

func TestKeyFilePermissionsEnforced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k.key")
	if _, err := LoadOrGenerate(path); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrGenerate(path); err == nil {
		t.Fatal("expected insecure permission error")
	}
}

func TestKeyFileWrongSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k.key")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrGenerate(path); err == nil {
		t.Fatal("expected size error")
	}
}

func TestPassphraseRoundTrip(t *testing.T) {
	env, err := SealWithPassphrase("open sesame", "mindfs-export", []byte("payload"))
	if err != nil {
		t.Fatalf("SealWithPassphrase: %v", err)
	}
	got, err := env.Open("open sesame", "mindfs-export")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("got %q", got)
	}
	if _, err := env.Open("wrong", "mindfs-export"); err == nil {
		t.Fatal("expected wrong passphrase error")
	}
	if _, err := env.Open("open sesame", "other-aad"); err == nil {
		t.Fatal("expected aad error")
	}
}
