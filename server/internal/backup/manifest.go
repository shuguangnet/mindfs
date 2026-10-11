package backup

import "time"

// Manifest describes a bundle's contents. It lives inside the encrypted
// payload as `manifest.json`.
type Manifest struct {
	Format        string         `json:"format"`
	Version       int            `json:"version"`
	CreatedAt     time.Time      `json:"created_at"`
	MindfsVersion string         `json:"mindfs_version,omitempty"`
	Options       ManifestOpts   `json:"options"`
	Roots         []ManifestRoot `json:"roots"`
	Global        ManifestGlobal `json:"global"`
	Warnings      []string       `json:"warnings,omitempty"`
}

// ManifestOpts records the export toggles that shaped the payload.
type ManifestOpts struct {
	IncludeUploads        bool `json:"include_uploads"`
	IncludeDeviceIdentity bool `json:"include_device_identity"`
}

// ManifestRoot describes one project root's metadata as bundled.
type ManifestRoot struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	RootPath     string `json:"root_path"`
	MetaLocation string `json:"meta_location"` // project | home
	FileCount    int    `json:"file_count"`
	Bytes        int64  `json:"bytes"`
	HasTasks     bool   `json:"has_tasks"`
}

// ManifestGlobal summarizes the bundled global config files.
type ManifestGlobal struct {
	FileCount              int   `json:"file_count"`
	Bytes                  int64 `json:"bytes"`
	DeviceIdentityIncluded bool  `json:"device_identity_included"`
}
