package backup

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"mindfs/server/internal/backupbundle"
	"mindfs/server/internal/config"
	"mindfs/server/internal/fs"
)

const (
	// maxFileBytes caps any single member in the bundle (uploads can be big,
	// but not arbitrarily big).
	maxFileBytes = 512 << 20

	globalPrefix = "global"
	rootsPrefix  = "roots"

	bakSuffixSep = ".bak-"
)

// deviceIdentityFiles are tied to this machine's relay/push identity; they are
// excluded unless the exporter opts in for a whole-machine migration.
var deviceIdentityFiles = map[string]bool{
	"device.json":           true,
	"web-push-vapid.json":   true,
	"local-cli-tokens.json": true,
}

// ExportRequest shapes a backup.
type ExportRequest struct {
	Passphrase            string
	IncludeUploads        bool
	IncludeDeviceIdentity bool
}

// ExportFile is the produced bundle.
type ExportFile struct {
	File     *os.File
	Name     string
	Size     int64
	Manifest Manifest
	cleanup  []string
}

// Cleanup removes staging artifacts (not the bundle file itself — the caller
// owns that and streams it out).
func (e *ExportFile) Cleanup() {
	for _, path := range e.cleanup {
		os.RemoveAll(path)
	}
}

func (m *Manager) resolveConfigDir() (string, error) {
	if m.configDir != "" {
		return m.configDir, nil
	}
	dir, err := config.MindFSConfigDir()
	if err != nil {
		return "", errf("resolve config dir: %w", err)
	}
	return dir, nil
}

func (m *Manager) tempDir(pattern string) (string, error) {
	if m.stageBase != "" {
		return os.MkdirTemp(m.stageBase, pattern)
	}
	return os.MkdirTemp("", pattern)
}

// Export builds the encrypted bundle for the whole instance.
func (m *Manager) Export(req ExportRequest) (*ExportFile, error) {
	if len([]rune(req.Passphrase)) < 8 {
		return nil, errf("%w", backupbundle.ErrShortPassphrase)
	}
	stage, err := m.tempDir("mindfs-export-*")
	if err != nil {
		return nil, errf("create staging: %w", err)
	}
	cleanup := []string{stage}
	fail := func(err error) (*ExportFile, error) {
		for _, p := range cleanup {
			os.RemoveAll(p)
		}
		return nil, err
	}
	configDir, err := m.resolveConfigDir()
	if err != nil {
		return nil, err
	}

	createdAt := m.nowUTC()
	payloadPath := filepath.Join(stage, "payload.tar.gz")
	manifest, err := m.buildPayload(payloadPath, configDir, req, createdAt)
	if err != nil {
		return nil, err
	}

	bundle, err := func() (*os.File, error) {
		payload, oerr := os.Open(payloadPath)
		if oerr != nil {
			return nil, errf("open payload: %w", oerr)
		}
		defer payload.Close()
		return backupbundle.Seal(payload, stage, backupbundle.Options{
			Passphrase:    req.Passphrase,
			MindfsVersion: m.version,
			CreatedAt:     createdAt.Format(time.RFC3339),
		})
	}()
	if err != nil {
		return fail(errf("seal bundle: %w", err))
	}
	info, err := bundle.Stat()
	if err != nil {
		bundle.Close()
		os.Remove(bundle.Name())
		return fail(errf("stat bundle: %w", err))
	}
	return &ExportFile{
		File:     bundle,
		Name:     backupbundle.SuggestFilename(createdAt.Format(time.RFC3339)),
		Size:     info.Size(),
		Manifest: *manifest,
		cleanup:  cleanup,
	}, nil
}

// buildPayload writes the tar.gz payload to path and returns the manifest.
func (m *Manager) buildPayload(path, configDir string, req ExportRequest, createdAt time.Time) (*Manifest, error) {
	payload, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, errf("create payload: %w", err)
	}
	ok := false
	defer func() {
		payload.Close()
		if !ok {
			os.Remove(path)
		}
	}()

	manifest := Manifest{
		Format:        backupbundle.FormatName,
		Version:       backupbundle.FormatVersion,
		CreatedAt:     createdAt,
		MindfsVersion: m.version,
		Options: ManifestOpts{
			IncludeUploads:        req.IncludeUploads,
			IncludeDeviceIdentity: req.IncludeDeviceIdentity,
		},
	}

	gz := gzip.NewWriter(payload)
	tw := tar.NewWriter(gz)

	globalStats, err := m.writeGlobal(tw, configDir, req.IncludeDeviceIdentity)
	if err != nil {
		return nil, err
	}
	manifest.Global = globalStats

	for _, root := range m.listRoots() {
		stats, warns, err := m.writeRoot(tw, root, req.IncludeUploads, filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		stats.ID = root.ID
		stats.Name = root.Name
		stats.RootPath = root.RootPath
		stats.MetaLocation = root.EffectiveMetaLocation()
		manifest.Roots = append(manifest.Roots, stats)
		manifest.Warnings = append(manifest.Warnings, warns...)
	}

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, errf("marshal manifest: %w", err)
	}
	if err := writeTarBytes(tw, "manifest.json", manifestJSON); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, errf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, errf("close gzip: %w", err)
	}
	if err := payload.Sync(); err != nil {
		return nil, errf("sync payload: %w", err)
	}
	ok = true
	return &manifest, nil
}

func (m *Manager) listRoots() []fs.RootInfo {
	if m.registry == nil {
		return nil
	}
	return m.registry.ListRoots()
}

func (m *Manager) writeGlobal(tw *tar.Writer, configDir string, includeDeviceIdentity bool) (ManifestGlobal, error) {
	stats := ManifestGlobal{DeviceIdentityIncluded: includeDeviceIdentity}
	if _, err := os.Stat(configDir); err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return stats, errf("stat config dir: %w", err)
	}
	err := filepath.WalkDir(configDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return errf("walk config: %w", err)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(configDir, path)
		if err != nil {
			return err
		}
		base := filepath.Base(path)
		if strings.Contains(base, bakSuffixSep) || strings.HasPrefix(base, "mindfs-import-") {
			return nil // rollover backups / import staging
		}
		if deviceIdentityFiles[base] && !includeDeviceIdentity {
			return nil
		}
		n, err := writeTarFile(tw, globalPrefix+"/"+filepath.ToSlash(rel), path)
		if err != nil {
			return err
		}
		stats.FileCount++
		stats.Bytes += n
		return nil
	})
	return stats, err
}

// writeRoot bundles one root's metadata directory. SQLite files are copied to
// a temp area first and quick-checked; failed copies degrade to jsonl-only
// with a recorded warning.
func (m *Manager) writeRoot(tw *tar.Writer, root fs.RootInfo, includeUploads bool, stageBase string) (ManifestRoot, []string, error) {
	stats := ManifestRoot{}
	var warnings []string
	metaDir := root.MetaDir()
	if metaDir == "" {
		warnings = append(warnings, fmt.Sprintf("root %s (%s): metadata directory unavailable", root.ID, root.RootPath))
		return stats, warnings, nil
	}
	dbStaging, err := os.MkdirTemp(stageBase, "mindfs-dbcheck-*")
	if err != nil {
		return stats, warnings, errf("db staging: %w", err)
	}
	defer os.RemoveAll(dbStaging)

	err = filepath.WalkDir(metaDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("root %s: metadata directory missing", root.ID))
				return nil
			}
			return errf("walk meta: %w", err)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(metaDir, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if !includeUploads && strings.HasPrefix(relSlash, "upload/") {
			return nil
		}
		if strings.Contains(filepath.Base(path), bakSuffixSep) {
			return nil
		}

		name := rootsPrefix + "/" + root.ID + "/" + relSlash
		if strings.HasSuffix(rel, ".db") {
			src, warn, err := stageCheckedDB(path, dbStaging)
			if err != nil {
				return err
			}
			if src == "" {
				warnings = append(warnings, fmt.Sprintf("root %s: %s", root.ID, warn))
				return nil
			}
			defer os.Remove(src)
			n, err := writeTarFile(tw, name, src)
			if err != nil {
				return err
			}
			stats.FileCount++
			stats.Bytes += n
			if strings.HasPrefix(relSlash, "tasks/") {
				stats.HasTasks = true
			}
			return nil
		}

		n, err := writeTarFile(tw, name, path)
		if err != nil {
			return err
		}
		stats.FileCount++
		stats.Bytes += n
		return nil
	})
	if err != nil {
		return stats, warnings, err
	}
	return stats, warnings, nil
}

// stageCheckedDB copies a sqlite database (plus WAL/SHM sidecars) into dir and
// quick-checks the copy. It returns the staged main file path, or "" with a
// warning message when the copy is unusable.
func stageCheckedDB(path, dir string) (string, string, error) {
	staged := filepath.Join(dir, filepath.Base(path))
	if err := copyFileWithSidecars(path, staged); err != nil {
		return "", "", errf("copy db %s: %w", path, err)
	}
	db, err := sql.Open("sqlite", "file:"+staged+"?mode=ro")
	if err != nil {
		return "", "", errf("open db copy: %w", err)
	}
	defer db.Close()
	var check string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&check); err != nil || !strings.EqualFold(strings.TrimSpace(check), "ok") {
		detail := "unknown"
		if err == nil {
			detail = check
		}
		return "", fmt.Sprintf("database %s failed consistency check (%s); jsonl sessions only", filepath.Base(path), detail), nil
	}
	return staged, "", nil
}

func copyFileWithSidecars(src, dst string) error {
	if err := copyFile0600(src, dst); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		side := src + suffix
		if _, err := os.Stat(side); err == nil {
			if err := copyFile0600(side, dst+suffix); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile0600(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	if fi.Size() > maxFileBytes {
		return fmt.Errorf("file exceeds %d bytes", maxFileBytes)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// --- tar helpers ---

func writeTarFile(tw *tar.Writer, name, path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, errf("stat %s: %w", path, err)
	}
	if info.Size() > maxFileBytes {
		return 0, errf("member %s exceeds %d bytes", name, maxFileBytes)
	}
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Format:  tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return 0, errf("tar header %s: %w", name, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, errf("open %s: %w", path, err)
	}
	defer file.Close()
	n, err := io.Copy(tw, file)
	if err != nil {
		return 0, errf("tar copy %s: %w", name, err)
	}
	return n, nil
}

func writeTarBytes(tw *tar.Writer, name string, payload []byte) error {
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(payload)), Format: tar.FormatPAX}
	if err := tw.WriteHeader(hdr); err != nil {
		return errf("tar header %s: %w", name, err)
	}
	if _, err := tw.Write(payload); err != nil {
		return errf("tar write %s: %w", name, err)
	}
	return nil
}
