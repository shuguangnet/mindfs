package sshops

import (
	"fmt"
	"strconv"
	"strings"
)

// SSHConfigEntry is one parsed Host block of an OpenSSH client config.
type SSHConfigEntry struct {
	Host         string   `json:"host"`
	Patterns     []string `json:"patterns"`
	HostName     string   `json:"host_name,omitempty"`
	Port         int      `json:"port,omitempty"`
	User         string   `json:"user,omitempty"`
	IdentityFile string   `json:"identity_file,omitempty"`
	ProxyJump    string   `json:"proxy_jump,omitempty"`
}

// ParseSSHConfigResult carries parsed entries plus skipped-pattern notes.
type ParseSSHConfigResult struct {
	Entries           []SSHConfigEntry `json:"entries"`
	SkippedPatterns   []string         `json:"skipped_patterns,omitempty"`
	SkippedMatches    int              `json:"skipped_matches"`
	SkippedNoHostName []string         `json:"skipped_no_hostname,omitempty"`
}

// ParseSSHConfig parses Host blocks from an OpenSSH client configuration.
// Match blocks and Host patterns with wildcards/negations are skipped and
// reported. For repeated directives the first value wins, mirroring ssh.
func ParseSSHConfig(data string) ParseSSHConfigResult {
	result := ParseSSHConfigResult{Entries: []SSHConfigEntry{}}
	skippingMatch := false
	var current *SSHConfigEntry

	flush := func() {
		if current != nil {
			result.Entries = append(result.Entries, *current)
			current = nil
		}
	}

	lines := strings.Split(data, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := splitConfigLine(line)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "host":
			flush()
			skippingMatch = false
			patterns := strings.Fields(value)
			usable := make([]string, 0, len(patterns))
			for _, p := range patterns {
				if strings.ContainsAny(p, "*?!") || strings.HasPrefix(p, "-") {
					result.SkippedPatterns = append(result.SkippedPatterns, p)
				} else {
					usable = append(usable, p)
				}
			}
			if len(usable) != 1 {
				if len(usable) == 0 {
					continue
				}
				// Multiple simple patterns: ssh matches any of them; import
				// them as one entry named after the first pattern.
				result.SkippedPatterns = append(result.SkippedPatterns, strings.Join(usable[1:], " "))
			}
			current = &SSHConfigEntry{Host: usable[0], Patterns: usable}
		case "match":
			flush()
			skippingMatch = true
			result.SkippedMatches++
		default:
			if skippingMatch || current == nil {
				continue
			}
			applyConfigDirective(current, strings.ToLower(key), value)
		}
	}
	flush()

	// Drop entries without HostName (pure pattern groupings).
	kept := result.Entries[:0]
	for _, entry := range result.Entries {
		if entry.HostName == "" {
			result.SkippedNoHostName = append(result.SkippedNoHostName, entry.Host)
			continue
		}
		kept = append(kept, entry)
	}
	result.Entries = kept
	return result
}

// splitConfigLine splits "Key value" or "Key=value" forms, stripping an
// optional trailing comment from the value.
func splitConfigLine(line string) (string, string, bool) {
	sep := strings.IndexAny(line, " \t=")
	if sep < 0 {
		return strings.ToLower(line), "", true
	}
	key := strings.TrimSpace(line[:sep])
	value := strings.TrimSpace(line[sep+1:])
	value = strings.Trim(value, `"`)
	if idx := strings.Index(value, " #"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	if value == "" {
		return "", "", false
	}
	return key, value, true
}

func applyConfigDirective(entry *SSHConfigEntry, key, value string) {
	switch key {
	case "hostname":
		if entry.HostName == "" {
			entry.HostName = value
		}
	case "port":
		if entry.Port == 0 {
			if port, err := strconv.Atoi(value); err == nil && port > 0 && port < 65536 {
				entry.Port = port
			}
		}
	case "user":
		if entry.User == "" {
			entry.User = value
		}
	case "identityfile":
		if entry.IdentityFile == "" {
			entry.IdentityFile = value
		}
	case "proxyjump":
		if entry.ProxyJump == "" {
			entry.ProxyJump = value
		}
	}
}

// configHint returns an error iff the parsed entry cannot be imported.
func (e SSHConfigEntry) configHint() error {
	err := Validate(Server{
		Alias: NormalizeAlias(e.Host), Host: e.HostName, Port: e.Port,
		User: e.User, Auth: AuthPassword, Enabled: true,
	})
	if err != nil {
		return fmt.Errorf("host %q: %w", e.Host, err)
	}
	return nil
}
