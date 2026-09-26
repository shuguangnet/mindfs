package sshops

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mindfs/server/internal/secretbox"
)

// Options configures a Manager.
type Options struct {
	// HomeDir overrides the user home directory (tests). Defaults to
	// os.UserHomeDir().
	HomeDir string
	// KeysDir overrides the managed key directory. Defaults to
	// <user config>/mindfs/ssh-keys.
	KeysDir string
}

// Manager orchestrates the encrypted store, key materialization, ssh config
// generation, connectivity tests, key deployment, and import/export.
type Manager struct {
	store   *Store
	homeDir string
	keysDir string
}

// NewManager creates a manager around store.
func NewManager(store *Store, opts Options) (*Manager, error) {
	if store == nil {
		return nil, errors.New("sshops: store required")
	}
	homeDir := strings.TrimSpace(opts.HomeDir)
	if homeDir == "" {
		dir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		homeDir = dir
	}
	keysDir := strings.TrimSpace(opts.KeysDir)
	if keysDir == "" {
		base, err := userConfigDir()
		if err != nil {
			return nil, err
		}
		keysDir = filepath.Join(base, "mindfs", "ssh-keys")
	}
	return &Manager{store: store, homeDir: homeDir, keysDir: keysDir}, nil
}

func userConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return base, nil
}

// SaveInput carries a server plus one-time secret updates. Secrets are only
// accepted here and never returned by any read API.
type SaveInput struct {
	Server
	// NewPassword replaces the stored password (plaintext, used once).
	NewPassword string `json:"new_password,omitempty"`
	// NewKeyContent replaces the managed private key content (plaintext,
	// used once). Must parse as an OpenSSH or PEM private key.
	NewKeyContent string `json:"new_key_content,omitempty"`
	// ReusePasswordFrom copies the password from another server id.
	ReusePasswordFrom string `json:"reuse_password_from,omitempty"`
	// ReuseKeyFrom copies the managed key from another server id.
	ReuseKeyFrom string `json:"reuse_key_from,omitempty"`
}

// SaveResult is returned by Save.
type SaveResult struct {
	Server  PublicServer `json:"server"`
	Warning string       `json:"warning,omitempty"`
}

// Save creates or updates a server, resolves secret reuse, persists the
// encrypted record, and rematerializes the ssh configuration.
func (m *Manager) Save(input SaveInput) (SaveResult, error) {
	srv := input.Server
	srv.Alias = NormalizeAlias(srv.Alias)
	srv.Host = strings.TrimSpace(srv.Host)
	srv.User = strings.TrimSpace(srv.User)
	srv.ProxyJump = strings.TrimSpace(srv.ProxyJump)
	srv.Notes = strings.TrimSpace(srv.Notes)
	if err := Validate(srv); err != nil {
		return SaveResult{}, err
	}
	if srv.Port == 0 {
		srv.Port = DefaultPort
	}
	if srv.Auth == AuthKeyPath {
		if err := ValidateKeyPath(strings.TrimSpace(srv.KeyPath)); err != nil {
			return SaveResult{}, err
		}
		srv.KeyPath = strings.TrimSpace(srv.KeyPath)
	}

	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	servers, err := m.store.loadLocked()
	if err != nil {
		return SaveResult{}, err
	}

	now := m.store.now().UTC()
	targetIdx := -1
	for i := range servers {
		if servers[i].Alias == srv.Alias {
			if srv.ID != "" && servers[i].ID == srv.ID {
				targetIdx = i
			} else {
				return SaveResult{}, errf(ErrCodeAliasConflict, "alias %q already exists", srv.Alias)
			}
		}
	}

	creating := targetIdx < 0
	if creating {
		if srv.ID != "" {
			// Update of a missing server: treat as create only when the id
			// is unknown; otherwise refuse to avoid duplicate identities.
			for i := range servers {
				if servers[i].ID == srv.ID {
					targetIdx = i
					creating = false
					break
				}
			}
		}
	}

	var record storedServer
	if creating {
		srv.ID = newServerID()
		srv.CreatedAt = now
		record = storedServer{Server: srv}
	} else {
		record = servers[targetIdx]
		record.Server = srv
		if srv.ID == "" {
			srv.ID = record.ID
		}
	}
	record.UpdatedAt = now

	switch srv.Auth {
	case AuthKeyPath:
		record.PasswordSealed = nil
		record.PrivateKeySealed = nil
	case AuthPassword:
		record.PrivateKeySealed = nil
		switch {
		case input.NewPassword != "":
			env, err := m.store.seal(record.ID, []byte(input.NewPassword))
			if err != nil {
				return SaveResult{}, err
			}
			record.PasswordSealed = env
		case input.ReusePasswordFrom != "":
			env, err := m.copySecret(record.ID, input.ReusePasswordFrom, servers, "password")
			if err != nil {
				return SaveResult{}, err
			}
			record.PasswordSealed = env
		default:
			if record.PasswordSealed == nil {
				return SaveResult{}, errf(ErrCodeSecretRequired, "password is required for password auth mode")
			}
		}
	case AuthKeyInline:
		record.PasswordSealed = nil
		switch {
		case input.NewKeyContent != "":
			content := strings.TrimSpace(input.NewKeyContent)
			if !looksLikePrivateKey(content) {
				return SaveResult{}, errf(ErrCodeInvalidKey, "key content does not look like a PEM or OpenSSH private key")
			}
			env, err := m.store.seal(record.ID, []byte(input.NewKeyContent))
			if err != nil {
				return SaveResult{}, err
			}
			record.PrivateKeySealed = env
		case input.ReuseKeyFrom != "":
			env, err := m.copySecret(record.ID, input.ReuseKeyFrom, servers, "key")
			if err != nil {
				return SaveResult{}, err
			}
			record.PrivateKeySealed = env
		default:
			if record.PrivateKeySealed == nil {
				return SaveResult{}, errf(ErrCodeSecretRequired, "key content is required for inline key auth mode")
			}
		}
	}

	if creating {
		servers = append(servers, record)
	} else {
		servers[targetIdx] = record
	}
	if err := m.store.saveLocked(servers); err != nil {
		return SaveResult{}, err
	}

	warning := m.materialize(servers)
	return SaveResult{Server: publicOf(record), Warning: warning}, nil
}

// copySecret decrypts a secret from the source record and re-seals it for
// the target record id.
func (m *Manager) copySecret(targetID, sourceID string, servers []storedServer, kind string) (*secretbox.Envelope, error) {
	for i := range servers {
		if servers[i].ID != sourceID {
			continue
		}
		switch kind {
		case "password":
			if servers[i].PasswordSealed == nil {
				return nil, errf(ErrCodeSecretRequired, "source server has no stored password")
			}
			plain, err := m.store.open(sourceID, servers[i].PasswordSealed)
			if err != nil {
				return nil, err
			}
			defer wipeBytes(plain)
			return m.store.seal(targetID, plain)
		case "key":
			if servers[i].PrivateKeySealed == nil {
				return nil, errf(ErrCodeSecretRequired, "source server has no managed key")
			}
			plain, err := m.store.open(sourceID, servers[i].PrivateKeySealed)
			if err != nil {
				return nil, err
			}
			defer wipeBytes(plain)
			return m.store.seal(targetID, plain)
		}
	}
	return nil, errf(ErrCodeNotFound, "reuse source server %q not found", sourceID)
}

// Delete removes a server and rematerializes the configuration.
func (m *Manager) Delete(id string) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	servers, err := m.store.loadLocked()
	if err != nil {
		return err
	}
	idx := -1
	for i := range servers {
		if servers[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errf(ErrCodeNotFound, "server %q not found", id)
	}
	servers = append(servers[:idx], servers[idx+1:]...)
	if err := m.store.saveLocked(servers); err != nil {
		return err
	}
	_ = m.materialize(servers)
	_ = os.Remove(m.ManagedKeyPath(id))
	return nil
}

// ReuseRef identifies a stored secret that can be reused without exposing it.
type ReuseRef struct {
	ID    string `json:"id"`
	Alias string `json:"alias"`
}

// ReuseInfo lists quick-reuse sources computed from existing servers.
type ReuseInfo struct {
	KeyPaths    []string   `json:"key_paths"`
	Passwords   []ReuseRef `json:"passwords"`
	ManagedKeys []ReuseRef `json:"managed_keys"`
}

// ListResponse is the payload of the list API.
type ListResponse struct {
	Servers     []PublicServer   `json:"servers"`
	Reuse       ReuseInfo        `json:"reuse"`
	Materialize *MaterializeInfo `json:"materialize,omitempty"`
}

// List returns sanitized servers plus reuse hints and materialization info.
func (m *Manager) List() (ListResponse, error) {
	resp := ListResponse{Servers: []PublicServer{}, Reuse: ReuseInfo{
		KeyPaths:    []string{},
		Passwords:   []ReuseRef{},
		ManagedKeys: []ReuseRef{},
	}}
	m.store.mu.RLock()
	servers, err := m.store.loadLocked()
	m.store.mu.RUnlock()
	if err != nil {
		return resp, err
	}
	keyPathSet := map[string]bool{}
	for _, item := range servers {
		resp.Servers = append(resp.Servers, publicOf(item))
		if item.Auth == AuthKeyPath && item.KeyPath != "" && !keyPathSet[item.KeyPath] {
			keyPathSet[item.KeyPath] = true
			resp.Reuse.KeyPaths = append(resp.Reuse.KeyPaths, item.KeyPath)
		}
		if item.PasswordSealed != nil {
			resp.Reuse.Passwords = append(resp.Reuse.Passwords, ReuseRef{ID: item.ID, Alias: item.Alias})
		}
		if item.PrivateKeySealed != nil {
			resp.Reuse.ManagedKeys = append(resp.Reuse.ManagedKeys, ReuseRef{ID: item.ID, Alias: item.Alias})
		}
	}
	sort.Slice(resp.Servers, func(i, j int) bool { return resp.Servers[i].Alias < resp.Servers[j].Alias })
	sort.Strings(resp.Reuse.KeyPaths)
	if info, err := m.MaterializeInfo(); err == nil {
		resp.Materialize = &info
	}
	return resp, nil
}

// looksLikePrivateKey performs a cheap sanity check on pasted key content.
func looksLikePrivateKey(content string) bool {
	head := content
	if len(head) > 64 {
		head = head[:64]
	}
	return strings.Contains(head, "-----BEGIN")
}
