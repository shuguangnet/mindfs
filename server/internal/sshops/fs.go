package sshops

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fsListLimit caps the number of entries returned per directory listing so a
// huge directory cannot produce an oversized API response.
const fsListLimit = 1000

// FSEntry is one directory entry in a key-file picker listing.
type FSEntry struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	IsDir        bool   `json:"is_dir"`
	IsLink       bool   `json:"is_link,omitempty"`
	Size         int64  `json:"size,omitempty"`
	LooksLikeKey bool   `json:"looks_like_key,omitempty"`
}

// FSListing is the payload of GET /api/ssh-servers/fs.
type FSListing struct {
	Path      string    `json:"path"`
	Parent    string    `json:"parent,omitempty"`
	Home      string    `json:"home"`
	Entries   []FSEntry `json:"entries"`
	Truncated bool      `json:"truncated,omitempty"`
}

// ListFS lists one directory level on the host for the key file picker. An
// empty or "~" path lists the user home. It never reads file contents; only
// names, types, and sizes are exposed.
func (m *Manager) ListFS(rawPath string) (*FSListing, error) {
	if m == nil {
		return nil, errf(ErrCodeSecretLocked, "ssh server manager not configured")
	}
	dir := m.resolveFSPath(rawPath)
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errf(ErrCodeBadPayload, "cannot list %q: %v", dir, err)
	}

	listing := &FSListing{
		Path:    dir,
		Parent:  parentDir(dir),
		Home:    m.homeDir,
		Entries: make([]FSEntry, 0, len(dirEntries)),
	}
	for _, entry := range dirEntries {
		if len(listing.Entries) >= fsListLimit {
			listing.Truncated = true
			break
		}
		info, infoErr := entry.Info()
		item := FSEntry{
			Name:         entry.Name(),
			Path:         filepath.Join(dir, entry.Name()),
			IsDir:        entry.IsDir(),
			IsLink:       entry.Type()&fs.ModeSymlink != 0,
			LooksLikeKey: looksLikeKeyName(entry.Name()),
		}
		if infoErr == nil && info != nil && !item.IsDir {
			item.Size = info.Size()
		}
		listing.Entries = append(listing.Entries, item)
	}
	sortEntries(listing.Entries)
	return listing, nil
}

// resolveFSPath expands ~ and cleans the path into an absolute directory path.
func (m *Manager) resolveFSPath(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" || trimmed == "~" {
		return m.homeDir
	}
	if trimmed == "~/" || strings.HasPrefix(trimmed, "~/") {
		return filepath.Join(m.homeDir, strings.TrimPrefix(trimmed, "~/"))
	}
	return filepath.Clean(trimmed)
}

func parentDir(dir string) string {
	parent := filepath.Dir(dir)
	if parent == dir {
		return "" // filesystem root has no parent
	}
	return parent
}

func sortEntries(entries []FSEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

// looksLikeKeyName reports whether a file name plausibly refers to an SSH
// private key (e.g. id_ed25519, identity, server.pem, deploy_key). It is only
// a UI hint; correctness is enforced when the key is actually used.
func looksLikeKeyName(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".pub") {
		return false
	}
	if strings.HasPrefix(lower, "id_") || strings.Contains(lower, "identity") {
		return true
	}
	for _, suffix := range []string{".pem", ".key"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return strings.Contains(lower, "_key") || strings.Contains(lower, "-key")
}
