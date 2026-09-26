// Package sshops manages SSH server aliases with encrypted credentials and
// materializes them into OpenSSH-compatible configuration so that
// `ssh <alias>` works inside agent shells and terminals.
package sshops

import (
	"errors"
	"fmt"
	"time"
)

// AuthMode describes how a server authenticates.
type AuthMode string

const (
	// AuthKeyPath uses an existing private key file path chosen by the user.
	AuthKeyPath AuthMode = "key_path"
	// AuthKeyInline uses key content pasted by the user and stored encrypted;
	// MindFS materializes it as a managed key file.
	AuthKeyInline AuthMode = "key_inline"
	// AuthPassword uses an encrypted stored password (interactive ssh use,
	// connectivity test, and one-click key deployment).
	AuthPassword AuthMode = "password"
)

// Key sources reported to clients.
const (
	KeySourcePath    = "path"
	KeySourceManaged = "managed"
)

// Server is the user-visible SSH server definition (no secrets).
type Server struct {
	ID        string    `json:"id"`
	Alias     string    `json:"alias"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	User      string    `json:"user"`
	Auth      AuthMode  `json:"auth"`
	KeyPath   string    `json:"key_path,omitempty"`
	ProxyJump string    `json:"proxy_jump,omitempty"`
	Enabled   bool      `json:"enabled"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PublicServer is the API-facing representation with secret-presence flags.
type PublicServer struct {
	ID          string    `json:"id"`
	Alias       string    `json:"alias"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	User        string    `json:"user"`
	Auth        AuthMode  `json:"auth"`
	KeyPath     string    `json:"key_path,omitempty"`
	KeySource   string    `json:"key_source,omitempty"`
	HasPassword bool      `json:"has_password"`
	ProxyJump   string    `json:"proxy_jump,omitempty"`
	Enabled     bool      `json:"enabled"`
	Notes       string    `json:"notes,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Structured error codes used by the API layer.
const (
	ErrCodeInvalidAlias     = "ssh_invalid_alias"
	ErrCodeInvalidHost      = "ssh_invalid_host"
	ErrCodeInvalidPort      = "ssh_invalid_port"
	ErrCodeInvalidUser      = "ssh_invalid_user"
	ErrCodeInvalidAuthMode  = "ssh_invalid_auth_mode"
	ErrCodeInvalidKeyPath   = "ssh_invalid_key_path"
	ErrCodeInvalidKey       = "ssh_invalid_key"
	ErrCodeInvalidProxyJump = "ssh_invalid_proxy_jump"
	ErrCodeAliasConflict    = "ssh_alias_conflict"
	ErrCodeNotFound         = "ssh_server_not_found"
	ErrCodeSecretRequired   = "ssh_secret_required"
	ErrCodeSecretLocked     = "ssh_secret_storage_unavailable"
	ErrCodeSecretCorrupt    = "ssh_secret_corrupt"
	ErrCodePassphraseNeeded = "ssh_passphrase_required"
	ErrCodeBadPayload       = "ssh_bad_payload"
	ErrCodeMaterialize      = "ssh_materialize_failed"
)

// Error is a structured sshops error with a stable code.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func errf(code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// CodeOf returns the structured code for err, defaulting to "".
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
