package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionIsNewer(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    bool
	}{
		{"0.85.1", "0.87.1", true},
		{"0.85.1", "0.85.1", false},
		{"0.87.1", "0.85.1", false},
		{"1.0.0", "1.0.1", true},
		{"1.0", "1.0.0", false},
		{"1.0.0", "1.0", false},
		{"1.2.3", "1.3.0", true},
		{"2.1.211", "2.1.212", true},
		{"", "1.0.0", false},
		{"1.0.0", "", false},
		{"unknown", "1.0.0", false},
		{"v1.0.0", "1.0.1", true},
	}
	for _, tc := range cases {
		if got := versionIsNewer(tc.current, tc.latest); got != tc.want {
			t.Errorf("versionIsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestNpmPackageFromInstallCommands(t *testing.T) {
	cases := []struct {
		commands LifecycleCommands
		want     string
	}{
		{LifecycleCommands{"npm install -g pi-acp@latest"}, "pi-acp"},
		{LifecycleCommands{"npm install -g @earendil-works/pi-coding-agent@latest"}, "@earendil-works/pi-coding-agent"},
		{LifecycleCommands{"npm i -g foo"}, "foo"},
		{LifecycleCommands{"curl -fsSL https://example.com/install.sh | sh"}, ""},
		{LifecycleCommands{}, ""},
		{LifecycleCommands{"npm install -g pi-acp@latest", "npm install -g other"}, "pi-acp"},
	}
	for _, tc := range cases {
		if got := npmPackageFromInstallCommands(tc.commands); got != tc.want {
			t.Errorf("npmPackageFromInstallCommands(%v) = %q, want %q", tc.commands, got, tc.want)
		}
	}
}

func TestLatestNpmVersionFetchesAndCaches(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/test-pkg/latest" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "4.5.6"})
	}))
	defer server.Close()

	previous := npmRegistryBaseURL
	npmRegistryBaseURL = server.URL
	defer func() { npmRegistryBaseURL = previous }()
	npmVersionMu.Lock()
	npmVersionCache = map[string]npmVersionEntry{}
	npmVersionMu.Unlock()

	if got := latestNpmVersion("test-pkg"); got != "4.5.6" {
		t.Fatalf("latestNpmVersion = %q, want 4.5.6", got)
	}
	if got := latestNpmVersion("test-pkg"); got != "4.5.6" {
		t.Fatalf("cached latestNpmVersion = %q, want 4.5.6", got)
	}
	if requests != 1 {
		t.Fatalf("registry requests = %d, want 1 (second call must be cached)", requests)
	}
}

func TestLatestNpmVersionHandlesRegistryFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	previous := npmRegistryBaseURL
	npmRegistryBaseURL = server.URL
	defer func() { npmRegistryBaseURL = previous }()
	npmVersionMu.Lock()
	npmVersionCache = map[string]npmVersionEntry{}
	npmVersionMu.Unlock()

	if got := latestNpmVersion("broken-pkg"); got != "" {
		t.Fatalf("latestNpmVersion = %q, want empty on failure", got)
	}
	// A failed lookup must not be cached as "no update available".
	npmVersionMu.Lock()
	_, cached := npmVersionCache["broken-pkg"]
	npmVersionMu.Unlock()
	if cached {
		t.Fatal("failed lookup should not populate the cache")
	}
}

func TestLatestNpmVersionEmptyPackage(t *testing.T) {
	if got := latestNpmVersion("  "); got != "" {
		t.Fatalf("latestNpmVersion(blank) = %q, want empty", got)
	}
}
