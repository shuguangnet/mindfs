// Package piexport converts MindFS sessions into native pi session files
// (format version 3) so exported conversations can be resumed with the plain
// `pi` CLI and, when the agent binding id is reused, by MindFS itself.
package piexport

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	agentDirEnv    = "PI_CODING_AGENT_DIR"
	sessionDirEnv  = "PI_CODING_AGENT_SESSION_DIR"
	sessionVersion = 3
	entryIDLen     = 8 // pi uses 8 hex chars for entry ids
)

// Exchange is one user or assistant turn.
type Exchange struct {
	Role      string // "user" or "assistant"
	Content   string
	Timestamp time.Time
}

// Session describes the input for one export.
type Session struct {
	Cwd       string // absolute project path used by the session
	SessionID string // optional; a UUIDv7 is generated when empty
	CreatedAt time.Time
	Exchanges []Exchange
}

// Result reports where the session landed.
type Result struct {
	SessionID string
	Dir       string // absolute pi sessions subdirectory for the cwd
	Path      string // absolute jsonl path (empty before writing)
	Existed   bool   // a file with the same session id was already present
}

// AgentSessionDir resolves pi's session directory the same way pi does:
// $PI_CODING_AGENT_SESSION_DIR, else <agentDir>/sessions where agentDir is
// $PI_CODING_AGENT_DIR or ~/.pi/agent.
func AgentSessionDir() (string, error) {
	if env := strings.TrimSpace(os.Getenv(sessionDirEnv)); env != "" {
		return filepath.Clean(expandHome(env)), nil
	}
	agentDir, err := AgentDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(agentDir, "sessions"), nil
}

// AgentDir resolves pi's agent config directory.
func AgentDir() (string, error) {
	if env := strings.TrimSpace(os.Getenv(agentDirEnv)); env != "" {
		return filepath.Clean(expandHome(env)), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("piexport: home directory unavailable: %w", err)
	}
	return filepath.Join(home, ".pi", "agent"), nil
}

// MungeDirName replicates pi's cwd → directory name rule:
// strip the leading slash/backslash, replace "/", "\", ":" with "-", and wrap
// in "--…--" (e.g. /root/mindfs → --root-mindfs--).
func MungeDirName(cwd string) string {
	resolved := filepath.Clean(strings.TrimSpace(cwd))
	trimmed := strings.TrimPrefix(strings.TrimPrefix(resolved, "/"), "\\")
	replaced := strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(trimmed)
	return "--" + replaced + "--"
}

// NewUUIDv7 returns a lexicographically sortable UUID (RFC 9562 layout)
// matching pi's session id charset.
func NewUUIDv7() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Degrade to v4-shaped randomness; charset stays valid.
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> (uint(i%8) * 8))
		}
	}
	ms := time.Now().UnixMilli()
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = 0x70 | (b[6] & 0x0F) // version 7
	b[8] = 0x80 | (b[8] & 0x3F) // RFC variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Generate renders the session as pi JSONL. It returns the bytes and the
// session id used (input id or generated).
func Generate(s Session) ([]byte, string, error) {
	id := strings.TrimSpace(s.SessionID)
	if id == "" {
		id = NewUUIDv7()
	}
	if err := validateSessionID(id); err != nil {
		return nil, "", err
	}
	createdAt := s.CreatedAt
	if createdAt.IsZero() {
		if len(s.Exchanges) > 0 && !s.Exchanges[0].Timestamp.IsZero() {
			createdAt = s.Exchanges[0].Timestamp
		} else {
			createdAt = time.Now().UTC()
		}
	}
	cwd := filepath.Clean(strings.TrimSpace(s.Cwd))
	if cwd == "" || cwd == "." {
		return nil, "", fmt.Errorf("piexport: cwd required")
	}

	var sb strings.Builder
	writeLine := func(v any) error {
		payload, err := json.Marshal(v)
		if err != nil {
			return err
		}
		sb.Write(payload)
		sb.WriteByte('\n')
		return nil
	}
	if err := writeLine(map[string]any{
		"type":      "session",
		"version":   sessionVersion,
		"id":        id,
		"timestamp": createdAt.UTC().Format(time.RFC3339Nano),
		"cwd":       cwd,
	}); err != nil {
		return nil, "", err
	}

	var parentID any // nil for the first entry
	nextID := func() string {
		var b [4]byte
		_, _ = rand.Read(b[:])
		return hex.EncodeToString(b[:])[:entryIDLen]
	}
	seen := map[string]bool{}
	for _, ex := range s.Exchanges {
		role := strings.TrimSpace(ex.Role)
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(ex.Content)
		if content == "" {
			continue
		}
		entryID := nextID()
		for seen[entryID] {
			entryID = nextID()
		}
		seen[entryID] = true
		ts := ex.Timestamp
		if ts.IsZero() {
			ts = createdAt
		}
		if err := writeLine(map[string]any{
			"type":      "message",
			"id":        entryID,
			"parentId":  parentID,
			"timestamp": ts.UTC().Format(time.RFC3339Nano),
			"message": map[string]any{
				"role": role,
				"content": []map[string]string{
					{"type": "text", "text": content},
				},
			},
		}); err != nil {
			return nil, "", err
		}
		parentID = entryID
	}
	if parentID == nil {
		return nil, "", fmt.Errorf("piexport: no exportable text exchanges")
	}
	return []byte(sb.String()), id, nil
}

// Write generates the session and stores it under pi's sessions directory for
// s.Cwd. It is idempotent per session id: an existing file is left untouched.
func Write(s Session) (Result, error) {
	res := Result{}
	if err := validateForWrite(s); err != nil {
		return res, err
	}
	payload, id, err := Generate(s)
	if err != nil {
		return res, err
	}
	base, err := AgentSessionDir()
	if err != nil {
		return res, err
	}
	dir := filepath.Join(base, MungeDirName(s.Cwd))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return res, fmt.Errorf("piexport: create session dir: %w", err)
	}

	// Idempotency: any existing file for this session id wins.
	matches, _ := filepath.Glob(filepath.Join(dir, "*_"+id+".jsonl"))
	if len(matches) > 0 {
		return Result{SessionID: id, Dir: dir, Path: matches[0], Existed: true}, nil
	}

	name := fileTimestamp(s.CreatedAt, s.Exchanges) + "_" + id + ".jsonl"
	target := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, ".piexport-*")
	if err != nil {
		return res, fmt.Errorf("piexport: temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return res, fmt.Errorf("piexport: write payload: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return res, err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return res, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return res, fmt.Errorf("piexport: rename: %w", err)
	}
	return Result{SessionID: id, Dir: dir, Path: target}, nil
}

// FindExisting returns the file for a session id under the cwd's session dir
// when present.
func FindExisting(cwd, sessionID string) (string, bool) {
	if strings.TrimSpace(sessionID) == "" {
		return "", false
	}
	base, err := AgentSessionDir()
	if err != nil {
		return "", false
	}
	matches, _ := filepath.Glob(filepath.Join(base, MungeDirName(cwd), "*_"+strings.TrimSpace(sessionID)+".jsonl"))
	if len(matches) == 0 {
		return "", false
	}
	return matches[0], true
}

func validateForWrite(s Session) error {
	if strings.TrimSpace(s.Cwd) == "" {
		return fmt.Errorf("piexport: cwd required")
	}
	return nil
}

func validateSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("piexport: session id required")
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("piexport: invalid session id character %q", r)
		}
		if (i == 0 || i == len(id)-1) && (r == '-' || r == '_' || r == '.') {
			return fmt.Errorf("piexport: session id must start and end alphanumeric")
		}
	}
	return nil
}

// fileTimestamp mirrors pi's file naming: RFC3339 with ":" and "." replaced
// by "-".
func fileTimestamp(created time.Time, exchanges []Exchange) string {
	ts := created
	if ts.IsZero() && len(exchanges) > 0 && !exchanges[0].Timestamp.IsZero() {
		ts = exchanges[0].Timestamp
	}
	if ts.IsZero() {
		ts = time.Now()
	}
	stamp := ts.UTC().Format(time.RFC3339Nano)
	return strings.NewReplacer(":", "-", ".", "-").Replace(stamp)
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~"+string(os.PathSeparator)) || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}
