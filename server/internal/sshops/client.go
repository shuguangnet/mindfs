package sshops

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Dial failure reason codes.
const (
	ReasonNetworkUnreachable = "network_unreachable"
	ReasonTimeout            = "timeout"
	ReasonAuthRejected       = "auth_rejected"
	ReasonKeyUnreadable      = "key_unreadable"
	ReasonHostKeyRejected    = "host_key_rejected"
	ReasonDialFailed         = "dial_failed"
	ReasonSecretMissing      = "secret_missing"
)

const dialTimeout = 10 * time.Second

// TestResult reports a connectivity check outcome.
type TestResult struct {
	OK         bool   `json:"ok"`
	ReasonCode string `json:"reason_code,omitempty"`
	Banner     string `json:"banner,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// DeployResult reports a key deployment outcome.
type DeployResult struct {
	Server  PublicServer `json:"server"`
	OK      bool         `json:"ok"`
	Detail  string       `json:"detail,omitempty"`
	Warning string       `json:"warning,omitempty"`
}

func (m *Manager) getStored(id string) (storedServer, []storedServer, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()
	servers, err := m.store.loadLocked()
	if err != nil {
		return storedServer{}, nil, err
	}
	for i := range servers {
		if servers[i].ID == id {
			return servers[i], servers, nil
		}
	}
	return storedServer{}, nil, errf(ErrCodeNotFound, "server %q not found", id)
}

// Test performs a real SSH dial and echo command for the given server.
func (m *Manager) Test(ctx context.Context, id string) (TestResult, error) {
	srv, _, err := m.getStored(id)
	if err != nil {
		return TestResult{}, err
	}
	return m.testServer(ctx, srv)
}

func (m *Manager) testServer(ctx context.Context, srv storedServer) (TestResult, error) {
	client, result := m.dial(ctx, srv)
	if !result.OK {
		return result, nil
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return TestResult{OK: false, ReasonCode: ReasonDialFailed, Detail: err.Error()}, nil
	}
	defer session.Close()
	out, err := session.CombinedOutput("echo mindfs-ok; uname -srm 2>/dev/null || ver")
	if err != nil {
		return TestResult{OK: false, ReasonCode: ReasonDialFailed, Detail: fmt.Sprintf("run command: %v", err)}, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "mindfs-ok" {
		return TestResult{OK: false, ReasonCode: ReasonDialFailed, Detail: "unexpected echo output"}, nil
	}
	banner := strings.TrimSpace(strings.Join(lines[1:], " "))
	return TestResult{OK: true, Banner: banner}, nil
}

// dial establishes an SSH connection, returning a classified result.
func (m *Manager) dial(ctx context.Context, srv storedServer) (*ssh.Client, TestResult) {
	addr := net.JoinHostPort(srv.Host, fmt.Sprintf("%d", srv.Port))
	if srv.Port == 0 {
		addr = net.JoinHostPort(srv.Host, "22")
	}

	authMethods, keyErr := m.authMethods(srv)
	if keyErr != nil {
		return nil, TestResult{OK: false, ReasonCode: ReasonKeyUnreadable, Detail: keyErr.Error()}
	}

	config := &ssh.ClientConfig{
		User:            srv.User,
		Auth:            authMethods,
		HostKeyCallback: m.acceptNewHostKey(),
		Timeout:         dialTimeout,
	}

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout+5*time.Second)
	defer cancel()
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, TestResult{OK: false, ReasonCode: classifyNetError(err), Detail: err.Error()}
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		_ = conn.Close()
		return nil, TestResult{OK: false, ReasonCode: classifySSHError(err), Detail: err.Error()}
	}
	return ssh.NewClient(sshConn, chans, reqs), TestResult{OK: true}
}

// authMethods builds auth methods from the stored secrets.
func (m *Manager) authMethods(srv storedServer) ([]ssh.AuthMethod, error) {
	switch srv.Auth {
	case AuthPassword:
		if srv.PasswordSealed == nil {
			return nil, errf(ErrCodeSecretRequired, "no stored password; save the password first")
		}
		plain, err := m.store.open(srv.ID, srv.PasswordSealed)
		if err != nil {
			return nil, err
		}
		password := string(plain)
		defer wipeBytes(plain)
		keyboardInteractive := func(user, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = password
			}
			return answers, nil
		}
		return []ssh.AuthMethod{
			ssh.Password(password),
			ssh.KeyboardInteractive(keyboardInteractive),
		}, nil
	case AuthKeyPath:
		path := expandHome(strings.TrimSpace(srv.KeyPath))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read key %s: %w", path, err)
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse key %s: %w", path, err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	case AuthKeyInline:
		if srv.PrivateKeySealed == nil {
			return nil, errf(ErrCodeSecretRequired, "no managed key stored")
		}
		plain, err := m.store.open(srv.ID, srv.PrivateKeySealed)
		if err != nil {
			return nil, err
		}
		defer wipeBytes(plain)
		signer, err := ssh.ParsePrivateKey(plain)
		if err != nil {
			return nil, fmt.Errorf("parse managed key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	return nil, errf(ErrCodeInvalidAuthMode, "auth mode %q cannot dial", srv.Auth)
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// acceptNewHostKey verifies against known_hosts and accepts-and-records
// unknown hosts (equivalent to StrictHostKeyChecking accept-new).
func (m *Manager) acceptNewHostKey() ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		knownHostsPath := filepath.Join(m.homeDir, ".ssh", "known_hosts")
		if callback, err := knownhosts.New(knownHostsPath); err == nil {
			if err := callback(hostname, remote, key); err == nil {
				return nil
			} else {
				var keyErr *knownhosts.KeyError
				if !errors.As(err, &keyErr) {
					return fmt.Errorf("host key verification for %s failed: %w", hostname, err)
				}
				if len(keyErr.Want) > 0 {
					return fmt.Errorf("host key for %s changed, refusing to continue", hostname)
				}
				// Unknown host: fall through to record.
			}
		}
		if err := m.appendKnownHost(hostname, remote, key); err != nil {
			return fmt.Errorf("record host key for %s: %w", hostname, err)
		}
		return nil
	}
}

func (m *Manager) appendKnownHost(hostname string, remote net.Addr, key ssh.PublicKey) error {
	entries := []string{knownhosts.Normalize(hostname)}
	if remote != nil {
		if _, port, err := net.SplitHostPort(remote.String()); err == nil && port != "" {
			normalized := knownhosts.Normalize(net.JoinHostPort(strings.Trim(hostname, "[]"), port))
			if normalized != entries[0] {
				entries = append(entries, normalized)
			}
		}
	}
	knownHostsPath := filepath.Join(m.homeDir, ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(knownHostsPath), 0o700); err != nil {
		return err
	}
	line := knownhosts.Line(entries, key)
	f, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

// DeployKey generates (or reuses) a dedicated ed25519 key, installs the
// public key on the remote using the stored password, and switches the
// server to managed-key authentication.
func (m *Manager) DeployKey(ctx context.Context, id string) (DeployResult, error) {
	srv, servers, err := m.getStored(id)
	if err != nil {
		return DeployResult{}, err
	}
	if srv.PasswordSealed == nil {
		return DeployResult{}, errf(ErrCodeSecretRequired, "stored password required to deploy a key; save the password first")
	}

	privateKey := srv.PrivateKeySealed
	generated := false
	if privateKey == nil {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return DeployResult{}, fmt.Errorf("generate key: %w", err)
		}
		block, err := ssh.MarshalPrivateKey(priv, "mindfs-"+srv.ID)
		if err != nil {
			return DeployResult{}, err
		}
		content := pem.EncodeToMemory(block)
		env, err := m.store.seal(srv.ID, content)
		if err != nil {
			return DeployResult{}, err
		}
		privateKey = env
		generated = true
		wipeBytes(content)
	}

	plain, err := m.store.open(srv.ID, privateKey)
	if err != nil {
		return DeployResult{}, err
	}
	signer, err := ssh.ParsePrivateKey(plain)
	wipeBytes(plain)
	if err != nil {
		if !generated {
			return DeployResult{}, errf(ErrCodeSecretCorrupt, "stored managed key cannot be parsed: %v", err)
		}
		return DeployResult{}, fmt.Errorf("parse generated key: %w", err)
	}
	pubKeyLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))

	// Dial with the stored password.
	passwordAuth := srv
	client, result := m.dialWithPassword(ctx, passwordAuth)
	if !result.OK {
		return DeployResult{Server: publicOf(srv), OK: false, Detail: result.Detail}, nil
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return DeployResult{Server: publicOf(srv), OK: false, Detail: err.Error()}, nil
	}
	defer session.Close()
	script := fmt.Sprintf(
		`umask 077; mkdir -p ~/.ssh && touch ~/.ssh/authorized_keys && grep -qxF '%s' ~/.ssh/authorized_keys || echo '%s' >> ~/.ssh/authorized_keys; chmod 700 ~/.ssh 2>/dev/null; chmod 600 ~/.ssh/authorized_keys`,
		pubKeyLine, pubKeyLine,
	)
	if _, err := session.CombinedOutput(script); err != nil {
		return DeployResult{Server: publicOf(srv), OK: false, Detail: fmt.Sprintf("install public key: %v", err)}, nil
	}

	// Switch to managed-key auth and persist.
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	current, err := m.store.loadLocked()
	if err != nil {
		return DeployResult{}, err
	}
	idx := -1
	for i := range current {
		if current[i].ID == srv.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return DeployResult{}, errf(ErrCodeNotFound, "server %q disappeared during deploy", id)
	}
	current[idx].Auth = AuthKeyInline
	current[idx].KeyPath = ""
	current[idx].PrivateKeySealed = privateKey
	current[idx].UpdatedAt = m.store.now().UTC()
	if err := m.store.saveLocked(current); err != nil {
		return DeployResult{}, err
	}
	warning := m.materialize(current)
	_ = servers
	return DeployResult{Server: publicOf(current[idx]), OK: true, Warning: warning}, nil
}

// dialWithPassword dials using the stored password regardless of the
// configured auth mode.
func (m *Manager) dialWithPassword(ctx context.Context, srv storedServer) (*ssh.Client, TestResult) {
	plain, err := m.store.open(srv.ID, srv.PasswordSealed)
	if err != nil {
		return nil, TestResult{OK: false, ReasonCode: ReasonSecretMissing, Detail: err.Error()}
	}
	password := string(plain)
	defer wipeBytes(plain)

	addr := net.JoinHostPort(srv.Host, fmt.Sprintf("%d", srv.Port))
	config := &ssh.ClientConfig{
		User: srv.User,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = password
				}
				return answers, nil
			}),
		},
		HostKeyCallback: m.acceptNewHostKey(),
		Timeout:         dialTimeout,
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout+5*time.Second)
	defer cancel()
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, TestResult{OK: false, ReasonCode: classifyNetError(err), Detail: err.Error()}
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		_ = conn.Close()
		return nil, TestResult{OK: false, ReasonCode: classifySSHError(err), Detail: err.Error()}
	}
	return ssh.NewClient(sshConn, chans, reqs), TestResult{OK: true}
}

func classifyNetError(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "i/o timeout") || errors.Is(err, context.DeadlineExceeded):
		return ReasonTimeout
	case strings.Contains(msg, "refused"), strings.Contains(msg, "unreachable"), strings.Contains(msg, "no route"), strings.Contains(msg, "no such host"):
		return ReasonNetworkUnreachable
	}
	return ReasonNetworkUnreachable
}

func classifySSHError(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unable to authenticate"), strings.Contains(msg, "no supported methods"), strings.Contains(msg, "authentication"):
		return ReasonAuthRejected
	case strings.Contains(msg, "host key"):
		return ReasonHostKeyRejected
	}
	return ReasonDialFailed
}
