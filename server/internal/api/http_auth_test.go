package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mindfs/server/auth"
)

func newTestAuthManager(t *testing.T, enabled bool) *auth.Manager {
	t.Helper()
	mgr, err := auth.NewManagerAt(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatalf("NewManagerAt: %v", err)
	}
	if err := mgr.SetCredentials("alice", "s3cret"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if enabled {
		if err := mgr.SetEnabled(true); err != nil {
			t.Fatalf("SetEnabled: %v", err)
		}
	}
	return mgr
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func runAuthMiddleware(t *testing.T, mgr *auth.Manager, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	h := &HTTPHandler{Auth: mgr, LocalCLIToken: "cli-token"}
	rec := httptest.NewRecorder()
	h.AuthMiddleware(okHandler()).ServeHTTP(rec, req)
	return rec
}

func TestAuthMiddlewareDisabledPasses(t *testing.T) {
	mgr := newTestAuthManager(t, false)
	rec := runAuthMiddleware(t, mgr, httptest.NewRequest(http.MethodGet, "/api/tree", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("disabled gate status = %d, want 200", rec.Code)
	}
}

func TestAuthMiddlewareBlocksUnauthenticatedAPI(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	rec := runAuthMiddleware(t, mgr, httptest.NewRequest(http.MethodGet, "/api/tree", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /api/tree status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddlewareAllowsExemptPaths(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	for _, path := range []string{"/health", "/api/auth/status", "/api/auth/login", "/api/relay/status", "/api/e2ee/open"} {
		rec := runAuthMiddleware(t, mgr, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("exempt path %s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestAuthMiddlewareAllowsStaticFrontend(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	for _, path := range []string{"/", "/assets/app.js", "/index.html"} {
		rec := runAuthMiddleware(t, mgr, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("static path %s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestAuthMiddlewareAllowsValidSession(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	token, _, err := mgr.IssueSession("alice", true)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	rec := runAuthMiddleware(t, mgr, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid session status = %d, want 200", rec.Code)
	}
}

func TestAuthMiddlewareAllowsLocalCLIRequest(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/12", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set(localCLIHeaderName, "cli-token")
	rec := runAuthMiddleware(t, mgr, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("local CLI request status = %d, want 200", rec.Code)
	}
}

func TestAuthMiddlewareAllowsRelayedRequest(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
	req.Header.Set("X-MindFS-Relayed", "1")
	rec := runAuthMiddleware(t, mgr, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("relayed request status = %d, want 200", rec.Code)
	}
}

func TestAuthMiddlewareGatesWebSocket(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	rec := runAuthMiddleware(t, mgr, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /ws status = %d, want 401", rec.Code)
	}
}

func authHandler(mgr *auth.Manager) *HTTPHandler {
	return &HTTPHandler{Auth: mgr}
}

func TestHandleAuthStatusReflectsGate(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	rec := httptest.NewRecorder()
	authHandler(mgr).handleAuthStatus(rec, httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"authenticated":false`) {
		t.Fatalf("unexpected status body: %s", body)
	}
}

func TestHandleAuthLoginSuccessSetsCookie(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"s3cret","remember":true}`))
	rec := httptest.NewRecorder()
	authHandler(mgr).handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != auth.CookieName || cookies[0].Value == "" {
		t.Fatalf("login should set a session cookie, got %+v", cookies)
	}
	if !cookies[0].HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}
}

func TestHandleAuthLoginRejectsBadPassword(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"wrong"}`))
	rec := httptest.NewRecorder()
	authHandler(mgr).handleAuthLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad password status = %d, want 401", rec.Code)
	}
}

func TestHandleAuthLoginRateLimits(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	for i := 0; i < auth.MaxLoginFailures; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"wrong"}`))
		req.RemoteAddr = "9.9.9.9:1234"
		rec := httptest.NewRecorder()
		authHandler(mgr).handleAuthLogin(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, rec.Code)
		}
	}
	// The next attempt, even with the correct password, is locked out.
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"s3cret"}`))
	req.RemoteAddr = "9.9.9.9:1234"
	rec := httptest.NewRecorder()
	authHandler(mgr).handleAuthLogin(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("locked out status = %d, want 429", rec.Code)
	}
}

func TestHandleAuthSettingsEnablesAndIssuesSession(t *testing.T) {
	mgr, err := auth.NewManagerAt(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatalf("NewManagerAt: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/auth/settings", strings.NewReader(`{"enabled":true,"username":"alice","new_password":"s3cret"}`))
	rec := httptest.NewRecorder()
	authHandler(mgr).handleAuthSettingsPut(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings put status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !mgr.Enabled() {
		t.Fatal("gate should be enabled after the update")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != auth.CookieName || cookies[0].Value == "" {
		t.Fatalf("enabling should issue a session cookie, got %+v", cookies)
	}
}

func TestHandleAuthSettingsDisableClearsSession(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	req := httptest.NewRequest(http.MethodPut, "/api/auth/settings", strings.NewReader(`{"enabled":false}`))
	rec := httptest.NewRecorder()
	authHandler(mgr).handleAuthSettingsPut(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings put status = %d, want 200", rec.Code)
	}
	if mgr.Enabled() {
		t.Fatal("gate should be disabled")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].MaxAge >= 0 {
		t.Fatalf("disabling should clear the cookie, got %+v", cookies)
	}
}

func TestHandleAuthSettingsRequiresCurrentPassword(t *testing.T) {
	mgr := newTestAuthManager(t, true)
	req := httptest.NewRequest(http.MethodPut, "/api/auth/settings", strings.NewReader(`{"new_password":"newpass"}`))
	rec := httptest.NewRecorder()
	authHandler(mgr).handleAuthSettingsPut(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("password change without current password status = %d, want 401", rec.Code)
	}
	if !mgr.VerifyPassword("alice", "s3cret") {
		t.Fatal("password must not change without the current password")
	}
}
