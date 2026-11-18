// Package contract owns the asset-contract sidecar: the on-disk record of what
// a game engine reads about a sprite, and the facts an edit has to preserve.
//
// The sidecar is the unit users hand around. Pixels are only interesting
// through it: a region names the permitted change, Facts name the import
// metadata that must survive, and Invariants cap how much may move.
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
)

// SchemaVersion is the only sidecar schema this build reads or writes.
const SchemaVersion = 1

// Process exit codes. They are part of the agent surface and must not drift.
const (
	ExitPass   = 0 // a contract was produced, an edit was applied
	ExitReject = 1 // the edit or the asset was refused; nothing was written
	ExitFault  = 2 // usage or contract error; nothing was written
)

// EngineGodot4 is the single engine this build maps.
const EngineGodot4 = "godot-4"

// Error carries an exit code and a stable machine code. Rejections and faults
// both travel through it so the CLI never invents a second taxonomy.
type Error struct {
	Code string `json:"code"`
	Msg  string `json:"message"`
	Exit int    `json:"-"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }

// Fault reports a usage or contract problem. Exit 2.
func Fault(code, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...), Exit: ExitFault}
}

// Violation reports a refused edit or asset. Exit 1.
func Violation(code, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...), Exit: ExitReject}
}

// SourceRef pins the raster the contract was built from.
type SourceRef struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Rect is a pixel-space rectangle.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Region is a named area in pixel space. Rect is always the bounding box; for
// shape "polygon" the polygon is the mask, for shape "rect" the rectangle is.
type Region struct {
	Name    string       `json:"name"`
	Shape   string       `json:"shape"`
	Rect    Rect         `json:"rect"`
	Polygon [][2]float64 `json:"polygon,omitempty"`
}

// Collision is one CollisionPolygon2D, in pixel space.
type Collision struct {
	Name   string       `json:"name"`
	Points [][2]float64 `json:"points"`
}

// AtlasRef records the atlas sheet and the sub-rectangle a sprite draws.
type AtlasRef struct {
	Sheet  string `json:"sheet"`
	Region Rect   `json:"region"`
}

// Facts are the import facts the engine reads. An edit that changes any of
// them is not an edit, it is a different asset.
type Facts struct {
	Pivot         [2]float64  `json:"pivot"`
	PixelsPerUnit float64     `json:"pixels_per_unit"`
	Scale         [2]float64  `json:"scale"`
	Collision     []Collision `json:"collision"`
	Atlas         *AtlasRef   `json:"atlas,omitempty"`
}

// Invariant is a declared ceiling on an edit. Only "max_changed_pct" is
// enforced in v0.1.
type Invariant struct {
	Kind  string  `json:"kind"`
	Value float64 `json:"value"`
}

// AssetContract is the sidecar document.
type AssetContract struct {
	SchemaVersion int         `json:"schema_version"`
	ID            string      `json:"id"`
	Engine        string      `json:"engine"`
	Scene         string      `json:"scene"`
	Source        SourceRef   `json:"source"`
	Regions       []Region    `json:"regions"`
	Facts         Facts       `json:"facts"`
	Invariants    []Invariant `json:"invariants"`
}

// Check is one named import check. Shared by the engine validator, the receipt
// and the check command so the vocabulary exists once.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | violated
	Code   string `json:"code,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// AssetID derives the stable id for an asset lineage. It depends on the engine
// and the source file name only, so repeated `init` runs hash identically and
// later edits keep the id.
func AssetID(engine, file string) string {
	sum := sha256.Sum256([]byte(engine + "\x00" + filepath.Base(file)))
	return hex.EncodeToString(sum[:6])
}

// Region looks a region up by name.
func (c *AssetContract) Region(name string) (Region, bool) {
	for _, r := range c.Regions {
		if r.Name == name {
			return r, true
		}
	}
	return Region{}, false
}

// MaxChangedPct returns the declared ceiling on changed pixels, if any.
func (c *AssetContract) MaxChangedPct() (float64, bool) {
	for _, inv := range c.Invariants {
		if inv.Kind == "max_changed_pct" {
			return inv.Value, true
		}
	}
	return 0, false
}
