package sshops

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"mindfs/server/internal/secretbox"
)

// Import/export sources.
const (
	SourceJSON      = "json"
	SourceSSHConfig = "ssh_config"
)

// Export modes.
const (
	ExportEncrypted = "encrypted"
	ExportNoSecrets = "no-secrets"
)

// ExportMeta is the plaintext metadata of one exported server.
type ExportMeta struct {
	Alias     string   `json:"alias"`
	Host      string   `json:"host"`
	Port      int      `json:"port"`
	User      string   `json:"user"`
	Auth      AuthMode `json:"auth"`
	KeyPath   string   `json:"key_path,omitempty"`
	ProxyJump string   `json:"proxy_jump,omitempty"`
	Notes     string   `json:"notes,omitempty"`
	Enabled   bool     `json:"enabled"`
}

// exportSecrets is the encrypted payload of an export file.
type exportSecrets struct {
	Servers []exportServerSecrets `json:"servers"`
}

type exportServerSecrets struct {
	Alias      string `json:"alias"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
}

// ExportFile is the portable JSON document written by Export.
type ExportFile struct {
	MindFS     string                        `json:"mindfs"`
	Version    int                           `json:"version"`
	Mode       string                        `json:"mode"`
	ExportedAt time.Time                     `json:"exported_at"`
	Servers    []ExportMeta                  `json:"servers"`
	Secrets    *secretbox.PassphraseEnvelope `json:"secrets,omitempty"`
}

// ImportEntry is one previewed entry of an import.
type ImportEntry struct {
	Meta      ExportMeta `json:"meta"`
	HasSecret bool       `json:"has_secret"`
	Conflict  bool       `json:"conflict"`
	Valid     bool       `json:"valid"`
	Error     string     `json:"error,omitempty"`
}

// ImportPreview is the preview response before applying an import.
type ImportPreview struct {
	Source          string        `json:"source"`
	Entries         []ImportEntry `json:"entries"`
	SkippedPatterns []string      `json:"skipped_patterns,omitempty"`
	SkippedMatches  int           `json:"skipped_matches"`
	NeedsPassphrase bool          `json:"needs_passphrase"`
}

// ImportApplyRequest applies a previously previewed import. The full payload
// must be provided again; the server re-parses and re-decrypts it.
type ImportApplyRequest struct {
	Source      string            `json:"source"`
	Payload     string            `json:"payload"`
	Passphrase  string            `json:"passphrase,omitempty"`
	Resolutions map[string]string `json:"resolutions"`
}

// ImportApplyResolution choices per alias.
const (
	ResolutionOverwrite = "overwrite"
	ResolutionSuffix    = "suffix"
	ResolutionSkip      = "skip"
)

// ImportResult summarizes an applied import.
type ImportResult struct {
	Created int    `json:"created"`
	Updated int    `json:"updated"`
	Skipped int    `json:"skipped"`
	Warning string `json:"warning,omitempty"`
}

// Export produces a portable JSON document. Encrypted mode requires a
// passphrase and embeds all secrets under one passphrase envelope.
func (m *Manager) Export(mode, passphrase string) (*ExportFile, error) {
	switch mode {
	case ExportEncrypted, ExportNoSecrets:
	default:
		return nil, errf(ErrCodeBadPayload, "mode must be %q or %q", ExportEncrypted, ExportNoSecrets)
	}
	if mode == ExportEncrypted && strings.TrimSpace(passphrase) == "" {
		return nil, errf(ErrCodePassphraseNeeded, "passphrase is required for encrypted export")
	}

	m.store.mu.RLock()
	servers, err := m.store.loadLocked()
	m.store.mu.RUnlock()
	if err != nil {
		return nil, err
	}

	file := &ExportFile{
		MindFS:     "ssh-servers",
		Version:    1,
		Mode:       mode,
		ExportedAt: m.store.now().UTC(),
		Servers:    []ExportMeta{},
	}
	secrets := exportSecrets{Servers: []exportServerSecrets{}}
	for _, srv := range servers {
		meta := ExportMeta{
			Alias:     srv.Alias,
			Host:      srv.Host,
			Port:      srv.Port,
			User:      srv.User,
			Auth:      srv.Auth,
			KeyPath:   srv.KeyPath,
			ProxyJump: srv.ProxyJump,
			Notes:     srv.Notes,
			Enabled:   srv.Enabled,
		}
		file.Servers = append(file.Servers, meta)
		secret := exportServerSecrets{Alias: srv.Alias}
		if srv.PasswordSealed != nil {
			if plain, err := m.store.open(srv.ID, srv.PasswordSealed); err == nil {
				secret.Password = string(plain)
				wipeBytes(plain)
			}
		}
		if srv.PrivateKeySealed != nil {
			if plain, err := m.store.open(srv.ID, srv.PrivateKeySealed); err == nil {
				secret.PrivateKey = string(plain)
				wipeBytes(plain)
			}
		}
		if secret.Password != "" || secret.PrivateKey != "" {
			secrets.Servers = append(secrets.Servers, secret)
		}
	}

	if mode == ExportEncrypted {
		payload, err := json.Marshal(secrets)
		if err != nil {
			return nil, err
		}
		envelope, err := secretbox.SealWithPassphrase(passphrase, "mindfs-ssh-export", payload)
		wipeBytes(payload)
		if err != nil {
			return nil, err
		}
		file.Secrets = envelope
	}
	return file, nil
}

// ImportPreview parses a payload and reports what would be imported.
func (m *Manager) ImportPreview(source, payload, passphrase string) (ImportPreview, error) {
	switch source {
	case SourceJSON:
		return m.previewJSON(payload, passphrase)
	case SourceSSHConfig:
		return m.previewSSHConfig(payload)
	}
	return ImportPreview{}, errf(ErrCodeBadPayload, "source must be %q or %q", SourceJSON, SourceSSHConfig)
}

func (m *Manager) previewJSON(payload, passphrase string) (ImportPreview, error) {
	preview := ImportPreview{Source: SourceJSON, Entries: []ImportEntry{}}
	var file ExportFile
	if err := json.Unmarshal([]byte(payload), &file); err != nil {
		return preview, errf(ErrCodeBadPayload, "not a valid MindFS export file: %v", err)
	}
	if file.MindFS != "ssh-servers" {
		return preview, errf(ErrCodeBadPayload, "not a MindFS ssh-servers export file")
	}

	secretsByAlias := map[string]exportServerSecrets{}
	if file.Secrets != nil {
		if strings.TrimSpace(passphrase) == "" {
			preview.NeedsPassphrase = true
		} else {
			plain, err := file.Secrets.Open(passphrase, "mindfs-ssh-export")
			if err != nil {
				return preview, errf(ErrCodePassphraseNeeded, "wrong passphrase or corrupted secrets")
			}
			var secrets exportSecrets
			if err := json.Unmarshal(plain, &secrets); err != nil {
				wipeBytes(plain)
				return preview, errf(ErrCodeBadPayload, "corrupted secrets payload")
			}
			wipeBytes(plain)
			for _, item := range secrets.Servers {
				secretsByAlias[item.Alias] = item
			}
		}
	}

	existing := m.existingAliases()
	for _, meta := range file.Servers {
		entry := ImportEntry{Meta: meta, Valid: true}
		alias := NormalizeAlias(meta.Alias)
		if secret, ok := secretsByAlias[alias]; ok {
			entry.HasSecret = secret.Password != "" || secret.PrivateKey != ""
		}
		if err := Validate(Server{
			Alias: alias, Host: meta.Host, Port: meta.Port, User: meta.User,
			Auth: meta.Auth, KeyPath: meta.KeyPath, ProxyJump: meta.ProxyJump,
			Enabled: meta.Enabled,
		}); err != nil {
			entry.Valid = false
			entry.Error = err.Error()
		} else if meta.Auth == AuthKeyPath {
			if err := ValidateKeyPath(meta.KeyPath); err != nil {
				entry.Valid = false
				entry.Error = err.Error()
			}
		}
		if existing[alias] {
			entry.Conflict = true
		}
		preview.Entries = append(preview.Entries, entry)
	}
	return preview, nil
}

func (m *Manager) previewSSHConfig(payload string) (ImportPreview, error) {
	preview := ImportPreview{Source: SourceSSHConfig, Entries: []ImportEntry{}}
	parsed := ParseSSHConfig(payload)
	preview.SkippedPatterns = parsed.SkippedPatterns
	preview.SkippedMatches = parsed.SkippedMatches

	existing := m.existingAliases()
	for _, entry := range parsed.Entries {
		meta := ExportMeta{
			Alias:     NormalizeAlias(entry.Host),
			Host:      entry.HostName,
			Port:      entry.Port,
			User:      entry.User,
			ProxyJump: entry.ProxyJump,
			Enabled:   true,
		}
		if entry.Port == 0 {
			meta.Port = DefaultPort
		}
		if entry.User == "" {
			meta.User = "root"
		}
		if entry.IdentityFile != "" {
			meta.Auth = AuthKeyPath
			meta.KeyPath = entry.IdentityFile
		} else {
			meta.Auth = AuthPassword
		}
		imp := ImportEntry{Meta: meta, Valid: true}
		if err := Validate(Server{
			Alias: meta.Alias, Host: meta.Host, Port: meta.Port, User: meta.User,
			Auth: meta.Auth, KeyPath: meta.KeyPath, ProxyJump: meta.ProxyJump,
			Enabled: true,
		}); err != nil {
			imp.Valid = false
			imp.Error = err.Error()
		} else if meta.Auth == AuthKeyPath {
			if err := ValidateKeyPath(meta.KeyPath); err != nil {
				imp.Valid = false
				imp.Error = err.Error()
			}
		}
		if existing[meta.Alias] {
			imp.Conflict = true
		}
		preview.Entries = append(preview.Entries, imp)
	}
	// Note which Host blocks lacked HostName and were skipped entirely.
	preview.SkippedPatterns = append(preview.SkippedPatterns, parsed.SkippedNoHostName...)
	return preview, nil
}

func (m *Manager) existingAliases() map[string]bool {
	m.store.mu.RLock()
	servers, err := m.store.loadLocked()
	m.store.mu.RUnlock()
	out := map[string]bool{}
	if err != nil {
		return out
	}
	for _, srv := range servers {
		out[srv.Alias] = true
	}
	return out
}

// ImportApply applies a previewed import with per-alias conflict resolution.
func (m *Manager) ImportApply(req ImportApplyRequest) (ImportResult, error) {
	result := ImportResult{}
	preview, err := m.ImportPreview(req.Source, req.Payload, req.Passphrase)
	if err != nil {
		return result, err
	}

	secretsByAlias := map[string]exportServerSecrets{}
	if req.Source == SourceJSON {
		var file ExportFile
		if err := json.Unmarshal([]byte(req.Payload), &file); err != nil || file.Secrets == nil {
			// no secrets to apply
		} else if plain, err := file.Secrets.Open(req.Passphrase, "mindfs-ssh-export"); err == nil {
			var secrets exportSecrets
			if err := json.Unmarshal(plain, &secrets); err == nil {
				for _, item := range secrets.Servers {
					secretsByAlias[item.Alias] = item
				}
			}
			wipeBytes(plain)
		} else {
			return result, errf(ErrCodePassphraseNeeded, "wrong passphrase or corrupted secrets")
		}
	}

	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	servers, err := m.store.loadLocked()
	if err != nil {
		return result, err
	}

	changed := false
	byAlias := map[string]int{}
	for i := range servers {
		byAlias[servers[i].Alias] = i
	}

	for _, entry := range preview.Entries {
		if !entry.Valid {
			result.Skipped++
			continue
		}
		meta := entry.Meta
		alias := NormalizeAlias(meta.Alias)
		resolution := req.Resolutions[alias]
		if resolution == "" {
			resolution = ResolutionSuffix
		}
		if entry.Conflict {
			switch resolution {
			case ResolutionSkip:
				result.Skipped++
				continue
			case ResolutionSuffix:
				alias = m.freeAlias(alias, byAlias, servers)
			case ResolutionOverwrite:
				// handled below by replacing the existing record
			default:
				result.Skipped++
				continue
			}
		}

		now := m.store.now().UTC()
		record := storedServer{Server: Server{
			Alias:     alias,
			Host:      strings.TrimSpace(meta.Host),
			Port:      meta.Port,
			User:      strings.TrimSpace(meta.User),
			Auth:      meta.Auth,
			KeyPath:   strings.TrimSpace(meta.KeyPath),
			ProxyJump: strings.TrimSpace(meta.ProxyJump),
			Notes:     strings.TrimSpace(meta.Notes),
			Enabled:   meta.Enabled,
			UpdatedAt: now,
		}}

		secret := secretsByAlias[NormalizeAlias(meta.Alias)]
		// Downgrade inline-key entries without key material to password mode
		// so a no-secrets import still yields usable interactive entries.
		if meta.Auth == AuthKeyInline && secret.PrivateKey == "" {
			meta.Auth = AuthPassword
		}

		// Resolve identity before sealing so the envelope AAD matches the id.
		overwriteIdx := -1
		if entry.Conflict && resolution == ResolutionOverwrite {
			if idx, ok := byAlias[alias]; ok {
				overwriteIdx = idx
			}
		}
		if overwriteIdx >= 0 {
			record.ID = servers[overwriteIdx].ID
		} else {
			record.ID = newServerID()
		}
		record.Auth = meta.Auth

		switch meta.Auth {
		case AuthKeyPath:
			if err := ValidateKeyPath(record.KeyPath); err != nil {
				result.Skipped++
				continue
			}
		case AuthPassword:
			if secret.Password != "" {
				env, err := m.store.seal(record.ID, []byte(secret.Password))
				if err != nil {
					return result, err
				}
				record.PasswordSealed = env
			}
		case AuthKeyInline:
			if secret.PrivateKey != "" {
				env, err := m.store.seal(record.ID, []byte(secret.PrivateKey))
				if err != nil {
					return result, err
				}
				record.PrivateKeySealed = env
			}
		}

		if overwriteIdx >= 0 {
			record.CreatedAt = servers[overwriteIdx].CreatedAt
			servers[overwriteIdx] = record
			result.Updated++
			changed = true
			continue
		}

		record.CreatedAt = now
		servers = append(servers, record)
		byAlias[record.Alias] = len(servers) - 1
		result.Created++
		changed = true
	}

	if !changed {
		return result, nil
	}
	if err := m.store.saveLocked(servers); err != nil {
		return ImportResult{}, err
	}
	result.Warning = m.materialize(servers)
	return result, nil
}

// freeAlias finds a non-conflicting alias like alias-2, alias-3, ...
func (m *Manager) freeAlias(alias string, byAlias map[string]int, servers []storedServer) string {
	taken := map[string]bool{}
	for _, srv := range servers {
		taken[srv.Alias] = true
	}
	if !taken[alias] {
		return alias
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", alias, i)
		if len(candidate) > 64 {
			candidate = candidate[:64]
		}
		if !taken[candidate] {
			return candidate
		}
	}
}

// ReadUserSSHConfig returns the local ~/.ssh/config content for import.
func (m *Manager) ReadUserSSHConfig() (string, error) {
	data, err := os.ReadFile(m.paths().UserConfig)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
