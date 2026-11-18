// Package godot maps a Godot 4 scene onto asset facts and checks those facts
// against a contract. It reads the node properties this build maps and records
// declared values; it does not re-implement Godot's importer. In particular
// pixels_per_unit comes from the project's own metadata/pixels_per_unit key,
// which the importer does not read.
//
// v0.1 simplification: CollisionPolygon2D points are treated as authored in
// texture-pixel space relative to the sprite origin. That is exact for the
// fixtures this repo ships and for the common "collision under the sprite"
// setup; it is not a general transform.
package godot

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SuperMarioYL/second-edit/internal/contract"
)

// Scene is the subset of a .tscn this build reads.
type Scene struct {
	Sprite        string
	TexturePath   string // res:// path of the raster the sprite draws
	HasAtlas      bool
	AtlasRegion   contract.Rect
	Centered      bool
	Offset        [2]float64
	Scale         [2]float64
	PixelsPerUnit float64
	Collisions    []Collision
}

// Collision is one named CollisionPolygon2D in sprite-local pixel space.
type Collision struct {
	Name   string
	Points [][2]float64
}

type section struct {
	kind  string
	attrs map[string]string
	props map[string]string
}

// Parse reads a Godot 4 scene. A file without a [gd_scene] header is rejected
// as an unknown engine rather than half-parsed.
func Parse(path string) (*Scene, *contract.Error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, contract.Fault("SCENE_UNREADABLE", "cannot read %s: %v", path, err)
	}
	text := string(data)
	if !strings.Contains(text, "[gd_scene") {
		return nil, contract.Fault("UNKNOWN_ENGINE", "%s is not a Godot 4 scene: no [gd_scene] header", path)
	}
	secs := parseSections(text)

	extPath := map[string]string{}
	subs := map[string]*section{}
	for _, s := range secs {
		switch s.kind {
		case "ext_resource":
			if id, ok := s.attrs["id"]; ok {
				extPath[id] = s.attrs["path"]
			}
		case "sub_resource":
			if id, ok := s.attrs["id"]; ok {
				subs[id] = s
			}
		}
	}

	sc := &Scene{Scale: [2]float64{1, 1}, PixelsPerUnit: 1, Centered: true}
	var sprite *section
	for _, s := range secs {
		if s.kind != "node" {
			continue
		}
		switch s.attrs["type"] {
		case "Sprite2D":
			if sprite == nil {
				sprite = s
			}
		case "CollisionPolygon2D":
			pts, ok := parseVec2Array(s.props["polygon"])
			if !ok || len(pts) < 3 {
				return nil, contract.Fault("SCENE_INVALID", "CollisionPolygon2D %q has no readable polygon", s.attrs["name"])
			}
			sc.Collisions = append(sc.Collisions, Collision{Name: s.attrs["name"], Points: pts})
		}
	}
	if sprite == nil {
		return nil, contract.Fault("NO_ENGINE_FACTS", "%s has no Sprite2D node; nothing to map", path)
	}
	sc.Sprite = sprite.attrs["name"]

	ref := sprite.props["texture"]
	switch {
	case strings.HasPrefix(ref, "ExtResource("):
		id, _ := quoted(ref)
		p, ok := extPath[id]
		if !ok {
			return nil, contract.Fault("NO_ENGINE_FACTS", "sprite texture refers to missing ext_resource %q", id)
		}
		sc.TexturePath = p
	case strings.HasPrefix(ref, "SubResource("):
		id, _ := quoted(ref)
		sub, ok := subs[id]
		if !ok || sub.attrs["type"] != "AtlasTexture" {
			return nil, contract.Fault("NO_ENGINE_FACTS", "sprite texture sub_resource %q is not an AtlasTexture", id)
		}
		atlasID, _ := quoted(sub.props["atlas"])
		p, ok := extPath[atlasID]
		if !ok {
			return nil, contract.Fault("NO_ENGINE_FACTS", "AtlasTexture %q has no atlas sheet", id)
		}
		region, ok := parseRect2(sub.props["region"])
		if !ok {
			return nil, contract.Fault("SCENE_INVALID", "AtlasTexture %q has no readable region", id)
		}
		sc.TexturePath = p
		sc.HasAtlas = true
		sc.AtlasRegion = region
	default:
		return nil, contract.Fault("NO_ENGINE_FACTS", "Sprite2D %q has no texture", sc.Sprite)
	}

	if v, ok := parseVec2(sprite.props["offset"]); ok {
		sc.Offset = v
	}
	if v, ok := parseVec2(sprite.props["scale"]); ok {
		sc.Scale = v
	}
	if b, ok := parseBool(sprite.props["centered"]); ok {
		sc.Centered = b
	}
	if raw, ok := sprite.props["metadata/pixels_per_unit"]; ok {
		if f, err := strconv.ParseFloat(raw, 64); err == nil && f > 0 {
			sc.PixelsPerUnit = f
		}
	}
	return sc, nil
}

// Origin is the texture-space position of the sprite's top-left corner.
func Origin(sc *Scene, width, height int) [2]float64 {
	ox, oy := sc.Offset[0], sc.Offset[1]
	if sc.Centered {
		ox -= float64(width) / 2
		oy -= float64(height) / 2
	}
	return [2]float64{ox, oy}
}

// FactsOf turns what the engine reads into contract facts, in pixel space.
func FactsOf(sc *Scene, width, height int) contract.Facts {
	origin := Origin(sc, width, height)
	facts := contract.Facts{
		Pivot:         [2]float64{-origin[0], -origin[1]},
		PixelsPerUnit: sc.PixelsPerUnit,
		Scale:         sc.Scale,
		Collision:     make([]contract.Collision, 0, len(sc.Collisions)),
	}
	for _, c := range sc.Collisions {
		pts := make([][2]float64, len(c.Points))
		for i, p := range c.Points {
			pts[i] = [2]float64{p[0] - origin[0], p[1] - origin[1]}
		}
		facts.Collision = append(facts.Collision, contract.Collision{Name: c.Name, Points: pts})
	}
	if sc.HasAtlas {
		facts.Atlas = &contract.AtlasRef{Sheet: sc.TexturePath, Region: sc.AtlasRegion}
	}
	return facts
}

// RegionsOf names the edit surface: every CollisionPolygon2D in the scene.
func RegionsOf(sc *Scene, width, height int) []contract.Region {
	origin := Origin(sc, width, height)
	out := make([]contract.Region, 0, len(sc.Collisions))
	for _, c := range sc.Collisions {
		pts := make([][2]float64, len(c.Points))
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for i, p := range c.Points {
			pts[i] = [2]float64{p[0] - origin[0], p[1] - origin[1]}
			minX = math.Min(minX, pts[i][0])
			minY = math.Min(minY, pts[i][1])
			maxX = math.Max(maxX, pts[i][0])
			maxY = math.Max(maxY, pts[i][1])
		}
		rect := contract.Rect{
			X: int(math.Round(minX)), Y: int(math.Round(minY)),
			W: int(math.Round(maxX - minX)), H: int(math.Round(maxY - minY)),
		}
		shape := "polygon"
		if isAxisAlignedRect(pts) {
			shape = "rect"
		}
		out = append(out, contract.Region{Name: c.Name, Shape: shape, Rect: rect, Polygon: pts})
	}
	return out
}

// Validate compares the contract's facts with the scene as it is now. It
// returns one check per fact, always in pivot, scale, collision, atlas order.
func Validate(facts contract.Facts, sc *Scene, width, height int) []contract.Check {
	origin := Origin(sc, width, height)
	pivot := [2]float64{-origin[0], -origin[1]}
	checks := []contract.Check{
		{Name: "pivot", Status: "ok"},
		{Name: "scale", Status: "ok"},
		{Name: "collision", Status: "ok"},
		{Name: "atlas", Status: "ok"},
	}

	if !closeVec(pivot, facts.Pivot) {
		checks[0] = contract.Check{
			Name: "pivot", Status: "violated", Code: "PIVOT_DRIFT",
			Detail: fmt.Sprintf("pivot (%.0f,%.0f) in scene, contract says (%.0f,%.0f)", pivot[0], pivot[1], facts.Pivot[0], facts.Pivot[1]),
		}
	}

	if !closeVec(sc.Scale, facts.Scale) || math.Abs(sc.PixelsPerUnit-facts.PixelsPerUnit) > 1e-6 {
		checks[1] = contract.Check{
			Name: "scale", Status: "violated", Code: "SCALE_MISMATCH",
			Detail: fmt.Sprintf("scale (%.3g,%.3g) ppu %.3g in scene, contract says (%.3g,%.3g) ppu %.3g",
				sc.Scale[0], sc.Scale[1], sc.PixelsPerUnit, facts.Scale[0], facts.Scale[1], facts.PixelsPerUnit),
		}
	}

	for _, c := range sc.Collisions {
		for _, p := range c.Points {
			px := p[0] - origin[0]
			py := p[1] - origin[1]
			if px < 0 || py < 0 || px > float64(width) || py > float64(height) {
				checks[2] = contract.Check{
					Name: "collision", Status: "violated", Code: "COLLISION_OUT_OF_BOUNDS",
					Detail: fmt.Sprintf("collision %q reaches (%.0f,%.0f) outside %dx%d", c.Name, px, py, width, height),
				}
				break
			}
		}
		if checks[2].Status == "violated" {
			break
		}
	}

	switch {
	case facts.Atlas == nil && !sc.HasAtlas:
		checks[3].Detail = "no atlas"
	case facts.Atlas == nil || !sc.HasAtlas:
		checks[3] = contract.Check{
			Name: "atlas", Status: "violated", Code: "ATLAS_REGION_SHIFT",
			Detail: "scene atlas presence does not match the contract",
		}
	case facts.Atlas.Region != sc.AtlasRegion || facts.Atlas.Sheet != sc.TexturePath:
		checks[3] = contract.Check{
			Name: "atlas", Status: "violated", Code: "ATLAS_REGION_SHIFT",
			Detail: fmt.Sprintf("atlas region %v in scene, contract says %v", sc.AtlasRegion, facts.Atlas.Region),
		}
	}
	return checks
}

// Init builds a contract from a PNG and the scene that owns it. It refuses a
// scene it cannot map, and it does not write anything.
func Init(pngPath, scenePath string) (*contract.AssetContract, *contract.Error) {
	src, cerr := contract.SourceOf(pngPath)
	if cerr != nil {
		return nil, cerr
	}
	sc, cerr := Parse(scenePath)
	if cerr != nil {
		return nil, cerr
	}
	if sc.TexturePath == "" || filepath.Base(sc.TexturePath) != src.File {
		return nil, contract.Fault("NO_ENGINE_FACTS", "%s does not draw %s; no engine facts to map", scenePath, src.File)
	}
	regions := RegionsOf(sc, src.Width, src.Height)
	if len(regions) == 0 {
		return nil, contract.Fault("NO_ENGINE_FACTS", "%s has no CollisionPolygon2D, so there is no named region to edit", scenePath)
	}
	return &contract.AssetContract{
		SchemaVersion: contract.SchemaVersion,
		ID:            contract.AssetID(contract.EngineGodot4, src.File),
		Engine:        contract.EngineGodot4,
		Scene:         filepath.Base(scenePath),
		Source:        src,
		Regions:       regions,
		Facts:         FactsOf(sc, src.Width, src.Height),
		Invariants:    []contract.Invariant{{Kind: "max_changed_pct", Value: 10}},
	}, nil
}

func closeVec(a, b [2]float64) bool {
	return math.Abs(a[0]-b[0]) < 1e-6 && math.Abs(a[1]-b[1]) < 1e-6
}

func isAxisAlignedRect(pts [][2]float64) bool {
	if len(pts) != 4 {
		return false
	}
	xs := map[float64]bool{}
	ys := map[float64]bool{}
	for _, p := range pts {
		xs[p[0]] = true
		ys[p[1]] = true
	}
	return len(xs) == 2 && len(ys) == 2
}

func parseSections(text string) []*section {
	var out []*section
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			fields := splitFields(line[1 : len(line)-1])
			if len(fields) == 0 {
				continue
			}
			s := &section{kind: fields[0], attrs: map[string]string{}, props: map[string]string{}}
			for _, f := range fields[1:] {
				if k, v, ok := strings.Cut(f, "="); ok {
					s.attrs[strings.TrimSpace(k)] = strings.Trim(unquote(strings.TrimSpace(v)), "\"")
				}
			}
			out = append(out, s)
			continue
		}
		if len(out) == 0 {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[len(out)-1].props[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

func splitFields(s string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			b.WriteRune(r)
		case r == ' ' && !quoted:
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// quoted pulls the first double-quoted token out of a reference like
// ExtResource("1_hero").
func quoted(s string) (string, bool) {
	start := strings.Index(s, "\"")
	if start < 0 {
		return "", false
	}
	rest := s[start+1:]
	end := strings.Index(rest, "\"")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

func unquote(s string) string {
	return strings.Trim(s, "\"")
}

func parseVec2(s string) ([2]float64, bool) {
	inner, ok := callArgs(s, "Vector2")
	if !ok {
		return [2]float64{}, false
	}
	nums := splitNumbers(inner)
	if len(nums) != 2 {
		return [2]float64{}, false
	}
	return [2]float64{nums[0], nums[1]}, true
}

func parseRect2(s string) (contract.Rect, bool) {
	inner, ok := callArgs(s, "Rect2")
	if !ok {
		return contract.Rect{}, false
	}
	nums := splitNumbers(inner)
	if len(nums) != 4 {
		return contract.Rect{}, false
	}
	return contract.Rect{
		X: int(math.Round(nums[0])), Y: int(math.Round(nums[1])),
		W: int(math.Round(nums[2])), H: int(math.Round(nums[3])),
	}, true
}

func parseVec2Array(s string) ([][2]float64, bool) {
	inner, ok := callArgs(s, "PackedVector2Array")
	if !ok {
		return nil, false
	}
	nums := splitNumbers(inner)
	if len(nums) < 6 || len(nums)%2 != 0 {
		return nil, false
	}
	out := make([][2]float64, 0, len(nums)/2)
	for i := 0; i < len(nums); i += 2 {
		out = append(out, [2]float64{nums[i], nums[i+1]})
	}
	return out, true
}

func callArgs(s, name string) (string, bool) {
	prefix := name + "("
	if !strings.HasPrefix(s, prefix) || !strings.HasSuffix(s, ")") {
		return "", false
	}
	return s[len(prefix) : len(s)-1], true
}

func splitNumbers(s string) []float64 {
	var out []float64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		f, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return nil
		}
		out = append(out, f)
	}
	return out
}

func parseBool(s string) (bool, bool) {
	switch s {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}
