package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeVersionString(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"codex-cli 0.146.0\n", "0.146.0"},
		{"2.1.211 (Claude Code)\n", "2.1.211"},
		{"v1.2.3", "1.2.3"},
		{"0.85.1\n", "0.85.1"},
		{"", ""},
		{"   \n  ", ""},
		{"no version here", "no version here"},
		{"tool 1.0\nsecond 2.0", "1.0"},
	}
	for _, tc := range cases {
		if got := normalizeVersionString(tc.raw); got != tc.want {
			t.Errorf("normalizeVersionString(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestDetectVersionFromInstallCommandUsesNpmTree(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "@scope", "agent")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest, err := json.Marshal(map[string]string{"name": "@scope/agent", "version": "9.9.9"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), manifest, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stub := filepath.Join(t.TempDir(), "npm")
	script := "#!/bin/sh\nif [ \"$1\" = \"root\" ]; then echo " + root + "; exit 0; fi\nexit 1\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(npm stub): %v", err)
	}
	t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))

	def := Definition{
		Name:            "scoped",
		InstallCommands: LifecycleCommands{"npm install -g @scope/agent@latest"},
	}
	if got := detectVersionFromInstallCommand(def); got != "9.9.9" {
		t.Fatalf("detectVersionFromInstallCommand = %q, want 9.9.9", got)
	}
}

func TestDetectVersionFromInstallCommandIgnoresNonNpmInstalls(t *testing.T) {
	def := Definition{
		Name:            "curl-based",
		InstallCommands: LifecycleCommands{"curl -fsSL https://example.com/install.sh | sh"},
	}
	if got := detectVersionFromInstallCommand(def); got != "" {
		t.Fatalf("detectVersionFromInstallCommand = %q, want empty", got)
	}
}

func TestDetectVersionFromCommandParsesOutput(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-agent")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'fake-agent 3.4.5'\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := detectVersionFromCommand(Definition{Name: "fake", Command: script}); got != "3.4.5" {
		t.Fatalf("detectVersionFromCommand = %q, want 3.4.5", got)
	}
}

func TestDetectVersionFromCommandHonorsVersionCommandOverride(t *testing.T) {
	script := filepath.Join(t.TempDir(), "wrapped-agent")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nif [ \"$1\" = \"-V\" ]; then echo '7.7.7'; exit 0; fi\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	def := Definition{Name: "wrapped", Command: script, VersionCommand: script + " -V"}
	if got := detectVersionFromCommand(def); got != "7.7.7" {
		t.Fatalf("detectVersionFromCommand = %q, want 7.7.7", got)
	}
}

func TestMergeAgentDefinitionKeepsVersionCommand(t *testing.T) {
	merged := mergeAgentDefinition(
		Definition{Name: "codex", VersionCommand: "codex --version"},
		Definition{Name: "codex"},
	)
	if merged.VersionCommand != "codex --version" {
		t.Fatalf("VersionCommand = %q, want base value preserved", merged.VersionCommand)
	}
}
