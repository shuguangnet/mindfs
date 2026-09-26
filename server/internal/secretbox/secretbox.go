// Package secretbox provides symmetric secret storage helpers for MindFS
// server-side credential material. It uses a locally generated master key
// (0600) plus per-record HKDF-SHA256 derived AES-256-GCM envelopes, and
// passphrase-derived envelopes for portable export files.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/pbkdf2"
)

const (
	// KeySize is the AES-256 key length used by all envelopes.
	KeySize = 32
	// Version is the current envelope format version.
	Version = 1
	// IVSize is the GCM nonce length.
	IVSize = 12
	// PBKDF2Iterations is the default passphrase derivation cost.
	PBKDF2Iterations = 600_000
	// SaltSize is the random salt length for passphrase derivation.
	SaltSize = 16

	masterInfo = "mindfs-secretbox/master/v1"
)

// Errors returned by this package.
var (
	ErrInvalidKey      = errors.New("invalid master key")
	ErrInsecureKeyFile = errors.New("master key file permissions are too open")
	ErrBadEnvelope     = errors.New("invalid secret envelope")
	ErrAuthFailed      = errors.New("decryption failed (wrong key or corrupted data)")
)

// MasterKey holds the raw master key material in memory.
type MasterKey struct {
	key [KeySize]byte
}

var _ = io.ReadFull // io used by key generation

func mindfsConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mindfs"), nil
}

// DefaultKeyPath returns the default master key location inside the MindFS
// user config directory (~/.config/mindfs/ssh-secret.key on Unix).
func DefaultKeyPath() (string, error) {
	configDir, err := mindfsConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "ssh-secret.key"), nil
}

// LoadOrGenerate loads the master key from path, creating it with secure
// permissions when missing. The MINDFS_SSH_SECRET_KEY environment variable
// can point to an alternate key file for containerized deployments.
func LoadOrGenerate(path string) (*MasterKey, error) {
	if env := os.Getenv("MINDFS_SSH_SECRET_KEY"); env != "" {
		path = env
	}
	if path == "" {
		return nil, ErrInvalidKey
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read master key: %w", err)
		}
		return generateMasterKey(path)
	}
	if len(data) != KeySize {
		return nil, fmt.Errorf("%w: %s has %d bytes, want %d", ErrInvalidKey, path, len(data), KeySize)
	}
	if err := checkKeyPerms(path); err != nil {
		return nil, err
	}
	m := &MasterKey{}
	copy(m.key[:], data)
	Wipe(data)
	return m, nil
}

func generateMasterKey(path string) (*MasterKey, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create master key dir: %w", err)
	}
	m := &MasterKey{}
	if _, err := io.ReadFull(rand.Reader, m.key[:]); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, m.key[:], 0o600); err != nil {
		return nil, fmt.Errorf("write master key: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, fmt.Errorf("publish master key: %w", err)
	}
	return m, nil
}

// Seal encrypts plaintext into an Envelope bound to aad (the record id).
func (m *MasterKey) Seal(aad string, plaintext []byte) (Envelope, error) {
	if m == nil {
		return Envelope{}, ErrInvalidKey
	}
	key, err := m.derive(aad)
	if err != nil {
		return Envelope{}, err
	}
	defer Wipe(key)
	return sealWithKey(key, aad, plaintext)
}

// Open decrypts an Envelope previously produced by Seal for the same aad.
func (m *MasterKey) Open(aad string, env Envelope) ([]byte, error) {
	if m == nil {
		return nil, ErrInvalidKey
	}
	key, err := m.derive(aad)
	if err != nil {
		return nil, err
	}
	defer Wipe(key)
	return openWithKey(key, aad, env)
}

func (m *MasterKey) derive(aad string) ([]byte, error) {
	reader := hkdf.New(sha256.New, m.key[:], []byte(aad), []byte(masterInfo))
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	return key, nil
}

// Envelope is the persisted AES-256-GCM ciphertext form.
type Envelope struct {
	Version    int    `json:"v"`
	IV         string `json:"iv"`
	CipherText string `json:"ct"`
}

func sealWithKey(key []byte, aad string, plaintext []byte) (Envelope, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return Envelope{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, err
	}
	iv := make([]byte, IVSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return Envelope{}, err
	}
	ct := gcm.Seal(nil, iv, plaintext, []byte(aad))
	return Envelope{
		Version:    Version,
		IV:         base64.StdEncoding.EncodeToString(iv),
		CipherText: base64.StdEncoding.EncodeToString(ct),
	}, nil
}

func openWithKey(key []byte, aad string, env Envelope) ([]byte, error) {
	if env.Version != Version {
		return nil, fmt.Errorf("%w: version %d", ErrBadEnvelope, env.Version)
	}
	iv, err := base64.StdEncoding.DecodeString(env.IV)
	if err != nil || len(iv) != IVSize {
		return nil, fmt.Errorf("%w: bad iv", ErrBadEnvelope)
	}
	ct, err := base64.StdEncoding.DecodeString(env.CipherText)
	if err != nil || len(ct) == 0 {
		return nil, fmt.Errorf("%w: bad ciphertext", ErrBadEnvelope)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, iv, ct, []byte(aad))
	if err != nil {
		return nil, ErrAuthFailed
	}
	return plaintext, nil
}

// PassphraseEnvelope is a self-describing passphrase-encrypted payload used
// for export files.
type PassphraseEnvelope struct {
	Version    int    `json:"v"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	IV         string `json:"iv"`
	CipherText string `json:"ct"`
}

// SealWithPassphrase encrypts plaintext with a key derived from passphrase.
func SealWithPassphrase(passphrase, aad string, plaintext []byte) (*PassphraseEnvelope, error) {
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	key := pbkdf2.Key([]byte(passphrase), salt, PBKDF2Iterations, KeySize, sha256.New)
	defer Wipe(key)
	inner, err := sealWithKey(key, aad, plaintext)
	if err != nil {
		return nil, err
	}
	return &PassphraseEnvelope{
		Version:    Version,
		KDF:        "pbkdf2-sha256",
		Iterations: PBKDF2Iterations,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		IV:         inner.IV,
		CipherText: inner.CipherText,
	}, nil
}

// Open decrypts the envelope with the given passphrase.
func (e *PassphraseEnvelope) Open(passphrase, aad string) ([]byte, error) {
	if e == nil {
		return nil, ErrBadEnvelope
	}
	if e.Version != Version {
		return nil, fmt.Errorf("%w: version %d", ErrBadEnvelope, e.Version)
	}
	salt, err := base64.StdEncoding.DecodeString(e.Salt)
	if err != nil || len(salt) == 0 {
		return nil, fmt.Errorf("%w: bad salt", ErrBadEnvelope)
	}
	iterations := e.Iterations
	if iterations < 1 {
		iterations = PBKDF2Iterations
	}
	key := pbkdf2.Key([]byte(passphrase), salt, iterations, KeySize, sha256.New)
	defer Wipe(key)
	return openWithKey(key, aad, Envelope{Version: Version, IV: e.IV, CipherText: e.CipherText})
}

// Wipe overwrites secret bytes in place.
func Wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
