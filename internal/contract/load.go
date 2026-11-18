package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// SourceOf hashes a PNG and records its dimensions without decoding pixels.
func SourceOf(path string) (SourceRef, *Error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SourceRef{}, Fault("SOURCE_UNREADABLE", "cannot read %s: %v", path, err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return SourceRef{}, Fault("NOT_A_PNG", "%s is not a PNG: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return SourceRef{
		File:   filepath.Base(path),
		SHA256: hex.EncodeToString(sum[:]),
		Width:  cfg.Width,
		Height: cfg.Height,
	}, nil
}

// Resolve joins a sidecar-relative reference onto the sidecar's directory.
func Resolve(baseDir, ref string) string {
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(baseDir, filepath.FromSlash(ref))
}

// Load reads and validates a sidecar.
func Load(path string) (*AssetContract, *Error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, Fault("CONTRACT_UNREADABLE", "cannot read %s: %v", path, err)
	}
	var c AssetContract
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, Fault("CONTRACT_INVALID", "%s is not a valid contract: %v", path, err)
	}
	if c.SchemaVersion != SchemaVersion {
		return nil, Fault("SCHEMA_UNSUPPORTED", "contract schema_version %d, this build reads %d", c.SchemaVersion, SchemaVersion)
	}
	if c.Engine != EngineGodot4 {
		return nil, Fault("UNKNOWN_ENGINE", "contract engine %q is not supported", c.Engine)
	}
	if c.Source.File == "" || c.Source.Width <= 0 || c.Source.Height <= 0 {
		return nil, Fault("CONTRACT_INVALID", "contract has no usable source reference")
	}
	if c.Scene == "" {
		return nil, Fault("CONTRACT_INVALID", "contract has no scene reference")
	}
	return &c, nil
}

// Save writes the sidecar with stable indentation and a trailing newline so two
// runs of the same command produce byte-identical files.
func (c *AssetContract) Save(path string) *Error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Fault("CONTRACT_UNWRITABLE", "cannot create %s: %v", dir, err)
		}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return Fault("CONTRACT_UNWRITABLE", "cannot encode contract: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return Fault("CONTRACT_UNWRITABLE", "cannot write %s: %v", path, err)
	}
	return nil
}

// RelativeRef turns a path into a sidecar-relative reference when that stays
// inside the sidecar's directory, and a bare basename otherwise. Absolute
// workspace paths never land in a shipped sidecar.
func RelativeRef(baseDir, path string) string {
	rel, err := filepath.Rel(baseDir, path)
	if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(path)
}
