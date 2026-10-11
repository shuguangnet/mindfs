// Package backup implements MindFS full-instance export/import: collecting
// the global config directory and every registered root's metadata into a
// passphrase-encrypted bundle, and restoring it (preview + apply) on the same
// or a new machine.
package backup

import (
	"fmt"
	"sync"
	"time"

	"mindfs/server/internal/fs"
)

// Registry is the slice of fs.Registry the manager needs.
type Registry interface {
	ListRoots() []fs.RootInfo
	UpsertWithMetaLocation(root, metaLocation string) (fs.RootInfo, error)
}

// fsRegistry adapts *fs.Registry (whose listing method is List()).
type fsRegistry struct {
	inner interface {
		List() []fs.RootInfo
		UpsertWithMetaLocation(root, metaLocation string) (fs.RootInfo, error)
	}
}

func (a fsRegistry) ListRoots() []fs.RootInfo { return a.inner.List() }

func (a fsRegistry) UpsertWithMetaLocation(root, metaLocation string) (fs.RootInfo, error) {
	return a.inner.UpsertWithMetaLocation(root, metaLocation)
}

// AdaptRegistry wraps *fs.Registry for the backup manager.
func AdaptRegistry(reg *fs.Registry) Registry {
	if reg == nil {
		return nil
	}
	return fsRegistry{inner: reg}
}

// Invalidator lets the API layer drop cached handles after an apply.
type Invalidator interface {
	InvalidateRootContext(rootID string)
	InvalidateGlobalStores()
}

// Options configures the manager (test seams included).
type Options struct {
	// ConfigDir overrides the global config directory (~/.config/mindfs).
	ConfigDir string
	// MindfsVersion stamps the manifest.
	MindfsVersion string
	// StagingBase overrides the directory that holds import staging dirs.
	StagingBase string
	// Now overrides time.Now in tests.
	Now func() time.Time
}

// Manager builds backups and restores them.
type Manager struct {
	registry    Registry
	invalidator Invalidator
	configDir   string
	version     string
	stageBase   string
	now         func() time.Time

	mu      sync.Mutex
	imports map[string]*stagedImport
}

// NewManager returns a backup manager. configDir resolution failures surface
// on first use, not construction, so a partially configured host still serves
// clear errors.
func NewManager(reg Registry, opts Options) *Manager {
	m := &Manager{
		registry:  reg,
		configDir: opts.ConfigDir,
		version:   opts.MindfsVersion,
		stageBase: opts.StagingBase,
		now:       opts.Now,
		imports:   make(map[string]*stagedImport),
	}
	if m.now == nil {
		m.now = time.Now
	}
	return m
}

// SetInvalidator wires the API-layer cache dropper (optional).
func (m *Manager) SetInvalidator(inv Invalidator) {
	m.invalidator = inv
}

func (m *Manager) nowUTC() time.Time {
	return m.now().UTC()
}

func errf(format string, args ...any) error {
	return fmt.Errorf("backup: "+format, args...)
}
