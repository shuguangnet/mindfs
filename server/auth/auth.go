// Package auth implements the optional single-account password gate for the
// MindFS server. When disabled (the default) the server behaves exactly as
// before. When enabled, every /api/* and /ws request must carry a valid signed
// session cookie, except for an explicit allow-list of pre-login endpoints and
// local CLI / relayed requests.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"mindfs/server/internal/config"
)

const (
	// CookieName is the name of the session cookie.
	CookieName = "mindfs_auth"

	// RememberTTL is how long a session lasts when "remember me" is checked.
	RememberTTL = 30 * 24 * time.Hour
	// SessionTTL bounds a non-remembered session token (the cookie itself is a
	// browser-session cookie and disappears when the browser closes).
	SessionTTL = 24 * time.Hour

	// MaxLoginFailures is the number of consecutive failures from one IP before
	// it is temporarily locked out.
	MaxLoginFailures = 5
	// LockDuration is how long an IP stays locked out after too many failures.
	LockDuration = 5 * time.Minute

	statePermission = 0o600
	dirPermission   = 0o700
)

// ErrNotConfigured is returned when an operation requires a password but none
// has been set yet.
var ErrNotConfigured = errors.New("auth password not configured")

// ErrInvalidCredentials is returned for a failed username/password check.
var ErrInvalidCredentials = errors.New("invalid_credentials")

// ErrLockedOut is returned when an IP is temporarily blocked.
var ErrLockedOut = errors.New("too_many_attempts")

// ErrInvalidSettings is returned for malformed settings updates.
var ErrInvalidSettings = errors.New("invalid_auth_settings")

// Status is the serializable view of the auth configuration.
type Status struct {
	Enabled     bool   `json:"enabled"`
	Username    string `json:"username"`
	HasPassword bool   `json:"has_password"`
}

type state struct {
	Enabled       bool   `json:"enabled"`
	Username      string `json:"username"`
	PasswordHash  string `json:"passwordHash"`
	SessionSecret string `json:"sessionSecret"`
	// SessionEpoch is bumped whenever credentials change or all sessions are
	// cleared, which invalidates every previously issued session token.
	SessionEpoch int64 `json:"sessionEpoch"`
}

type attemptRecord struct {
	failures  int
	lockedTil time.Time
}

// Manager owns the persisted auth state and the in-memory login rate limiter.
type Manager struct {
	mu       sync.Mutex
	path     string
	state    state
	attempts map[string]*attemptRecord
	now      func() time.Time
	// modTime/size track the on-disk file so changes made by the CLI (for
	// example `mindfs auth set-password`) are picked up without a restart.
	modTime time.Time
	size    int64
}

// NewManager loads (or lazily initializes) the auth state from the user config
// directory.
func NewManager() (*Manager, error) {
	path, err := StatePath()
	if err != nil {
		return nil, err
	}
	return NewManagerAt(path)
}

// NewManagerAt loads the auth state from an explicit path. Tests use it to
// point at a temporary file.
func NewManagerAt(path string) (*Manager, error) {
	mgr := &Manager{
		path:     path,
		attempts: make(map[string]*attemptRecord),
		now:      time.Now,
	}
	loaded, err := readState(path)
	if err != nil {
		return nil, err
	}
	mgr.state = loaded
	// A file that claims to be enabled but has no usable password is treated as
	// disabled so the server does not lock everyone out.
	if mgr.state.Enabled && !mgr.hasPassword() {
		mgr.state.Enabled = false
	}
	mgr.modTime, mgr.size = statState(path)
	return mgr, nil
}

// StatePath returns the path to auth.json under the MindFS config directory.
func StatePath() (string, error) {
	dir, err := config.MindFSConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "auth.json"), nil
}

// Enabled reports whether the login gate is active.
func (m *Manager) Enabled() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadIfChangedLocked()
	return m.state.Enabled
}

// Status returns the current configuration view.
func (m *Manager) Status() Status {
	if m == nil {
		return Status{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadIfChangedLocked()
	return Status{
		Enabled:     m.state.Enabled,
		Username:    m.state.Username,
		HasPassword: m.hasPassword(),
	}
}

// SetEnabled turns the login gate on or off. Enabling requires a password to be
// configured; disabling clears every outstanding session (Q14-A).
func (m *Manager) SetEnabled(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadIfChangedLocked()
	if enabled && !m.hasPassword() {
		return ErrNotConfigured
	}
	if m.state.Enabled == enabled {
		return nil
	}
	m.state.Enabled = enabled
	if !enabled {
		m.state.SessionEpoch++
	}
	return m.persistLocked()
}

// SetCredentials sets the username and password, hashing the password with
// bcrypt and invalidating all outstanding sessions.
func (m *Manager) SetCredentials(username, password string) error {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" {
		return fmt.Errorf("%w: username required", ErrInvalidSettings)
	}
	if password == "" {
		return fmt.Errorf("%w: password required", ErrInvalidSettings)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadIfChangedLocked()
	m.state.Username = username
	m.state.PasswordHash = string(hash)
	m.state.SessionEpoch++
	return m.persistLocked()
}

// SetUsername changes only the username, keeping the current password.
func (m *Manager) SetUsername(username string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("%w: username required", ErrInvalidSettings)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadIfChangedLocked()
	if !m.hasPassword() {
		return ErrNotConfigured
	}
	if m.state.Username == username {
		return nil
	}
	m.state.Username = username
	m.state.SessionEpoch++
	return m.persistLocked()
}

// VerifyPassword checks a username/password pair.
func (m *Manager) VerifyPassword(username, password string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	m.reloadIfChangedLocked()
	hash := m.state.PasswordHash
	wantUsername := m.state.Username
	m.mu.Unlock()
	if hash == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(username)), []byte(wantUsername)) != 1 {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ClearSessions invalidates every issued session token.
func (m *Manager) ClearSessions() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadIfChangedLocked()
	m.state.SessionEpoch++
	return m.persistLocked()
}

// IssueSession creates a signed session token. The returned maxAge is the
// cookie Max-Age in seconds; 0 means a browser-session cookie.
func (m *Manager) IssueSession(username string, remember bool) (string, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadIfChangedLocked()
	if !m.hasPassword() {
		return "", 0, ErrNotConfigured
	}
	if strings.TrimSpace(username) == "" {
		username = m.state.Username
	}
	ttl := SessionTTL
	maxAge := 0
	if remember {
		ttl = RememberTTL
		maxAge = int(RememberTTL / time.Second)
	}
	payload := sessionPayload{
		Username: username,
		Epoch:    m.state.SessionEpoch,
		Expires:  m.now().Add(ttl).Unix(),
	}
	token, err := m.signLocked(payload)
	if err != nil {
		return "", 0, err
	}
	return token, maxAge, nil
}

// ValidateToken reports whether a session token is authentic and unexpired.
func (m *Manager) ValidateToken(token string) bool {
	if m == nil || strings.TrimSpace(token) == "" {
		return false
	}
	m.mu.Lock()
	m.reloadIfChangedLocked()
	secret := m.state.SessionSecret
	epoch := m.state.SessionEpoch
	m.mu.Unlock()
	if secret == "" {
		return false
	}
	payload, ok := verifyToken(secret, token)
	if !ok {
		return false
	}
	if payload.Epoch != epoch {
		return false
	}
	if m.now().Unix() >= payload.Expires {
		return false
	}
	return true
}

// ValidateRequest reports whether the request carries a valid session cookie.
func (m *Manager) ValidateRequest(r *http.Request) bool {
	if m == nil || r == nil {
		return false
	}
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}
	return m.ValidateToken(cookie.Value)
}

// AllowLogin applies the in-memory rate limiter for the given client IP.
func (m *Manager) AllowLogin(ip string) (bool, time.Duration) {
	ip = normalizeIP(ip)
	if ip == "" || m == nil {
		return true, 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record := m.attempts[ip]
	if record == nil {
		return true, 0
	}
	if m.now().Before(record.lockedTil) {
		return false, record.lockedTil.Sub(m.now())
	}
	return true, 0
}

// RecordFailure increments the failure counter for ip and locks it out once the
// threshold is reached.
func (m *Manager) RecordFailure(ip string) {
	ip = normalizeIP(ip)
	if ip == "" || m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record := m.attempts[ip]
	if record == nil {
		record = &attemptRecord{}
		m.attempts[ip] = record
	}
	record.failures++
	if record.failures >= MaxLoginFailures {
		record.failures = 0
		record.lockedTil = m.now().Add(LockDuration)
	}
}

// ResetFailures clears the failure counter after a successful login.
func (m *Manager) ResetFailures(ip string) {
	ip = normalizeIP(ip)
	if ip == "" || m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.attempts, ip)
}

type sessionPayload struct {
	Username string `json:"u"`
	Epoch    int64  `json:"e"`
	Expires  int64  `json:"x"`
}

func (m *Manager) signLocked(payload sessionPayload) (string, error) {
	if m.state.SessionSecret == "" {
		secret, err := newSecret()
		if err != nil {
			return "", err
		}
		m.state.SessionSecret = secret
		if err := m.persistLocked(); err != nil {
			return "", err
		}
	}
	return signToken(m.state.SessionSecret, payload)
}

func signToken(secret string, payload sessionPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

func verifyToken(secret, token string) (sessionPayload, bool) {
	encoded, sig, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || encoded == "" || sig == "" {
		return sessionPayload{}, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encoded))
	expected := mac.Sum(nil)
	provided, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return sessionPayload{}, false
	}
	if subtle.ConstantTimeCompare(expected, provided) != 1 {
		return sessionPayload{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return sessionPayload{}, false
	}
	var payload sessionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return sessionPayload{}, false
	}
	return payload, true
}

func (m *Manager) hasPassword() bool {
	return strings.TrimSpace(m.state.PasswordHash) != ""
}

func (m *Manager) persistLocked() error {
	if strings.TrimSpace(m.path) == "" {
		return nil
	}
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, dirPermission); err != nil {
		return err
	}
	_ = os.Chmod(dir, dirPermission)
	payload, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	// Write to a temp file and rename so concurrent readers (and the running
	// server reloading after a CLI change) never observe a partial file.
	tmp, err := os.CreateTemp(dir, ".auth-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(statePermission); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, m.path); err != nil {
		return err
	}
	_ = os.Chmod(m.path, statePermission)
	m.modTime, m.size = statState(m.path)
	return nil
}

// reloadIfChangedLocked refreshes in-memory state when auth.json was modified
// externally (for example by `mindfs auth set-password`). Callers must hold
// m.mu.
func (m *Manager) reloadIfChangedLocked() {
	if strings.TrimSpace(m.path) == "" {
		return
	}
	modTime, size := statState(m.path)
	if modTime.IsZero() {
		return
	}
	if modTime.Equal(m.modTime) && size == m.size {
		return
	}
	loaded, err := readState(m.path)
	if err != nil {
		return
	}
	m.state = loaded
	if m.state.Enabled && !m.hasPassword() {
		m.state.Enabled = false
	}
	m.modTime = modTime
	m.size = size
}

func statState(path string) (time.Time, int64) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, 0
	}
	return info.ModTime(), info.Size()
}

func readState(path string) (state, error) {
	var st state
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return state{}, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return state{}, err
	}
	return st, nil
}

func newSecret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func normalizeIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	return strings.TrimSpace(value)
}

// ClientIP extracts the client IP from a request, preferring the direct peer
// address. It intentionally does not trust X-Forwarded-For so a client cannot
// trivially dodge the rate limiter.
func ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	return normalizeIP(r.RemoteAddr)
}
