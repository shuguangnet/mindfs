package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureStableWorkDirUsesScratchRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MINDFS_SCRATCH_DIR", "")

	path, err := EnsureStableWorkDir("title-rename", "codex")
	if err != nil {
		t.Fatalf("EnsureStableWorkDir: %v", err)
	}
	want := filepath.Join(home, ".mindfs", "scratch", "mindfs-title-rename", "codex")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("scratch dir not created: %v", err)
	}
	// Scratch directories must never be auto-registered as user projects.
	if !IsTemporaryWorkDir(path) {
		t.Fatalf("scratch dir %q should be excluded from project discovery", path)
	}
}

func TestEnsureStableWorkDirHonorsScratchOverride(t *testing.T) {
	override := t.TempDir()
	t.Setenv("MINDFS_SCRATCH_DIR", override)

	path, err := EnsureStableWorkDir("agent-probe", "pi")
	if err != nil {
		t.Fatalf("EnsureStableWorkDir: %v", err)
	}
	if !strings.HasPrefix(path, override) {
		t.Fatalf("path = %q, want prefix %q", path, override)
	}
	if !IsTemporaryWorkDir(path) {
		t.Fatalf("overridden scratch dir %q should be excluded from project discovery", path)
	}
}

func TestEnsureStableWorkDirFallsBackToDefaultAgentName(t *testing.T) {
	t.Setenv("MINDFS_SCRATCH_DIR", t.TempDir())

	path, err := EnsureStableWorkDir("agent-probe", "  ")
	if err != nil {
		t.Fatalf("EnsureStableWorkDir: %v", err)
	}
	if filepath.Base(path) != "default" {
		t.Fatalf("base = %q, want default", filepath.Base(path))
	}
}

func TestIsTemporaryWorkDir(t *testing.T) {
	t.Setenv("MINDFS_SCRATCH_DIR", filepath.Join(t.TempDir(), "scratch"))

	legacy := filepath.Join(os.TempDir(), "mindfs-title-rename", "codex")
	if !IsTemporaryWorkDir(legacy) {
		t.Fatalf("legacy temp scratch dir %q should be temporary", legacy)
	}
	if IsTemporaryWorkDir("") {
		t.Fatal("empty path must not be reported as temporary")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if IsTemporaryWorkDir(wd) {
		t.Fatalf("working directory %q must not be reported as temporary", wd)
	}
}
