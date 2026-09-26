package sshops

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// fakeSSHServer is a minimal in-process SSH server for exercising the
// dial, test, and key-deployment paths without external dependencies.
type fakeSSHServer struct {
	listener net.Listener

	mu           sync.Mutex
	passwords    map[string]string
	acceptedKeys map[string]bool
	execLog      []string
	closed       bool
}

func startFakeSSHServer(t *testing.T) *fakeSSHServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSSHServer{
		listener:     listener,
		passwords:    map[string]string{},
		acceptedKeys: map[string]bool{},
	}
	go s.serve()
	t.Cleanup(func() { s.close() })
	return s
}

func (s *fakeSSHServer) close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		_ = s.listener.Close()
	}
	s.mu.Unlock()
}

func (s *fakeSSHServer) host() string { return "127.0.0.1" }

func (s *fakeSSHServer) port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *fakeSSHServer) serve() {
	config := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if want, ok := s.passwords[conn.User()]; ok && want == string(password) {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", conn.User())
		},
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.acceptedKeys[string(key.Marshal())] {
				return nil, nil
			}
			return nil, fmt.Errorf("unknown public key for %q", conn.User())
		},
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return
	}
	config.AddHostKey(signer)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn, config)
	}
}

func (s *fakeSSHServer) handleConn(conn net.Conn, config *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		channel, requests, err := newChan.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(channel, requests)
	}
}

var authorizedKeyLine = regexp.MustCompile(`echo '([^']+)' >> ?~?/?\.?s?sb?h?o?o?t?.*authorized_keys`)

func (s *fakeSSHServer) handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	for req := range requests {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
			_ = req.Reply(false, nil)
			continue
		}
		s.mu.Lock()
		s.execLog = append(s.execLog, payload.Command)
		s.mu.Unlock()

		// Emulate appending to authorized_keys so the key becomes accepted.
		for _, m := range authorizedKeyLine.FindAllStringSubmatch(payload.Command, -1) {
			if parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(m[1])); err == nil {
				s.mu.Lock()
				s.acceptedKeys[string(parsed.Marshal())] = true
				s.mu.Unlock()
			}
		}

		_ = req.Reply(true, nil)
		_, _ = channel.Write([]byte("mindfs-ok\nfake 6.1 mindfs\n"))
		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		_ = channel.Close()
	}
}

func TestDialTestAndDeploy(t *testing.T) {
	mgr, dir := newTestManager(t)
	server := startFakeSSHServer(t)
	server.mu.Lock()
	server.passwords["root"] = "hunter2"
	server.mu.Unlock()

	res, err := mgr.Save(SaveInput{
		Server: Server{
			Alias: "fake", Host: server.host(), Port: server.port(),
			User: "root", Auth: AuthPassword, Enabled: true,
		},
		NewPassword: "hunter2",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	test, err := mgr.Test(context.Background(), res.Server.ID)
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if !test.OK {
		t.Fatalf("expected ok, got %+v", test)
	}

	// Deploy a dedicated key.
	deploy, err := mgr.DeployKey(context.Background(), res.Server.ID)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if !deploy.OK || deploy.Server.Auth != AuthKeyInline || deploy.Server.KeySource != KeySourceManaged {
		t.Fatalf("deploy result: %+v", deploy)
	}

	// managed key materialized
	keyPath := fmt.Sprintf("%s/keys/%s.key", dir, res.Server.ID)
	if _, err := readTestFile(keyPath); err != nil {
		t.Fatalf("managed key missing: %v", err)
	}

	// After deploy the alias must authenticate with the key.
	test2, err := mgr.Test(context.Background(), res.Server.ID)
	if err != nil {
		t.Fatalf("test after deploy: %v", err)
	}
	if !test2.OK {
		t.Fatalf("expected ok after deploy, got %+v", test2)
	}

	// Deploy again is idempotent (reuses the same key).
	deploy2, err := mgr.DeployKey(context.Background(), res.Server.ID)
	if err != nil {
		t.Fatalf("second deploy: %v", err)
	}
	if !deploy2.OK {
		t.Fatalf("second deploy failed: %+v", deploy2)
	}
	first, _ := readTestFile(keyPath)
	deploy2Again, _ := readTestFile(keyPath)
	if string(first) != string(deploy2Again) {
		t.Fatal("managed key changed on redeploy")
	}
}

func TestDialWrongPassword(t *testing.T) {
	mgr, _ := newTestManager(t)
	server := startFakeSSHServer(t)
	server.mu.Lock()
	server.passwords["root"] = "right"
	server.mu.Unlock()

	res, err := mgr.Save(SaveInput{
		Server: Server{
			Alias: "badpw", Host: server.host(), Port: server.port(),
			User: "root", Auth: AuthPassword, Enabled: true,
		},
		NewPassword: "wrong",
	})
	if err != nil {
		t.Fatal(err)
	}
	test, err := mgr.Test(context.Background(), res.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if test.OK || test.ReasonCode != ReasonAuthRejected {
		t.Fatalf("expected auth_rejected, got %+v", test)
	}
}

func TestDialUnreachableHost(t *testing.T) {
	mgr, _ := newTestManager(t)
	// Port 1 on localhost is practically closed.
	res, err := mgr.Save(SaveInput{
		Server: Server{
			Alias: "dead", Host: "127.0.0.1", Port: 1,
			User: "root", Auth: AuthPassword, Enabled: true,
		},
		NewPassword: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	test, err := mgr.Test(context.Background(), res.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if test.OK || test.ReasonCode != ReasonNetworkUnreachable {
		t.Fatalf("expected network_unreachable, got %+v", test)
	}
}

func TestKeyPathAuthUnreadable(t *testing.T) {
	mgr, _ := newTestManager(t)
	res, err := mgr.Save(SaveInput{
		Server: Server{
			Alias: "kp", Host: "127.0.0.1", Port: 22,
			User: "root", Auth: AuthKeyPath, KeyPath: "/nonexistent/id_ed25519", Enabled: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	test, err := mgr.Test(context.Background(), res.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if test.OK || test.ReasonCode != ReasonKeyUnreadable {
		t.Fatalf("expected key_unreadable, got %+v", test)
	}
	if !strings.Contains(test.Detail, "/nonexistent") {
		t.Fatalf("detail should mention path: %q", test.Detail)
	}
}

func readTestFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
