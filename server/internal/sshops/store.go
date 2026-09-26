package sshops

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"mindfs/server/internal/secretbox"
)

// storedServer persists a server plus its encrypted secrets.
type storedServer struct {
	Server
	PasswordSealed   *secretbox.Envelope `json:"password_sealed,omitempty"`
	PrivateKeySealed *secretbox.Envelope `json:"private_key_sealed,omitempty"`
}

type storeFile struct {
	Version int            `json:"version"`
	Servers []storedServer `json:"servers"`
}

// Store persists SSH server definitions with encrypted credentials.
type Store struct {
	mu       sync.RWMutex
	filePath string
	box      *secretbox.MasterKey
	now      func() time.Time
}

// NewStore creates the default store (~/.config/mindfs/ssh-servers.json)
// together with its master key.
func NewStore() (*Store, error) {
	configDir, err := secretbox.DefaultKeyPath()
	if err != nil {
		return nil, err
	}
	base := filepath.Dir(configDir)
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	keyPath := filepath.Join(base, "ssh-secret.key")
	box, err := secretbox.LoadOrGenerate(keyPath)
	if err != nil {
		return nil, err
	}
	return &Store{
		filePath: filepath.Join(base, "ssh-servers.json"),
		box:      box,
		now:      time.Now,
	}, nil
}

// NewStoreAt creates a store at an explicit path with a caller-provided
// master key (used by tests and imports).
func NewStoreAt(path string, box *secretbox.MasterKey) *Store {
	return &Store{filePath: path, box: box, now: time.Now}
}

// SetNow overrides the clock (tests only).
func (s *Store) SetNow(now func() time.Time) { s.now = now }

func (s *Store) load() ([]storedServer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() ([]storedServer, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []storedServer{}, nil
		}
		return nil, fmt.Errorf("read ssh servers: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return []storedServer{}, nil
	}
	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse ssh servers: %w", err)
	}
	if file.Version != 1 {
		return nil, fmt.Errorf("unsupported ssh servers store version %d", file.Version)
	}
	return file.Servers, nil
}

func (s *Store) saveLocked(servers []storedServer) error {
	file := storeFile{Version: 1, Servers: servers}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write ssh servers: %w", err)
	}
	if err := os.Rename(tmp, s.filePath); err != nil {
		return fmt.Errorf("publish ssh servers: %w", err)
	}
	return nil
}

// seal encrypts secret material bound to the record id.
func (s *Store) seal(id string, plaintext []byte) (*secretbox.Envelope, error) {
	if s.box == nil {
		return nil, errf(ErrCodeSecretLocked, "secret storage unavailable")
	}
	env, err := s.box.Seal("ssh-server:"+id, plaintext)
	if err != nil {
		return nil, err
	}
	return &env, nil
}

func (s *Store) open(id string, env *secretbox.Envelope) ([]byte, error) {
	if env == nil {
		return nil, errf(ErrCodeSecretRequired, "no secret stored")
	}
	if s.box == nil {
		return nil, errf(ErrCodeSecretLocked, "secret storage unavailable")
	}
	plaintext, err := s.box.Open("ssh-server:"+id, *env)
	if err != nil {
		return nil, errf(ErrCodeSecretCorrupt, "stored secret could not be decrypted (master key changed?)")
	}
	return plaintext, nil
}

// listPublic returns sanitized entries plus reuse hints.
func (s *Store) listPublic() ([]PublicServer, error) {
	stored, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]PublicServer, 0, len(stored))
	for _, item := range stored {
		out = append(out, publicOf(item))
	}
	return out, nil
}

func publicOf(item storedServer) PublicServer {
	p := PublicServer{
		ID:          item.ID,
		Alias:       item.Alias,
		Host:        item.Host,
		Port:        item.Port,
		User:        item.User,
		Auth:        item.Auth,
		KeyPath:     item.KeyPath,
		ProxyJump:   item.ProxyJump,
		Enabled:     item.Enabled,
		Notes:       item.Notes,
		UpdatedAt:   item.UpdatedAt,
		HasPassword: item.PasswordSealed != nil,
	}
	switch {
	case item.Auth == AuthKeyPath:
		p.KeySource = KeySourcePath
	case item.PrivateKeySealed != nil:
		p.KeySource = KeySourceManaged
	}
	return p
}

var idPattern = regexp.MustCompile(`^[a-z0-9]{6,12}$`)

func newServerID() string {
	buf := make([]byte, 6)
	if _, err := readRandom(buf); err != nil {
		return fmt.Sprintf("srv%d", time.Now().UnixNano())
	}
	const hexDigits = "0123456789abcdef"
	id := make([]byte, 0, 12)
	for _, b := range buf {
		id = append(id, hexDigits[b>>4], hexDigits[b&0x0f])
	}
	return "srv" + string(id)
}

func validID(id string) bool { return idPattern.MatchString(id) }
