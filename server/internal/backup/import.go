package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"mindfs/server/internal/backupbundle"
	"mindfs/server/internal/fs"
)

const (
	importTTL        = 30 * time.Minute
	importMaxMembers = 200000
	importMaxPayload = 8 << 30 // 8 GiB staged payload ceiling

	statusNew         = "new"
	statusSamePath    = "exists_same_path"
	statusPathMissing = "path_missing_on_disk"

	actionSkip    = "skip"
	actionMerge   = "merge"
	actionReplace = "replace"
)

// stagedImport holds one uploaded bundle between preview and apply.
type stagedImport struct {
	token     string
	dir       string // staging root: payload.tar.gz + extracted/
	manifest  Manifest
	createdAt time.Time
}

// PreviewRoot is one root row in the preview response.
type PreviewRoot struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	RootPath     string `json:"root_path"`
	MetaLocation string `json:"meta_location"`
	Status       string `json:"status"` // new | exists_same_path | path_missing_on_disk
	FileCount    int    `json:"file_count"`
	Bytes        int64  `json:"bytes"`
	HasTasks     bool   `json:"has_tasks"`
}

// Preview is the phase-1 response.
type Preview struct {
	Token         string         `json:"token"`
	CreatedAt     time.Time      `json:"created_at"`
	MindfsVersion string         `json:"mindfs_version,omitempty"`
	Options       ManifestOpts   `json:"options"`
	Roots         []PreviewRoot  `json:"roots"`
	Global        ManifestGlobal `json:"global"`
	Warnings      []string       `json:"warnings,omitempty"`
}

// ApplyRequest is the phase-2 input.
type ApplyRequest struct {
	Token                 string      `json:"token"`
	Roots                 []ApplyRoot `json:"roots"`
	Global                string      `json:"global"` // skip | replace
	IncludeDeviceIdentity bool        `json:"include_device_identity"`
}

// ApplyRoot selects the action for one bundled root.
type ApplyRoot struct {
	ID           string `json:"id"`
	Action       string `json:"action"` // skip | merge | replace
	PathOverride string `json:"path_override,omitempty"`
}

// ApplyResult summarizes what happened.
type ApplyResult struct {
	Roots               []ApplyRootResult `json:"roots"`
	GlobalFilesReplaced int               `json:"global_files_replaced"`
	GlobalSkipped       bool              `json:"global_skipped"`
	Warnings            []string          `json:"warnings,omitempty"`
	ReloadNeeded        bool              `json:"reload_needed"`
}

// ApplyRootResult reports per-root counters.
type ApplyRootResult struct {
	ID             string `json:"id"`
	Action         string `json:"action"`
	RootPath       string `json:"root_path"`
	FilesCopied    int    `json:"files_copied"`
	SessionsMerged int    `json:"sessions_merged"`
	Registered     bool   `json:"registered"`
	PathOverridden bool   `json:"path_overridden"`
}

var (
	ErrBadToken       = errors.New("unknown or expired import token")
	ErrInvalidRequest = errors.New("invalid apply request")
)

// Import decrypts an uploaded bundle, extracts it to a staging dir, and
// returns the preview. The staging survives until apply or TTL expiry.
func (m *Manager) Import(src io.Reader, passphrase string) (Preview, error) {
	stage, err := m.tempDir("mindfs-import-")
	if err != nil {
		return Preview{}, errf("create staging: %w", err)
	}
	fail := func(err error) (Preview, error) {
		os.RemoveAll(stage)
		return Preview{}, err
	}

	payloadPath := filepath.Join(stage, "bundle.mbk")
	if err := copyToFile(src, payloadPath, importMaxPayload); err != nil {
		return fail(errf("store upload: %w", err))
	}
	file, err := os.Open(payloadPath)
	if err != nil {
		return fail(errf("open upload: %w", err))
	}
	rc, err := backupbundle.Open(file, passphrase)
	if err != nil {
		file.Close()
		return fail(errf("%w", err))
	}

	decrypted := filepath.Join(stage, "payload.tar.gz")
	if err := copyToFile(rc, decrypted, importMaxPayload); err != nil {
		rc.Close()
		file.Close()
		return fail(errf("decrypt: %w", err))
	}
	rc.Close()
	file.Close()
	os.Remove(payloadPath)

	extractDir := filepath.Join(stage, "x")
	manifest, err := extractBundle(decrypted, extractDir)
	if err != nil {
		return fail(err)
	}

	token := newToken()
	staged := &stagedImport{token: token, dir: stage, manifest: *manifest, createdAt: m.nowUTC()}
	m.mu.Lock()
	m.expireLocked()
	m.imports[token] = staged
	m.mu.Unlock()

	return m.buildPreview(staged), nil
}

func (m *Manager) buildPreview(staged *stagedImport) Preview {
	preview := Preview{
		Token:         staged.token,
		CreatedAt:     staged.manifest.CreatedAt,
		MindfsVersion: staged.manifest.MindfsVersion,
		Options:       staged.manifest.Options,
		Global:        staged.manifest.Global,
		Warnings:      append([]string{}, staged.manifest.Warnings...),
	}
	existing := map[string]fs.RootInfo{}
	for _, root := range m.listRoots() {
		existing[root.ID] = root
	}
	for _, root := range staged.manifest.Roots {
		row := PreviewRoot{
			ID:           root.ID,
			Name:         root.Name,
			RootPath:     root.RootPath,
			MetaLocation: root.MetaLocation,
			FileCount:    root.FileCount,
			Bytes:        root.Bytes,
			HasTasks:     root.HasTasks,
		}
		switch {
		case existing[root.ID].ID == root.ID && samePath(existing[root.ID].RootPath, root.RootPath):
			row.Status = statusSamePath
		case dirExists(root.RootPath):
			row.Status = statusNew
		default:
			row.Status = statusPathMissing
		}
		preview.Roots = append(preview.Roots, row)
	}
	return preview
}

// Apply restores selected sections from a staged import.
func (m *Manager) Apply(req ApplyRequest) (ApplyResult, error) {
	result := ApplyResult{}
	m.mu.Lock()
	staged, ok := m.imports[req.Token]
	if ok && m.nowUTC().Sub(staged.createdAt) > importTTL {
		ok = false
		os.RemoveAll(staged.dir)
		delete(m.imports, req.Token)
	}
	m.mu.Unlock()
	if !ok {
		return result, ErrBadToken
	}
	defer func() {
		m.mu.Lock()
		if cur, still := m.imports[req.Token]; still && cur == staged {
			delete(m.imports, req.Token)
		}
		m.mu.Unlock()
		os.RemoveAll(staged.dir)
	}()

	globalAction := strings.TrimSpace(req.Global)
	if globalAction == "" {
		globalAction = actionSkip
	}
	if globalAction != actionSkip && globalAction != actionReplace {
		return result, fmt.Errorf("%w: bad global action %q", ErrInvalidRequest, req.Global)
	}
	byID := map[string]ManifestRoot{}
	for _, root := range staged.manifest.Roots {
		byID[root.ID] = root
	}
	requested := map[string]ApplyRoot{}
	for _, rr := range req.Roots {
		if _, ok := byID[rr.ID]; !ok {
			return result, fmt.Errorf("%w: unknown root %q", ErrInvalidRequest, rr.ID)
		}
		requested[rr.ID] = rr
	}

	configDir, err := m.resolveConfigDir()
	if err != nil {
		return result, err
	}

	var invalidatedRoots []string
	reloadGlobal := false

	// Roots first (registration may be needed), then global config.
	for _, bundled := range staged.manifest.Roots {
		rr, ok := requested[bundled.ID]
		if !ok || rr.Action == actionSkip {
			result.Roots = append(result.Roots, ApplyRootResult{ID: bundled.ID, Action: actionSkip, RootPath: bundled.RootPath})
			continue
		}
		if rr.Action != actionMerge && rr.Action != actionReplace {
			return result, fmt.Errorf("%w: bad action %q", ErrInvalidRequest, rr.Action)
		}
		res, err := m.applyRoot(staged, bundled, rr)
		if err != nil {
			return result, err
		}
		result.Roots = append(result.Roots, res)
		invalidatedRoots = append(invalidatedRoots, res.ID)
	}

	if globalAction == actionReplace {
		count, err := applyGlobal(filepath.Join(staged.dir, "x"), configDir, req.IncludeDeviceIdentity)
		if err != nil {
			return result, err
		}
		result.GlobalFilesReplaced = count
		reloadGlobal = true
		if _, err := os.Stat(filepath.Join(configDir, "ssh-servers.json")); err == nil {
			result.Warnings = append(result.Warnings, "ssh store restored; rematerialization scheduled")
		}
	} else {
		result.GlobalSkipped = true
	}

	if m.invalidator != nil {
		for _, rootID := range invalidatedRoots {
			m.invalidator.InvalidateRootContext(rootID)
		}
		if reloadGlobal {
			m.invalidator.InvalidateGlobalStores()
		}
	}
	result.ReloadNeeded = len(invalidatedRoots) > 0 || reloadGlobal
	return result, nil
}

func (m *Manager) applyRoot(staged *stagedImport, bundled ManifestRoot, rr ApplyRoot) (ApplyRootResult, error) {
	res := ApplyRootResult{ID: bundled.ID, Action: rr.Action, RootPath: bundled.RootPath, PathOverridden: strings.TrimSpace(rr.PathOverride) != ""}

	rootPath := bundled.RootPath
	if res.PathOverridden {
		rootPath = filepath.Clean(strings.TrimSpace(rr.PathOverride))
		if !dirExists(rootPath) {
			return res, fmt.Errorf("%w: path_override %q does not exist", ErrInvalidRequest, rootPath)
		}
	}
	if !dirExists(rootPath) {
		return res, fmt.Errorf("%w: root path %q missing on disk (use path_override)", ErrInvalidRequest, bundled.RootPath)
	}

	var root fs.RootInfo
	if m.registry != nil {
		registered, err := m.registry.UpsertWithMetaLocation(rootPath, bundled.MetaLocation)
		if err != nil {
			return res, errf("register root %s: %w", rootPath, err)
		}
		root = registered
		res.Registered = true
		res.RootPath = registered.RootPath
	} else {
		root = fs.NewRootInfo(bundled.ID, bundled.Name, rootPath)
	}
	res.ID = root.ID

	metaDir := root.MetaDir()
	if metaDir == "" {
		return res, errf("root %s: metadata directory unavailable", root.ID)
	}
	stagedRootDir := filepath.Join(staged.dir, "x", rootsPrefix, bundled.ID)

	if rr.Action == actionReplace {
		count, err := copyTree(stagedRootDir, metaDir, nil)
		if err != nil {
			return res, errf("replace root %s: %w", root.ID, err)
		}
		res.FilesCopied = count
		return res, nil
	}

	// merge
	copied, err := copyTree(stagedRootDir, metaDir, func(rel string) bool {
		slash := filepath.ToSlash(rel)
		if strings.HasPrefix(slash, "sessions/") && strings.HasSuffix(slash, ".jsonl") {
			return true
		}
		return strings.HasPrefix(slash, "upload/")
	})
	if err != nil {
		return res, errf("merge root %s: %w", root.ID, err)
	}
	res.FilesCopied = copied
	merged, err := mergeSessionDB(filepath.Join(stagedRootDir, "sessions", "session-list.db"), filepath.Join(metaDir, "sessions", "session-list.db"))
	if err != nil {
		return ApplyRootResult{}, err
	}
	res.SessionsMerged = merged
	return res, nil
}

// mergeSessionDB inserts rows from the staged session db into the target db
// with conflict-skip semantics. A missing staged db is a no-op; a missing
// target db simply receives a copy of the staged one.
func mergeSessionDB(stagedDB, targetDB string) (int, error) {
	if _, err := os.Stat(stagedDB); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(targetDB), 0o700); err != nil {
		return 0, errf("create sessions dir: %w", err)
	}
	if _, err := os.Stat(targetDB); err != nil {
		if os.IsNotExist(err) {
			if err := copyFile0600(stagedDB, targetDB); err != nil {
				return 0, errf("seed session db: %w", err)
			}
			return countSessions(targetDB)
		}
		return 0, err
	}

	db, err := sql.Open("sqlite", targetDB)
	if err != nil {
		return 0, errf("open session db: %w", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), "ATTACH DATABASE ? AS staged", stagedDB); err != nil {
		return 0, errf("attach staged db: %w", err)
	}
	defer db.Exec("DETACH DATABASE staged")

	sessionsCols := "key, type, parent_session_key, parent_tool_call_id, source, task_id, model, agent, mode, effort, fast_service, shell, plan_mode, name, related_files_json, related_worktree_json, last_context_window_total_tokens, last_context_window_model_context_window, pinned_at, created_at, updated_at, closed_at"
	bindingsCols := "session_key, agent, agent_session_id, agent_ctx_seq, external_source_path, external_source_offset, external_source_mtime_ns"

	for _, table := range []struct{ name, cols, conflict string }{
		{"sessions", sessionsCols, "m.key = t.key"},
		{"session_agent_bindings", bindingsCols, "m.session_key = t.session_key AND m.agent = t.agent"},
	} {
		if !tableExists(db, "staged", table.name) || !tableExists(db, "main", table.name) {
			continue
		}
		stmt := fmt.Sprintf("INSERT INTO main.%s (%s) SELECT %s FROM staged.%s t WHERE NOT EXISTS (SELECT 1 FROM main.%s m WHERE %s)", table.name, table.cols, table.cols, table.name, table.name, table.conflict)
		if _, err := db.Exec(stmt); err != nil {
			return 0, errf("merge %s: %w (bundle schema may differ; try replace)", table.name, err)
		}
	}
	return countSessions(targetDB)
}

func tableExists(db *sql.DB, schema, name string) bool {
	var found string
	err := db.QueryRow(fmt.Sprintf("SELECT name FROM %s.sqlite_master WHERE type='table' AND name='%s'", schema, name)).Scan(&found)
	return err == nil && found == name
}

func countSessions(dbPath string) (int, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return 0, errf("open session db: %w", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n); err != nil {
		return 0, nil // table missing — nothing merged
	}
	return n, nil
}

// applyGlobal rolls staged global files into the config dir with .bak
// rollovers, gating device identity.
func applyGlobal(extractDir, configDir string, includeDeviceIdentity bool) (int, error) {
	stagedGlobal := filepath.Join(extractDir, globalPrefix)
	if !dirExists(stagedGlobal) {
		return 0, nil
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return 0, errf("create config dir: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	count := 0
	err := filepath.WalkDir(stagedGlobal, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return errf("walk staged global: %w", err)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(stagedGlobal, path)
		if err != nil {
			return err
		}
		base := filepath.Base(path)
		if deviceIdentityFiles[base] && !includeDeviceIdentity {
			return nil
		}
		target := filepath.Join(configDir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return errf("mkdir: %w", err)
		}
		if _, err := os.Lstat(target); err == nil {
			if err := os.Rename(target, target+bakSuffixSep+stamp); err != nil {
				return errf("rollover %s: %w", target, err)
			}
		}
		if err := copyFile0600(path, target); err != nil {
			return errf("restore %s: %w", target, err)
		}
		count++
		return nil
	})
	return count, err
}

// copyTree copies files from src into dst. When filter is non-nil only
// relative paths it approves are copied; approved targets are skipped when
// they already exist.
func copyTree(src, dst string, filter func(rel string) bool) (int, error) {
	if !dirExists(src) {
		return 0, nil
	}
	count := 0
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return errf("walk staged root: %w", err)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if filter != nil && !filter(rel) {
			return nil
		}
		target := filepath.Join(dst, rel)
		if _, err := os.Lstat(target); err == nil {
			return nil // keep existing
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return errf("mkdir: %w", err)
		}
		if err := copyFile0600(path, target); err != nil {
			return errf("copy %s: %w", rel, err)
		}
		count++
		return nil
	})
	return count, err
}

// extractBundle safely unpacks the decrypted payload and parses the manifest.
func extractBundle(archivePath, destDir string) (*Manifest, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, errf("open payload: %w", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, errf("gunzip: %w", err)
	}
	defer gz.Close()

	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return nil, errf("mkdir staging: %w", err)
	}
	tr := tar.NewReader(gz)
	members := 0
	var manifest *Manifest
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errf("tar read: %w", err)
		}
		members++
		if members > importMaxMembers {
			return nil, errf("too many archive members")
		}
		name := filepath.ToSlash(filepath.Clean(hdr.Name))
		if strings.HasPrefix(name, "/") || strings.Contains(name, "../") || strings.Contains(name, "..\\") {
			return nil, errf("unsafe archive member %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(filepath.Join(destDir, filepath.FromSlash(name)), 0o700); err != nil {
				return nil, errf("mkdir member: %w", err)
			}
			continue
		case tar.TypeReg:
			if hdr.Size > maxFileBytes {
				return nil, errf("member %s too large", name)
			}
		default:
			return nil, errf("unsupported archive member %q", hdr.Name)
		}
		if name == "manifest.json" {
			payload, err := io.ReadAll(io.LimitReader(tr, 1<<20))
			if err != nil {
				return nil, errf("read manifest: %w", err)
			}
			var mf Manifest
			if err := json.Unmarshal(payload, &mf); err != nil {
				return nil, errf("parse manifest: %w", err)
			}
			manifest = &mf
			continue
		}
		target := filepath.Join(destDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return nil, errf("mkdir member: %w", err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, errf("create member: %w", err)
		}
		if _, err := io.Copy(out, io.LimitReader(tr, maxFileBytes+1)); err != nil {
			out.Close()
			return nil, errf("write member: %w", err)
		}
		out.Close()
	}
	if manifest == nil {
		return nil, errf("bundle has no manifest.json")
	}
	return manifest, nil
}

// expireLocked drops expired staging dirs; caller holds m.mu.
func (m *Manager) expireLocked() {
	now := m.nowUTC()
	for token, staged := range m.imports {
		if now.Sub(staged.createdAt) > importTTL {
			os.RemoveAll(staged.dir)
			delete(m.imports, token)
		}
	}
}

func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func samePath(a, b string) bool {
	return filepath.Clean(strings.TrimSpace(a)) == filepath.Clean(strings.TrimSpace(b))
}

func copyToFile(src io.Reader, dst string, limit int64) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, io.LimitReader(src, limit+1))
	if err == nil {
		if fi, statErr := out.Stat(); statErr == nil && fi.Size() > limit {
			return fmt.Errorf("stream exceeds %d bytes", limit)
		}
	}
	return err
}
