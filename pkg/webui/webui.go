// Package webui manages the on-disk copy of the embedded WebUI.
//
// On first use it extracts the built-in assets to a user directory and records
// the version they came from. On later runs it compares the recorded version
// with the current built-in version: if the binary now ships a different UI, the
// difference is reported so the user can choose to update. Updating backs up the
// existing directory before overwriting it, so local edits are never lost
// without a copy.
package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// MarkerName is the file recording which built-in version produced the copy.
const MarkerName = ".coren-webui.json"

// Marker records the provenance of an extracted WebUI directory.
type Marker struct {
	// Hash is the content hash of the built-in assets at extraction time.
	Hash string `json:"hash"`
	// Version is the human-readable app version at extraction time.
	Version string `json:"version,omitempty"`
	// ExtractedAt is when the assets were written.
	ExtractedAt time.Time `json:"extracted_at"`
}

// Status describes the relationship between the built-in assets and the copy.
type Status struct {
	// Dir is the on-disk directory being managed.
	Dir string `json:"dir"`
	// BuiltinHash and BuiltinVersion describe the assets in the binary.
	BuiltinHash    string `json:"builtin_hash"`
	BuiltinVersion string `json:"builtin_version"`
	// DiskHash and DiskVersion describe the extracted copy (empty if none).
	DiskHash    string `json:"disk_hash"`
	DiskVersion string `json:"disk_version"`
	// Extracted reports whether a copy exists on disk.
	Extracted bool `json:"extracted"`
	// UpdateAvailable reports whether the built-in assets differ from the copy.
	UpdateAvailable bool `json:"update_available"`
}

// Manager owns one extraction directory for one embedded asset set.
type Manager struct {
	// Source is the embedded WebUI root (e.g. the "web" subtree).
	Source fs.FS
	// Dir is where the copy lives.
	Dir string
	// Version is the human-readable app version.
	Version string
	// builtinHash caches the content hash of Source.
	builtinHash string
}

// New creates a Manager. It computes the built-in content hash once.
func New(source fs.FS, dir, version string) (*Manager, error) {
	h, err := hashFS(source)
	if err != nil {
		return nil, err
	}
	return &Manager{Source: source, Dir: dir, Version: version, builtinHash: h}, nil
}

// BuiltinHash returns the content hash of the built-in assets.
func (m *Manager) BuiltinHash() string { return m.builtinHash }

// Status inspects the copy and reports whether an update is available.
func (m *Manager) Status() Status {
	st := Status{
		Dir:            m.Dir,
		BuiltinHash:    m.builtinHash,
		BuiltinVersion: m.Version,
	}
	marker, ok := m.readMarker()
	if !ok {
		return st
	}
	st.Extracted = true
	st.DiskHash = marker.Hash
	st.DiskVersion = marker.Version
	st.UpdateAvailable = marker.Hash != m.builtinHash
	return st
}

// EnsureDir extracts the assets when the directory has no marker yet. It is a
// no-op when a copy already exists, so local edits are preserved. Returns true
// when it performed an extraction.
func (m *Manager) EnsureDir() (bool, error) {
	if _, ok := m.readMarker(); ok {
		return false, nil
	}
	if err := extract(m.Source, m.Dir); err != nil {
		return false, err
	}
	if err := m.writeMarker(); err != nil {
		return false, err
	}
	return true, nil
}

// Update backs up the current directory and overwrites it with the built-in
// assets, then records the new version. It returns the backup path.
func (m *Manager) Update() (string, error) {
	backup := ""
	if _, err := os.Stat(m.Dir); err == nil {
		backup = fmt.Sprintf("%s.bak-%s", m.Dir, time.Now().Format("20060102150405"))
		if err := os.Rename(m.Dir, backup); err != nil {
			return "", fmt.Errorf("backup existing webui: %w", err)
		}
	}
	if err := extract(m.Source, m.Dir); err != nil {
		// Best-effort restore on failure.
		if backup != "" {
			_ = os.Rename(backup, m.Dir)
		}
		return "", err
	}
	if err := m.writeMarker(); err != nil {
		return "", err
	}
	return backup, nil
}

// readMarker loads the provenance marker, returning false when absent.
func (m *Manager) readMarker() (Marker, bool) {
	data, err := os.ReadFile(filepath.Join(m.Dir, MarkerName))
	if err != nil {
		return Marker{}, false
	}
	var marker Marker
	if err := json.Unmarshal(data, &marker); err != nil {
		return Marker{}, false
	}
	return marker, true
}

// writeMarker records the current built-in version beside the assets.
func (m *Manager) writeMarker() error {
	marker := Marker{Hash: m.builtinHash, Version: m.Version, ExtractedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.Dir, MarkerName), data, 0o644)
}

// FS returns a filesystem rooted at the on-disk copy when it exists, else the
// built-in source. This lets the server prefer local edits with a safe default.
func (m *Manager) FS() fs.FS {
	if _, err := os.Stat(filepath.Join(m.Dir, MarkerName)); err == nil {
		return os.DirFS(m.Dir)
	}
	return m.Source
}

// extract writes every file in src under dst, preserving relative paths.
func extract(src fs.FS, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// hashFS computes a stable content hash over all files in fsys.
func hashFS(fsys fs.FS) (string, error) {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		f, err := fsys.Open(p)
		if err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, p); err != nil {
			f.Close()
			return "", err
		}
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// IsUpdateAvailable is a convenience for callers that only need the boolean.
func (m *Manager) IsUpdateAvailable() bool {
	return m.Status().UpdateAvailable
}

// StatusProvider is implemented by shells that expose their WebUI status, so the
// launcher can report a stale copy without importing a concrete shell.
type StatusProvider interface {
	WebUIStatus() (Status, bool)
}
