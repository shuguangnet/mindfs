package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// EnsureStableWorkDir returns a per-(kind, agent) scratch directory for
// internal agent runs such as probes and session naming.
//
// The directory deliberately lives under the user home instead of os.TempDir():
// agents key their native session storage by working directory, so a scratch
// directory that is wiped on reboot leaves orphaned sessions behind (for
// example dozens of empty `codex resume` entries under
// /tmp/mindfs-title-rename/codex). A stable path keeps that bookkeeping in one
// place and lets operators reclaim the space deliberately.
func EnsureStableWorkDir(kind, agentName string) (string, error) {
	base := filepath.Join(scratchRootDir(), "mindfs-"+strings.TrimSpace(kind))
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	name := strings.TrimSpace(agentName)
	if name == "" {
		name = "default"
	}
	path := filepath.Join(base, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func scratchRootDir() string {
	if value := strings.TrimSpace(os.Getenv("MINDFS_SCRATCH_DIR")); value != "" {
		return value
	}
	if home := UserHomeDir(); home != "" {
		return filepath.Join(home, ".mindfs", "scratch")
	}
	return filepath.Join(os.TempDir(), "mindfs")
}

// IsTemporaryWorkDir reports whether a path is a MindFS scratch directory that
// should never be auto-registered as a user project. It covers both the current
// home-based scratch root and the historical os.TempDir() layout so leftover
// directories are still filtered out.
func IsTemporaryWorkDir(path string) bool {
	normalizedPath := NormalizeComparablePath(path)
	if normalizedPath == "" {
		return false
	}
	if isInsideDir(normalizedPath, NormalizeComparablePath(scratchRootDir())) {
		return true
	}
	return isInsideDir(normalizedPath, NormalizeComparablePath(os.TempDir()))
}

func isInsideDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
