package sshops

import (
	"encoding/json"
	"strings"
	"testing"
)

const sampleSSHConfig = `# my ssh config
Include /etc/ssh/ssh_config.d/*.conf

Host crunchbits
    HostName 203.0.113.10
    Port 2222
    User deploy
    IdentityFile ~/.ssh/id_crunchbits

Host web-1
  HostName=10.0.0.5
  User=root

Host *
    ServerAliveInterval 60

Match host 10.0.0.5 user root
    ForwardAgent yes

Host ignored-no-hostname
    User root

Host gpu-box remote-gpu
    HostName 10.0.0.9
    User admin
    ProxyJump web-1
`

func TestParseSSHConfigGolden(t *testing.T) {
	result := ParseSSHConfig(sampleSSHConfig)
	if len(result.Entries) != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", len(result.Entries), result.Entries)
	}
	first := result.Entries[0]
	if first.Host != "crunchbits" || first.HostName != "203.0.113.10" || first.Port != 2222 || first.User != "deploy" || first.IdentityFile != "~/.ssh/id_crunchbits" {
		t.Fatalf("first entry wrong: %+v", first)
	}
	second := result.Entries[1]
	if second.Host != "web-1" || second.HostName != "10.0.0.5" || second.User != "root" || second.Port != 0 {
		t.Fatalf("second entry wrong: %+v", second)
	}
	third := result.Entries[2]
	if third.Host != "gpu-box" || third.HostName != "10.0.0.9" || third.ProxyJump != "web-1" {
		t.Fatalf("third entry wrong: %+v", third)
	}
	if result.SkippedMatches != 1 {
		t.Fatalf("want 1 skipped Match block, got %d", result.SkippedMatches)
	}
	joined := strings.Join(result.SkippedPatterns, ",")
	if !strings.Contains(joined, "*") || !strings.Contains(joined, "remote-gpu") {
		t.Fatalf("skipped patterns wrong: %v", result.SkippedPatterns)
	}
	if len(result.SkippedNoHostName) != 1 || result.SkippedNoHostName[0] != "ignored-no-hostname" {
		t.Fatalf("no-hostname skips wrong: %v", result.SkippedNoHostName)
	}
}

func TestImportPreviewFromSSHConfig(t *testing.T) {
	mgr, _ := newTestManager(t)
	preview, err := mgr.ImportPreview(SourceSSHConfig, sampleSSHConfig, "")
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview.Entries) != 3 {
		t.Fatalf("entries: %+v", preview.Entries)
	}
	if preview.Entries[0].Meta.Auth != AuthKeyPath || preview.Entries[0].Meta.KeyPath != "~/.ssh/id_crunchbits" {
		t.Fatalf("entry0 meta: %+v", preview.Entries[0].Meta)
	}
	if preview.Entries[1].Meta.Auth != AuthPassword {
		t.Fatalf("entry1 should be password mode: %+v", preview.Entries[1].Meta)
	}
	if !preview.Entries[0].Valid || !preview.Entries[1].Valid {
		t.Fatal("expected valid entries")
	}

	// Existing alias shows up as conflict after import.
	if _, err := mgr.ImportApply(ImportApplyRequest{Source: SourceSSHConfig, Payload: sampleSSHConfig}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	preview2, _ := mgr.ImportPreview(SourceSSHConfig, sampleSSHConfig, "")
	if len(preview2.Entries) != 3 {
		t.Fatalf("entries2: %+v", preview2.Entries)
	}
	for _, entry := range preview2.Entries {
		if !entry.Conflict {
			t.Fatalf("expected conflict for %+v", entry.Meta)
		}
	}
}

func TestImportApplySuffixResolvesConflicts(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.Save(passwordServer("crunchbits", "1.1.1.1")); err != nil {
		t.Fatal(err)
	}
	result, err := mgr.ImportApply(ImportApplyRequest{Source: SourceSSHConfig, Payload: sampleSSHConfig})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Created != 3 || result.Skipped != 0 {
		t.Fatalf("result: %+v", result)
	}
	list, _ := mgr.List()
	aliases := map[string]bool{}
	for _, srv := range list.Servers {
		aliases[srv.Alias] = true
	}
	if !aliases["crunchbits"] || !aliases["crunchbits-2"] || !aliases["web-1"] || !aliases["gpu-box"] {
		t.Fatalf("aliases after suffix import: %v", aliases)
	}
}

func TestImportApplySkipAndOverwrite(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.Save(passwordServer("crunchbits", "1.1.1.1")); err != nil {
		t.Fatal(err)
	}
	result, err := mgr.ImportApply(ImportApplyRequest{
		Source: SourceSSHConfig, Payload: sampleSSHConfig,
		Resolutions: map[string]string{"crunchbits": ResolutionSkip, "web-1": ResolutionOverwrite},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Created != 2 || result.Updated != 0 || result.Skipped != 1 {
		t.Fatalf("result: %+v", result)
	}
	list, _ := mgr.List()
	for _, srv := range list.Servers {
		if srv.Alias == "crunchbits" && srv.Host != "1.1.1.1" {
			t.Fatalf("skip resolution not honored: %+v", srv)
		}
	}
}

func TestExportImportEncryptedRoundTrip(t *testing.T) {
	mgr, _ := newTestManager(t)
	res1, _ := mgr.Save(passwordServer("web-1", "10.0.0.1"))
	in := SaveInput{Server: Server{
		Alias: "keyed", Host: "10.0.0.2", Port: 22, User: "deploy", Auth: AuthKeyInline, Enabled: true,
	}, NewKeyContent: testPrivateKey}
	res2, _ := mgr.Save(in)
	_ = res1
	_ = res2

	file, err := mgr.Export(ExportEncrypted, "open sesame")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(file.Servers) != 2 || file.Secrets == nil {
		t.Fatalf("export file wrong: %+v", file)
	}
	blob, _ := json.Marshal(file)
	if strings.Contains(string(blob), "hunter2") || strings.Contains(string(blob), "OPENSSH PRIVATE KEY") {
		t.Fatal("secrets leaked in export")
	}

	// Import into a fresh manager.
	mgr2, dir2 := newTestManager(t)
	_ = dir2
	preview, err := mgr2.ImportPreview(SourceJSON, string(blob), "")
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !preview.NeedsPassphrase || len(preview.Entries) != 2 {
		t.Fatalf("preview: %+v", preview)
	}

	// Wrong passphrase errors.
	if _, err := mgr2.ImportApply(ImportApplyRequest{Source: SourceJSON, Payload: string(blob), Passphrase: "nope"}); err == nil {
		t.Fatal("expected passphrase error")
	}

	result, err := mgr2.ImportApply(ImportApplyRequest{Source: SourceJSON, Payload: string(blob), Passphrase: "open sesame"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Created != 2 {
		t.Fatalf("result: %+v", result)
	}
	list, _ := mgr2.List()
	var web1, keyed PublicServer
	for _, srv := range list.Servers {
		switch srv.Alias {
		case "web-1":
			web1 = srv
		case "keyed":
			keyed = srv
		}
	}
	if !web1.HasPassword {
		t.Fatal("password not imported")
	}
	if keyed.KeySource != KeySourceManaged {
		t.Fatalf("managed key not imported: %+v", keyed)
	}
	// Managed key materialized and usable (parse-level check).
	keyBytes, err := readTestFile(mgr2.ManagedKeyPath(keyed.ID))
	if err != nil || !strings.Contains(string(keyBytes), "OPENSSH PRIVATE KEY") {
		t.Fatalf("managed key not materialized after import: %v", err)
	}
}

func TestExportNoSecrets(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.Save(passwordServer("web-1", "10.0.0.1")); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Export(ExportEncrypted, ""); err == nil {
		t.Fatal("expected passphrase required error")
	}
	file, err := mgr.Export(ExportNoSecrets, "")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if file.Secrets != nil || len(file.Servers) != 1 {
		t.Fatalf("no-secrets export wrong: %+v", file)
	}
	blob, _ := json.Marshal(file)
	if strings.Contains(string(blob), "hunter2") {
		t.Fatal("secret leaked")
	}
}
