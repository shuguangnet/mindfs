package remote

import (
	"path/filepath"
	"testing"
)

func TestStoreSaveRetainsExistingSecretWhenBlank(t *testing.T) {
	store := NewStoreAt(filepath.Join(t.TempDir(), "remote-servers.json"))
	saved, err := store.Save(Server{
		ID:            "Prod_A",
		Name:          "Prod",
		BaseURL:       "https://example.com/",
		NodeID:        "node-1",
		PairingSecret: "secret-1",
		DefaultRootID: "root",
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if saved.ID != "prod_a" {
		t.Fatalf("ID = %q, want prod_a", saved.ID)
	}

	if _, err := store.Save(Server{
		ID:            "prod_a",
		Name:          "Prod 2",
		BaseURL:       "https://example.com/base/",
		NodeID:        "node-2",
		DefaultRootID: "root-2",
		Enabled:       true,
	}); err != nil {
		t.Fatalf("Save(blank secret) error = %v", err)
	}
	got, ok, err := store.Get("prod_a")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() not found")
	}
	if got.PairingSecret != "secret-1" {
		t.Fatalf("PairingSecret = %q, want retained secret", got.PairingSecret)
	}
	if got.BaseURL != "https://example.com/base" {
		t.Fatalf("BaseURL = %q, want trimmed base URL", got.BaseURL)
	}
}

func TestPublicRedactsSecret(t *testing.T) {
	got := Public(Server{
		ID:            "prod",
		Name:          "Prod",
		BaseURL:       "https://example.com",
		NodeID:        "node",
		PairingSecret: "secret",
		DefaultRootID: "root",
		Enabled:       true,
	})
	if !got.HasSecret {
		t.Fatal("HasSecret = false, want true")
	}
}

func TestRemoteNameEncoding(t *testing.T) {
	encoded := EncodeAgentName("Prod_A", "codex")
	serverID, name, ok := ParseName(encoded)
	if !ok {
		t.Fatal("ParseName() ok = false")
	}
	if serverID != "prod_a" || name != "codex" {
		t.Fatalf("ParseName() = %q %q, want prod_a codex", serverID, name)
	}
}
