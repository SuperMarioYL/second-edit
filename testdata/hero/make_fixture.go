// Command make_fixture writes the hero sprite fixture: a 96x96 atlas sheet, a
// Godot 4 scene that draws it, one good patch per named region (helmet and
// sword) and one patch that reaches outside the helmet region. Everything is
// deterministic; re-running it on the same machine reproduces the same bytes.
//
//	go run ./testdata/hero/make_fixture.go -out /tmp/se
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	size   = 96
	origin = 48.0 // sprite is centered, so texture pixel = local + 48
)

var (
	steel  = color.NRGBA{R: 0x5B, G: 0x6B, B: 0x86, A: 0xFF}
	visor  = color.NRGBA{R: 0x2F, G: 0x3B, B: 0x52, A: 0xFF}
	blade  = color.NRGBA{R: 0xC9, G: 0xCE, B: 0xD6, A: 0xFF}
	torso  = color.NRGBA{R: 0x44, G: 0x50, B: 0x6B, A: 0xFF}
	helmet = [][2]float64{{-16, -24}, {16, -24}, {24, -16}, {24, 8}, {16, 16}, {-16, 16}, {-24, 8}, {-24, -16}}
	sword  = [][2]float64{{-6, 16}, {6, 16}, {6, 40}, {-6, 40}}
	body   = [][2]float64{{-12, 16}, {12, 16}, {12, 44}, {-12, 44}}
)

func main() {
	out := flag.String("out", ".", "directory to write the fixture into")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}

	sheet := newSheet()
	writePNG(filepath.Join(*out, "hero.png"), sheet)

	helmetPatch := crop(sheet, 24, 24, 48, 40)
	repaint(helmetPatch, 24, 24, helmet, func(lx, ly float64) bool { return ly >= -18 && ly < -8 }, color.NRGBA{R: 0xC0, G: 0x39, B: 0x2B, A: 0xFF})
	writePNG(filepath.Join(*out, "helmet.png"), helmetPatch)

	// A second accepted patch on a different region, so the demo can show a
	// chained receipt (0002 with parent_receipt 0001).
	swordPatch := crop(sheet, 42, 64, 12, 24)
	repaint(swordPatch, 42, 64, sword, func(lx, ly float64) bool { return true }, color.NRGBA{R: 0xE8, G: 0xB4, B: 0x4A, A: 0xFF})
	writePNG(filepath.Join(*out, "sword.png"), swordPatch)

	sloppy := crop(sheet, 24, 24, 48, 40)
	repaint(sloppy, 24, 24, helmet, func(lx, ly float64) bool { return ly >= -18 && ly < -8 }, color.NRGBA{R: 0xC0, G: 0x39, B: 0x2B, A: 0xFF})
	// One pixel outside the octagon, inside its bounding box: the refusal case.
	sloppy.Set(47, 1, color.NRGBA{R: 0xC0, G: 0x39, B: 0x2B, A: 0xFF})
	writePNG(filepath.Join(*out, "sloppy.png"), sloppy)

	writeText(filepath.Join(*out, "hero.tscn"), scene(0, 0, 1, 1, 96, 0))
	writeText(filepath.Join(*out, "pivot_drift.tscn"), scene(2, 0, 1, 1, 96, 0))
	writeText(filepath.Join(*out, "scale_mismatch.tscn"), scene(0, 0, 2, 2, 96, 0))
	writeText(filepath.Join(*out, "collision_out_of_bounds.tscn"), scene(0, 0, 1, 1, 96, 30))
	writeText(filepath.Join(*out, "atlas_region_shift.tscn"), scene(0, 0, 1, 1, 80, 0))
	writeText(filepath.Join(*out, "no_sprite.tscn"), noSpriteScene())

	mask := regionMask(helmet)
	changed := 0
	for y := 0; y < 40; y++ {
		for x := 0; x < 48; x++ {
			if !mask[y][x] {
				continue
			}
			a := helmetPatch.NRGBAAt(x, y)
			b := sheet.NRGBAAt(x+24, y+24)
			if a != b {
				changed++
			}
		}
	}
	fmt.Printf("wrote %s\n", *out)
	fmt.Printf("hero.png %dx%d · helmet region %d px · good patch changes %d px (%.1f%% of the sheet)\n",
		size, size, maskArea(mask), changed, float64(changed)*100/float64(size*size))
	fmt.Println("sword.png repaints the sword region, for a second chained edit")
	fmt.Println("sloppy.png differs from hero.png at exactly 1 pixel outside the helmet polygon")
}

func newSheet() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	fillPoly(img, body, torso)
	fillPoly(img, sword, blade)
	fillPoly(img, helmet, steel)
	fillPoly(img, helmet, visor, func(lx, ly float64) bool { return ly >= -18 && ly < -8 })
	return img
}

func regionMask(poly [][2]float64) [40][48]bool {
	var mask [40][48]bool
	for y := 0; y < 40; y++ {
		for x := 0; x < 48; x++ {
			px := float64(x) + 24 + 0.5 - origin
			py := float64(y) + 24 + 0.5 - origin
			mask[y][x] = pointInPoly(px, py, poly)
		}
	}
	return mask
}

func maskArea(mask [40][48]bool) int {
	n := 0
	for _, row := range mask {
		for _, v := range row {
			if v {
				n++
			}
		}
	}
	return n
}

func fillPoly(img *image.NRGBA, poly [][2]float64, c color.NRGBA, keep ...func(lx, ly float64) bool) {
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			lx := float64(x) + 0.5 - origin
			ly := float64(y) + 0.5 - origin
			if !pointInPoly(lx, ly, poly) {
				continue
			}
			if len(keep) > 0 && !keep[0](lx, ly) {
				continue
			}
			img.Set(x, y, c)
		}
	}
}

// repaint recolours patch pixels whose texture-space point is inside poly and
// satisfies keep. bboxX/bboxY locate the patch in the sheet.
func repaint(img *image.NRGBA, bboxX, bboxY int, poly [][2]float64, keep func(lx, ly float64) bool, c color.NRGBA) {
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			lx := float64(x+bboxX) + 0.5 - origin
			ly := float64(y+bboxY) + 0.5 - origin
			if pointInPoly(lx, ly, poly) && keep(lx, ly) {
				img.Set(x, y, c)
			}
		}
	}
}

func crop(src *image.NRGBA, x, y, w, h int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			out.Set(i, j, src.NRGBAAt(x+i, y+j))
		}
	}
	return out
}

func pointInPoly(x, y float64, poly [][2]float64) bool {
	inside := false
	j := len(poly) - 1
	for i := 0; i < len(poly); i++ {
		xi, yi := poly[i][0], poly[i][1]
		xj, yj := poly[j][0], poly[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			inside = !inside
		}
		j = i
	}
	return inside
}

func scene(offsetX, offsetY, scaleX, scaleY float64, atlasW int, swordExtra float64) string {
	swordPts := append([][2]float64{}, sword...)
	swordPts[2][1] += swordExtra
	swordPts[3][1] += swordExtra
	return fmt.Sprintf(`[gd_scene load_steps=3 format=3]

[ext_resource type="Texture2D" path="res://hero.png" id="1_hero"]

[sub_resource type="AtlasTexture" id="AtlasTexture_hero"]
atlas = ExtResource("1_hero")
region = Rect2(0, 0, %d, %d)

[node name="Hero" type="Sprite2D"]
texture = SubResource("AtlasTexture_hero")
centered = true
offset = Vector2(%s, %s)
scale = Vector2(%s, %s)
metadata/pixels_per_unit = 1.0

[node name="helmet" type="CollisionPolygon2D" parent="."]
polygon = PackedVector2Array(%s)

[node name="sword" type="CollisionPolygon2D" parent="."]
polygon = PackedVector2Array(%s)
`, atlasW, size, g(offsetX), g(offsetY), g(scaleX), g(scaleY), pack(helmet), pack(swordPts))
}

func noSpriteScene() string {
	return fmt.Sprintf(`[gd_scene load_steps=2 format=3]

[node name="Root" type="Node2D"]

[node name="helmet" type="CollisionPolygon2D" parent="."]
polygon = PackedVector2Array(%s)
`, pack(helmet))
}

func pack(pts [][2]float64) string {
	parts := make([]string, 0, len(pts))
	for _, p := range pts {
		parts = append(parts, g(p[0])+", "+g(p[1]))
	}
	return strings.Join(parts, ", ")
}

func g(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}

func writePNG(path string, img *image.NRGBA) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

func writeText(path, s string) {
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		log.Fatal(err)
	}
}
