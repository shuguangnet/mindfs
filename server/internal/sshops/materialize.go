package sshops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	managedHeaderComment = "# Managed by MindFS - do not edit (begin)"
	managedFooterComment = "# Managed by MindFS (end)"
	includeMarker        = "# mindfs-servers (managed)"
)

// Paths groups the filesystem locations used by materialization.
type Paths struct {
	HomeDir     string
	UserConfig  string // ~/.ssh/config
	ManagedConf string // ~/.ssh/config.d/mindfs-servers.conf
	KeysDir     string
}

func (m *Manager) paths() Paths {
	sshDir := filepath.Join(m.homeDir, ".ssh")
	return Paths{
		HomeDir:     m.homeDir,
		UserConfig:  filepath.Join(sshDir, "config"),
		ManagedConf: filepath.Join(sshDir, "config.d", "mindfs-servers.conf"),
		KeysDir:     m.keysDir,
	}
}

// ManagedKeyPath returns the materialized private key file path for id.
func (m *Manager) ManagedKeyPath(id string) string {
	return filepath.Join(m.keysDir, id+".key")
}

// MaterializeInfo describes the current materialization state.
type MaterializeInfo struct {
	ConfigFile string `json:"config_file"`
	Included   bool   `json:"included"`
	Warning    string `json:"warning,omitempty"`
}

// MaterializeInfo reports where the managed configuration lives and whether
// the user config includes it, without touching the filesystem state.
func (m *Manager) MaterializeInfo() (MaterializeInfo, error) {
	paths := m.paths()
	info := MaterializeInfo{ConfigFile: paths.ManagedConf}
	data, err := os.ReadFile(paths.UserConfig)
	if err != nil {
		if os.IsNotExist(err) {
			return info, nil
		}
		return info, fmt.Errorf("read ssh config: %w", err)
	}
	info.Included = containsIncludeLine(string(data), paths.ManagedConf)
	return info, nil
}

// materialize regenerates the managed include file and include directive for
// the given stored servers. It returns a human-readable warning on partial
// failure (the encrypted store stays authoritative).
func (m *Manager) materialize(servers []storedServer) string {
	paths := m.paths()

	// Materialize managed key files for inline-key servers.
	enabledKeyIDs := map[string]bool{}
	for _, srv := range servers {
		if !srv.Enabled || srv.Auth != AuthKeyInline || srv.PrivateKeySealed == nil {
			continue
		}
		enabledKeyIDs[srv.ID] = true
		if err := m.writeManagedKey(srv); err != nil {
			return fmt.Sprintf("materialize key %s: %v", srv.Alias, err)
		}
	}

	var b strings.Builder
	b.WriteString(managedHeaderComment + "\n")
	var warnings []string
	for _, srv := range servers {
		if !srv.Enabled {
			continue
		}
		block, err := renderHostBlock(srv, m.ManagedKeyPath(srv.ID))
		if err != nil {
			// One broken entry must not block the rest of the config.
			warnings = append(warnings, fmt.Sprintf("materialize %s: %v", srv.Alias, err))
			continue
		}
		b.WriteString(block)
	}
	b.WriteString(managedFooterComment + "\n")

	if err := writeAtomic(paths.ManagedConf, []byte(b.String()), 0o600, 0o700); err != nil {
		return fmt.Sprintf("write managed ssh config: %v", err)
	}

	if err := m.ensureInclude(paths); err != nil {
		return fmt.Sprintf("update ssh config include: %v", err)
	}

	// Remove managed key files whose owning server entry no longer exists.
	// Keys of disabled servers are kept so re-enabling keeps working.
	if entries, err := os.ReadDir(paths.KeysDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".key") {
				continue
			}
			id := strings.TrimSuffix(name, ".key")
			if !validID(id) || m.managedKeyHasOwner(servers, id) {
				continue
			}
			_ = os.Remove(filepath.Join(paths.KeysDir, name))
		}
	}
	return strings.Join(warnings, "; ")
}

func (m *Manager) managedKeyHasOwner(servers []storedServer, id string) bool {
	for _, srv := range servers {
		if srv.ID == id && srv.Auth == AuthKeyInline && srv.PrivateKeySealed != nil {
			return true
		}
	}
	return false
}

func (m *Manager) writeManagedKey(srv storedServer) error {
	keyBytes, err := m.store.open(srv.ID, srv.PrivateKeySealed)
	if err != nil {
		return err
	}
	defer wipeBytes(keyBytes)
	return writeAtomic(m.ManagedKeyPath(srv.ID), keyBytes, 0o600, 0o700)
}

// renderHostBlock produces one Host stanza for a server.
func renderHostBlock(srv storedServer, managedKeyPath string) (string, error) {
	port := srv.Port
	if port == 0 {
		port = DefaultPort
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Host %s\n", srv.Alias)
	fmt.Fprintf(&b, "    HostName %s\n", srv.Host)
	fmt.Fprintf(&b, "    Port %d\n", port)
	fmt.Fprintf(&b, "    User %s\n", srv.User)
	switch srv.Auth {
	case AuthKeyPath:
		if srv.KeyPath == "" {
			return "", errf(ErrCodeInvalidKeyPath, "server %s has no key path", srv.Alias)
		}
		fmt.Fprintf(&b, "    IdentityFile %s\n", quoteIfNeeded(srv.KeyPath))
		fmt.Fprintf(&b, "    IdentitiesOnly yes\n")
	case AuthKeyInline:
		if srv.PrivateKeySealed == nil {
			return "", errf(ErrCodeSecretRequired, "server %s has no managed key", srv.Alias)
		}
		fmt.Fprintf(&b, "    IdentityFile %s\n", quoteIfNeeded(managedKeyPath))
		fmt.Fprintf(&b, "    IdentitiesOnly yes\n")
	case AuthPassword:
		// Interactive password entry; no identity lines.
	}
	if srv.ProxyJump != "" {
		fmt.Fprintf(&b, "    ProxyJump %s\n", srv.ProxyJump)
	}
	fmt.Fprintf(&b, "    StrictHostKeyChecking accept-new\n")
	return b.String(), nil
}

// ensureInclude adds the Include directive for the managed config to the
// user's ssh config when missing, idempotently.
func (m *Manager) ensureInclude(paths Paths) error {
	if err := os.MkdirAll(filepath.Dir(paths.UserConfig), 0o700); err != nil {
		return fmt.Errorf("create ssh dir: %w", err)
	}
	existing, err := os.ReadFile(paths.UserConfig)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read ssh config: %w", err)
	}
	content := string(existing)
	if containsIncludeLine(content, paths.ManagedConf) {
		return nil
	}
	line := fmt.Sprintf("Include %s %s\n", quoteIfNeeded(paths.ManagedConf), includeMarker)
	if content == "" {
		content = line
	} else {
		content = line + "\n" + content
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(paths.UserConfig); err == nil {
		mode = info.Mode().Perm()
	}
	return writeAtomicWithMode(paths.UserConfig, []byte(content), mode, 0o700)
}

// containsIncludeLine reports whether content already includes the managed
// config path in an Include directive (quoted or bare, with trailing comment).
func containsIncludeLine(content, target string) bool {
	normTarget := filepath.ToSlash(target)
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(strings.ToLower(line), "include") {
			continue
		}
		rest := strings.TrimSpace(line[len("include"):])
		// Strip trailing comment.
		if idx := strings.Index(rest, "#"); idx >= 0 {
			rest = strings.TrimSpace(rest[:idx])
		}
		for _, tok := range strings.Fields(rest) {
			tok = strings.Trim(tok, `"`)
			if filepath.ToSlash(tok) == normTarget {
				return true
			}
		}
	}
	return false
}

func quoteIfNeeded(path string) string {
	if strings.ContainsAny(path, " \t") {
		return `"` + path + `"`
	}
	return path
}

// writeAtomic writes data to path via tmp+rename creating parent dirs.
func writeAtomic(path string, data []byte, fileMode, dirMode os.FileMode) error {
	return writeAtomicWithMode(path, data, fileMode, dirMode)
}

func writeAtomicWithMode(path string, data []byte, fileMode, dirMode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, fileMode); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return os.Rename(tmp, path)
}

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
