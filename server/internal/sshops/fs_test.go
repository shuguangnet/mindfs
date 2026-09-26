package sshops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListFSDefaultsToHomeAndSorts(t *testing.T) {
	mgr, dir := newTestManager(t)
	home := filepath.Join(dir, "home")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".ssh/id_ed25519", ".ssh/id_ed25519.pub", ".ssh/known_hosts", "notes.txt", "server.pem", "deploy_key"} {
		if err := os.WriteFile(filepath.Join(home, filepath.FromSlash(name)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(home, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	listing, err := mgr.ListFS("")
	if err != nil {
		t.Fatalf("ListFS: %v", err)
	}
	if listing.Path != home {
		t.Fatalf("path = %q want %q", listing.Path, home)
	}
	if listing.Home != home {
		t.Fatalf("home = %q want %q", listing.Home, home)
	}
	if listing.Parent != filepath.Dir(home) {
		t.Fatalf("parent = %q", listing.Parent)
	}

	var names []string
	for _, entry := range listing.Entries {
		names = append(names, entry.Name)
		if entry.Path != filepath.Join(home, entry.Name) {
			t.Fatalf("entry %s path = %q", entry.Name, entry.Path)
		}
	}
	got := strings.Join(names, ",")
	want := ".ssh,docs,deploy_key,notes.txt,server.pem"
	if got != want {
		t.Fatalf("entries = %q want %q", got, want)
	}

	byName := map[string]FSEntry{}
	for _, entry := range listing.Entries {
		byName[entry.Name] = entry
	}
	if !byName[".ssh"].IsDir {
		t.Fatal(".ssh should be a dir")
	}
	if byName["notes.txt"].IsDir {
		t.Fatal("notes.txt should not be a dir")
	}
	if byName["notes.txt"].LooksLikeKey {
		t.Fatal("notes.txt should not look like a key")
	}
	for _, keyName := range []string{"deploy_key", "server.pem"} {
		if !byName[keyName].LooksLikeKey {
			t.Fatalf("%s should look like a key", keyName)
		}
	}
	if byName["notes.txt"].Size == 0 {
		t.Fatal("file size should be reported")
	}

	// Inside ~/.ssh the id_* files carry the key hint.
	sshListing, err := mgr.ListFS("~/.ssh")
	if err != nil {
		t.Fatalf("ListFS .ssh: %v", err)
	}
	sshNames := strings.Join(func() []string {
		out := make([]string, 0, len(sshListing.Entries))
		for _, entry := range sshListing.Entries {
			out = append(out, entry.Name)
		}
		return out
	}(), ",")
	if sshNames != "id_ed25519,id_ed25519.pub,known_hosts" {
		t.Fatalf(".ssh entries = %q", sshNames)
	}
	for _, entry := range sshListing.Entries {
		if entry.Name == "known_hosts" || entry.Name == "id_ed25519.pub" {
			if entry.LooksLikeKey {
				t.Fatalf("%s should not look like a private key", entry.Name)
			}
			continue
		}
		if !entry.LooksLikeKey {
			t.Fatalf("%s should look like a key", entry.Name)
		}
	}
}

func TestListFSTildeExpansionAndParent(t *testing.T) {
	mgr, dir := newTestManager(t)
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}

	listing, err := mgr.ListFS("~/.ssh")
	if err != nil {
		t.Fatalf("ListFS: %v", err)
	}
	if listing.Path != filepath.Join(home, ".ssh") {
		t.Fatalf("path = %q", listing.Path)
	}
	if listing.Parent != home {
		t.Fatalf("parent = %q want home %q", listing.Parent, home)
	}

	// "~" alone lists the home directory.
	rootListing, err := mgr.ListFS("~")
	if err != nil {
		t.Fatalf("ListFS ~: %v", err)
	}
	if rootListing.Path != home {
		t.Fatalf("~ path = %q want %q", rootListing.Path, home)
	}
}

func TestListFSErrorsOnUnreadablePath(t *testing.T) {
	mgr, dir := newTestManager(t)
	if _, err := mgr.ListFS(filepath.Join(dir, "missing-dir")); err == nil {
		t.Fatal("expected error for missing dir")
	} else if CodeOf(err) != ErrCodeBadPayload {
		t.Fatalf("code = %q want %q", CodeOf(err), ErrCodeBadPayload)
	}
	filePath := filepath.Join(dir, "some-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ListFS(filePath); err == nil {
		t.Fatal("expected error for file path")
	}
}

func TestListFSTruncatesHugeDirectories(t *testing.T) {
	mgr, dir := newTestManager(t)
	big := filepath.Join(dir, "big")
	if err := os.Mkdir(big, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < fsListLimit+50; i++ {
		name := string(rune('a'+i/26%26)) + string(rune('a'+i%26)) + string(rune('0'+i/676%10))
		if err := os.WriteFile(filepath.Join(big, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	listing, err := mgr.ListFS(big)
	if err != nil {
		t.Fatalf("ListFS: %v", err)
	}
	if len(listing.Entries) != fsListLimit {
		t.Fatalf("entries = %d want %d", len(listing.Entries), fsListLimit)
	}
	if !listing.Truncated {
		t.Fatal("truncated flag should be set")
	}
}
