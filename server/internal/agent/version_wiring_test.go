package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"testing"
	"time"
)

// stubNpmRegistry points the update check at a local server and clears the cache.
func stubNpmRegistry(t *testing.T, version string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
	}))
	t.Cleanup(server.Close)
	previous := npmRegistryBaseURL
	npmRegistryBaseURL = server.URL
	t.Cleanup(func() { npmRegistryBaseURL = previous })
	npmVersionMu.Lock()
	npmVersionCache = map[string]npmVersionEntry{}
	npmVersionMu.Unlock()
}

func writeAgentStub(t *testing.T, dir, name, version string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho '" + name + " " + version + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
	return path
}

func waitForUpdateAvailable(t *testing.T, p *Prober, name string) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, st := range p.GetConfiguredStatuses() {
			if st.Name == name && st.UpdateAvailable {
				return st
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	statuses := p.GetConfiguredStatuses()
	for _, st := range statuses {
		if st.Name == name {
			t.Fatalf("agent %s never reported an update: version=%q latest=%q update=%v", name, st.Version, st.LatestVersion, st.UpdateAvailable)
		}
	}
	t.Fatalf("agent %s missing from statuses", name)
	return Status{}
}

// The install probe must publish the version and the update check must run for
// npm-managed agents, otherwise the UI cannot offer an in-place update.
func TestProbeInstallOnlyPublishesVersionAndUpdateAvailability(t *testing.T) {
	dir := t.TempDir()
	command := writeAgentStub(t, dir, "fake-agent", "1.0.0")
	stubNpmRegistry(t, "1.2.0")

	def := Definition{
		Name:            "fake",
		Command:         command,
		InstallCommands: LifecycleCommands{"npm install -g fake-agent@latest"},
	}
	pool := NewPool(Config{Agents: []Definition{def}})
	prober := NewProber(&Config{Agents: []Definition{def}}, pool, time.Minute)

	prober.probeInstallOnly([]Definition{def})

	status := waitForUpdateAvailable(t, prober, "fake")
	if status.Version != "1.0.0" {
		t.Fatalf("Version = %q, want 1.0.0", status.Version)
	}
	if status.LatestVersion != "1.2.0" {
		t.Fatalf("LatestVersion = %q, want 1.2.0", status.LatestVersion)
	}
	if status.VersionCheckedAt.IsZero() {
		t.Fatal("VersionCheckedAt should be recorded")
	}
}

// An up-to-date agent must not be flagged, and the version must still be shown.
func TestProbeInstallOnlySkipsUpdateWhenCurrent(t *testing.T) {
	dir := t.TempDir()
	command := writeAgentStub(t, dir, "current-agent", "2.0.0")
	stubNpmRegistry(t, "2.0.0")

	def := Definition{
		Name:            "current",
		Command:         command,
		InstallCommands: LifecycleCommands{"npm install -g current-agent@latest"},
	}
	pool := NewPool(Config{Agents: []Definition{def}})
	prober := NewProber(&Config{Agents: []Definition{def}}, pool, time.Minute)

	prober.probeInstallOnly([]Definition{def})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, st := range prober.GetConfiguredStatuses() {
			if st.Name == "current" && st.LatestVersion == "2.0.0" {
				if st.UpdateAvailable {
					t.Fatal("UpdateAvailable = true for an up-to-date agent")
				}
				if st.Version != "2.0.0" {
					t.Fatalf("Version = %q, want 2.0.0", st.Version)
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("latest version was never published")
}

// Agents installed by a non-npm installer must not trigger a registry lookup.
func TestProbeInstallOnlySkipsRegistryForNonNpmAgents(t *testing.T) {
	dir := t.TempDir()
	command := writeAgentStub(t, dir, "curl-agent", "3.0.0")

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "9.9.9"})
	}))
	defer server.Close()
	previous := npmRegistryBaseURL
	npmRegistryBaseURL = server.URL
	defer func() { npmRegistryBaseURL = previous }()
	npmVersionMu.Lock()
	npmVersionCache = map[string]npmVersionEntry{}
	npmVersionMu.Unlock()

	def := Definition{
		Name:            "curl",
		Command:         command,
		InstallCommands: LifecycleCommands{"curl -fsSL https://example.com/install.sh | sh"},
	}
	pool := NewPool(Config{Agents: []Definition{def}})
	prober := NewProber(&Config{Agents: []Definition{def}}, pool, time.Minute)

	prober.probeInstallOnly([]Definition{def})
	time.Sleep(300 * time.Millisecond)

	if requests != 0 {
		t.Fatalf("registry was queried %d times for a curl-installed agent", requests)
	}
	statuses := prober.GetConfiguredStatuses()
	if len(statuses) != 1 || statuses[0].Version != "3.0.0" {
		t.Fatalf("statuses = %#v, want version 3.0.0", statuses)
	}
}

// A runtime probe must not wipe the version the install pass resolved, so
// probeInstalledAgents carries the known version forward.
func TestProbeInstalledAgentsKeepsVersionFromInstallPass(t *testing.T) {
	dir := t.TempDir()
	command := writeAgentStub(t, dir, "keep-agent", "4.5.6")

	def := Definition{Name: "keep", Command: command}
	pool := NewPool(Config{Agents: []Definition{def}})
	prober := NewProber(&Config{Agents: []Definition{def}}, pool, time.Minute)

	// The install pass resolves and publishes the version.
	prober.probeInstallOnly([]Definition{def})
	if got := prober.currentVersion("keep"); got != "4.5.6" {
		t.Fatalf("currentVersion after install pass = %q, want 4.5.6", got)
	}

	// A runtime probe replaces the whole status; the version must survive it.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	prober.mu.Lock()
	prober.statuses["keep"] = unavailableStatus("keep", true, "probe pending", time.Now().UTC())
	prober.mu.Unlock()

	kept := firstNonEmptyValue(prober.currentVersion("keep"), detectAgentVersion(def))
	if kept != "4.5.6" {
		t.Fatalf("carried version = %q, want 4.5.6", kept)
	}
	_ = ctx
}
