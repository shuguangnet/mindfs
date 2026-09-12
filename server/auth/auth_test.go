package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	mgr, err := NewManagerAt(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatalf("NewManagerAt: %v", err)
	}
	return mgr
}

func TestDisabledByDefault(t *testing.T) {
	mgr := newTestManager(t)
	status := mgr.Status()
	if status.Enabled {
		t.Fatal("auth should be disabled by default")
	}
	if status.HasPassword {
		t.Fatal("no password should be configured by default")
	}
	if mgr.Enabled() {
		t.Fatal("Enabled() should be false by default")
	}
}

func TestEnableRequiresPassword(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.SetEnabled(true); err != ErrNotConfigured {
		t.Fatalf("SetEnabled(true) without password = %v, want ErrNotConfigured", err)
	}
	if err := mgr.SetCredentials("alice", "s3cret"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if err := mgr.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled(true): %v", err)
	}
	if !mgr.Enabled() {
		t.Fatal("auth should be enabled")
	}
}

func TestVerifyPassword(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.SetCredentials("alice", "s3cret"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if !mgr.VerifyPassword("alice", "s3cret") {
		t.Fatal("correct credentials should verify")
	}
	if mgr.VerifyPassword("alice", "wrong") {
		t.Fatal("wrong password should not verify")
	}
	if mgr.VerifyPassword("bob", "s3cret") {
		t.Fatal("wrong username should not verify")
	}
}

func TestPasswordChangeInvalidatesSessions(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.SetCredentials("alice", "s3cret"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if err := mgr.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	token, _, err := mgr.IssueSession("alice", true)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if !mgr.ValidateToken(token) {
		t.Fatal("fresh token should be valid")
	}
	if err := mgr.SetCredentials("alice", "newpass"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if mgr.ValidateToken(token) {
		t.Fatal("token should be invalid after password change")
	}
}

func TestClearSessionsInvalidatesTokens(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.SetCredentials("alice", "s3cret"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	token, _, err := mgr.IssueSession("alice", false)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if err := mgr.ClearSessions(); err != nil {
		t.Fatalf("ClearSessions: %v", err)
	}
	if mgr.ValidateToken(token) {
		t.Fatal("token should be invalid after ClearSessions")
	}
}

func TestDisableClearsSessions(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.SetCredentials("alice", "s3cret"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if err := mgr.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	token, _, err := mgr.IssueSession("alice", true)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if err := mgr.SetEnabled(false); err != nil {
		t.Fatalf("SetEnabled(false): %v", err)
	}
	if mgr.ValidateToken(token) {
		t.Fatal("token should be invalid after disabling auth")
	}
	// Re-enabling should still require a fresh login.
	if err := mgr.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled(true): %v", err)
	}
	if mgr.ValidateToken(token) {
		t.Fatal("token should remain invalid after re-enabling")
	}
}

func TestSessionExpiry(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.SetCredentials("alice", "s3cret"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	now := time.Now()
	mgr.now = func() time.Time { return now }
	token, _, err := mgr.IssueSession("alice", false)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if !mgr.ValidateToken(token) {
		t.Fatal("token should be valid before expiry")
	}
	mgr.now = func() time.Time { return now.Add(SessionTTL + time.Minute) }
	if mgr.ValidateToken(token) {
		t.Fatal("token should be expired")
	}
}

func TestRememberCookieMaxAge(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.SetCredentials("alice", "s3cret"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	_, maxAge, err := mgr.IssueSession("alice", false)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if maxAge != 0 {
		t.Fatalf("non-remember maxAge = %d, want 0 (browser session cookie)", maxAge)
	}
	_, maxAge, err = mgr.IssueSession("alice", true)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if maxAge != int(RememberTTL/time.Second) {
		t.Fatalf("remember maxAge = %d, want %d", maxAge, int(RememberTTL/time.Second))
	}
}

func TestRateLimiter(t *testing.T) {
	mgr := newTestManager(t)
	for i := 0; i < MaxLoginFailures-1; i++ {
		mgr.RecordFailure("1.2.3.4")
		if ok, _ := mgr.AllowLogin("1.2.3.4"); !ok {
			t.Fatalf("locked out too early at attempt %d", i+1)
		}
	}
	mgr.RecordFailure("1.2.3.4")
	if ok, retry := mgr.AllowLogin("1.2.3.4"); ok || retry <= 0 {
		t.Fatalf("expected lockout with positive retry, got ok=%v retry=%v", ok, retry)
	}
	// A different IP is unaffected.
	if ok, _ := mgr.AllowLogin("5.6.7.8"); !ok {
		t.Fatal("other IP should not be locked out")
	}
	mgr.ResetFailures("1.2.3.4")
	if ok, _ := mgr.AllowLogin("1.2.3.4"); !ok {
		t.Fatal("ResetFailures should unlock the IP")
	}
}

func TestPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	mgr, err := NewManagerAt(path)
	if err != nil {
		t.Fatalf("NewManagerAt: %v", err)
	}
	if err := mgr.SetCredentials("alice", "s3cret"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if err := mgr.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	reloaded, err := NewManagerAt(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	status := reloaded.Status()
	if !status.Enabled || status.Username != "alice" || !status.HasPassword {
		t.Fatalf("reloaded status = %+v", status)
	}
	if !reloaded.VerifyPassword("alice", "s3cret") {
		t.Fatal("reloaded manager should verify the persisted password")
	}
}

func TestEnabledWithoutPasswordIsTreatedAsDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	writeStateForTest(t, path, state{Enabled: true, Username: "alice"})
	mgr, err := NewManagerAt(path)
	if err != nil {
		t.Fatalf("NewManagerAt: %v", err)
	}
	if mgr.Enabled() {
		t.Fatal("state with enabled=true but no password must be treated as disabled")
	}
}

func TestValidateRequest(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.SetCredentials("alice", "s3cret"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	token, _, err := mgr.IssueSession("alice", false)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	if !mgr.ValidateRequest(req) {
		t.Fatal("request with valid cookie should validate")
	}
	missing := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
	if mgr.ValidateRequest(missing) {
		t.Fatal("request without cookie should not validate")
	}
}

func TestExternalFileChangeIsReloaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	server, err := NewManagerAt(path)
	if err != nil {
		t.Fatalf("NewManagerAt: %v", err)
	}
	if err := server.SetCredentials("alice", "oldpass"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if err := server.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	token, _, err := server.IssueSession("alice", true)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	// Simulate `mindfs auth set-password` running against the same file while
	// the server keeps its own manager instance alive.
	time.Sleep(10 * time.Millisecond)
	cli, err := NewManagerAt(path)
	if err != nil {
		t.Fatalf("NewManagerAt (cli): %v", err)
	}
	if err := cli.SetCredentials("alice", "newpass"); err != nil {
		t.Fatalf("cli SetCredentials: %v", err)
	}

	if !server.VerifyPassword("alice", "newpass") {
		t.Fatal("running server should observe the password set by the CLI")
	}
	if server.VerifyPassword("alice", "oldpass") {
		t.Fatal("old password should no longer verify")
	}
	if server.ValidateToken(token) {
		t.Fatal("sessions issued before the external change should be invalidated")
	}
}

func writeStateForTest(t *testing.T, path string, st state) {
	t.Helper()
	mgr := &Manager{path: path, attempts: map[string]*attemptRecord{}, now: time.Now, state: st}
	if err := mgr.persistLocked(); err != nil {
		t.Fatalf("persist: %v", err)
	}
}
