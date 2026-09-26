package agent

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// npmRegistryBaseURL is a variable so tests can point it at a stub server.
var npmRegistryBaseURL = "https://registry.npmjs.org"

const (
	npmVersionTimeout = 8 * time.Second
	npmVersionTTL     = 6 * time.Hour
	// maxNpmVersionBodyBytes bounds a registry response so a hostile or broken
	// endpoint cannot exhaust memory.
	maxNpmVersionBodyBytes = 1 << 20
)

type npmVersionEntry struct {
	version   string
	fetchedAt time.Time
}

var (
	npmVersionMu    sync.Mutex
	npmVersionCache = map[string]npmVersionEntry{}
)

// npmPackageFromInstallCommands extracts the npm package name an agent declares
// for installation, so update checks only run for npm-managed agents.
func npmPackageFromInstallCommands(commands LifecycleCommands) string {
	for _, command := range commands {
		match := npmPackagePattern.FindStringSubmatch(command)
		if len(match) != 2 {
			continue
		}
		name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(match[1]), "@latest"))
		if name != "" {
			return name
		}
	}
	return ""
}

// latestNpmVersion returns the newest published version of a package, cached for
// npmVersionTTL. It returns an empty string when the registry is unreachable;
// callers treat that as "unknown" rather than "up to date".
func latestNpmVersion(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return ""
	}
	now := time.Now()

	npmVersionMu.Lock()
	if entry, ok := npmVersionCache[pkg]; ok && now.Sub(entry.fetchedAt) < npmVersionTTL {
		npmVersionMu.Unlock()
		return entry.version
	}
	npmVersionMu.Unlock()

	version := fetchLatestNpmVersion(pkg)
	if version == "" {
		return ""
	}

	npmVersionMu.Lock()
	npmVersionCache[pkg] = npmVersionEntry{version: version, fetchedAt: now}
	npmVersionMu.Unlock()
	return version
}

func fetchLatestNpmVersion(pkg string) string {
	ctx, cancel := newNpmRequestContext()
	defer cancel()

	endpoint := strings.TrimSuffix(npmRegistryBaseURL, "/") + "/" + pkg + "/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[agent/version] npm.latest.error package=%s err=%v", pkg, err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxNpmVersionBodyBytes))
	if err != nil {
		return ""
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Version)
}

func newNpmRequestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), npmVersionTimeout)
}

// versionIsNewer reports whether latest is a strictly greater dotted numeric
// version than current. Non-numeric parts fall back to inequality.
func versionIsNewer(current, latest string) bool {
	current = strings.TrimSpace(strings.TrimPrefix(current, "v"))
	latest = strings.TrimSpace(strings.TrimPrefix(latest, "v"))
	if current == "" || latest == "" {
		return false
	}
	if current == latest {
		return false
	}
	currentParts, okCurrent := parseNumericVersion(current)
	latestParts, okLatest := parseNumericVersion(latest)
	if !okCurrent || !okLatest {
		return false
	}
	for i := 0; i < len(currentParts) || i < len(latestParts); i++ {
		var a, b int
		if i < len(currentParts) {
			a = currentParts[i]
		}
		if i < len(latestParts) {
			b = latestParts[i]
		}
		if b != a {
			return b > a
		}
	}
	return false
}

var numericVersionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

func parseNumericVersion(value string) ([]int, bool) {
	if !numericVersionPattern.MatchString(value) {
		return nil, false
	}
	fields := strings.Split(value, ".")
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		number := 0
		for _, r := range field {
			number = number*10 + int(r-'0')
		}
		parts = append(parts, number)
	}
	return parts, true
}
