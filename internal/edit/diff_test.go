package edit

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperMarioYL/second-edit/internal/contract"
	"github.com/SuperMarioYL/second-edit/internal/engine/godot"
)

const fixtureDir = "../../testdata/hero"

var fixtures = []string{"hero.png", "helmet.png", "sloppy.png", "hero.tscn"}

func TestRasterizePolygon(t *testing.T) {
	c := loadContract(t, filepath.Join(fixtureDir, "hero.asset.json"))
	region, ok := c.Region("helmet")
	if !ok {
		t.Fatal("helmet region missing")
	}
	mask := Rasterize(region, c.Source.Width, c.Source.Height)
	if n := mask.Count(); n < 1700 || n > 1900 {
		t.Fatalf("octagon should cover about 1792 px, got %d", n)
	}
	if mask.Get(region.Rect.X, region.Rect.Y) {
		t.Fatal("the bounding-box corner is outside the octagon")
	}
	if !mask.Get(48, 40) {
		t.Fatal("the centre of the helmet must be inside")
	}
}

func TestUntouchedHashOnlyCoversTheComplement(t *testing.T) {
	dir := setup(t)
	src := loadPNG(t, filepath.Join(dir, "hero.png"))
	c := loadContract(t, filepath.Join(dir, "hero.asset.json"))
	region, _ := c.Region("helmet")
	mask := Rasterize(region, c.Source.Width, c.Source.Height)
	base := UntouchedHash(src, mask)

	inside := clone(src)
	inside.Set(48, 40, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	if got := UntouchedHash(inside, mask); got != base {
		t.Fatal("a change inside the mask must not move the complement hash")
	}

	outside := clone(src)
	outside.Set(4, 4, color.NRGBA{R: 9, G: 9, B: 9, A: 255})
	if got := UntouchedHash(outside, mask); got == base {
		t.Fatal("a change outside the mask must move the complement hash")
	}
}

func TestApplyWritesOnlyOnACleanComplement(t *testing.T) {
	dir := setup(t)
	res, cerr := Apply(Options{
		ContractPath: filepath.Join(dir, "hero.asset.json"),
		Region:       "helmet",
		PatchPath:    filepath.Join(dir, "helmet.png"),
		OutPath:      filepath.Join(dir, "hero.v2.png"),
	})
	if cerr != nil {
		t.Fatalf("apply: %v", cerr)
	}
	if res.Receipt.Verdict != "PASS" || res.Receipt.ChangedPx == 0 {
		t.Fatalf("want a passing edit with changed pixels, got %+v", res.Receipt)
	}
	if res.Receipt.ID != "0001" || res.Receipt.ParentReceipt != "" {
		t.Fatalf("first receipt must be 0001 with no parent, got %q/%q", res.Receipt.ID, res.Receipt.ParentReceipt)
	}
	c := loadContract(t, filepath.Join(dir, "hero.asset.json"))
	region, _ := c.Region("helmet")
	want := UntouchedHash(loadPNG(t, filepath.Join(dir, "hero.png")), Rasterize(region, c.Source.Width, c.Source.Height))
	if res.Receipt.UntouchedSHA != want {
		t.Fatal("receipt does not carry the complement hash")
	}
	if _, err := os.Stat(filepath.Join(dir, "hero.v2.png")); err != nil {
		t.Fatal("output PNG missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "receipts", "0001.json")); err != nil {
		t.Fatal("receipt missing")
	}
}

func TestApplyRefusesOutOfRegionAndWritesNothing(t *testing.T) {
	dir := setup(t)
	res, cerr := Apply(Options{
		ContractPath: filepath.Join(dir, "hero.asset.json"),
		Region:       "helmet",
		PatchPath:    filepath.Join(dir, "sloppy.png"),
		OutPath:      filepath.Join(dir, "hero.v3.png"),
	})
	if cerr == nil || cerr.Code != "OUT_OF_REGION" || cerr.Exit != contract.ExitReject {
		t.Fatalf("want OUT_OF_REGION exit 1, got %v", cerr)
	}
	if res != nil {
		t.Fatal("a refusal must not return a result")
	}
	if _, err := os.Stat(filepath.Join(dir, "hero.v3.png")); !os.IsNotExist(err) {
		t.Fatal("a refused edit must not write an output PNG")
	}
	if _, err := os.Stat(filepath.Join(dir, "hero.v3.asset.json")); !os.IsNotExist(err) {
		t.Fatal("a refused edit must not write a sidecar")
	}
	if _, err := os.Stat(filepath.Join(dir, "receipts")); !os.IsNotExist(err) {
		t.Fatal("a refused edit must not write a receipt")
	}
}

func TestApplyChainsReceipts(t *testing.T) {
	dir := setup(t)
	first, cerr := Apply(Options{
		ContractPath: filepath.Join(dir, "hero.asset.json"),
		Region:       "helmet",
		PatchPath:    filepath.Join(dir, "helmet.png"),
		OutPath:      filepath.Join(dir, "hero.v2.png"),
	})
	if cerr != nil {
		t.Fatal(cerr)
	}
	second := tintPatch(t, dir, "helmet.b.png", "hero.png", func(ax, ay int) bool { return ay >= 40 && ay < 48 })
	res, cerr := Apply(Options{
		ContractPath: filepath.Join(dir, "hero.v2.asset.json"),
		Region:       "helmet",
		PatchPath:    second,
		OutPath:      filepath.Join(dir, "hero.v3.png"),
	})
	if cerr != nil {
		t.Fatalf("second apply: %v", cerr)
	}
	if res.Receipt.ID != "0002" || res.Receipt.ParentReceipt != first.Receipt.ID {
		t.Fatalf("want 0002 parented to %q, got %q/%q", first.Receipt.ID, res.Receipt.ID, res.Receipt.ParentReceipt)
	}
	if res.Receipt.BeforeSHA != first.Receipt.AfterSHA {
		t.Fatal("edit N must start where edit N-1 ended")
	}
}

func TestBrokenChainIsRefused(t *testing.T) {
	dir := setup(t)
	if _, cerr := Apply(Options{
		ContractPath: filepath.Join(dir, "hero.asset.json"),
		Region:       "helmet",
		PatchPath:    filepath.Join(dir, "helmet.png"),
		OutPath:      filepath.Join(dir, "hero.v2.png"),
	}); cerr != nil {
		t.Fatal(cerr)
	}
	path := filepath.Join(dir, "receipts", "0001.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := tamperAfterSHA(string(data))
	if corrupt == string(data) {
		t.Fatal("test could not corrupt the receipt")
	}
	if err := os.WriteFile(path, []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := tintPatch(t, dir, "helmet.b.png", "hero.v2.png", func(ax, ay int) bool { return ay >= 40 && ay < 48 })
	_, cerr := Apply(Options{
		ContractPath: filepath.Join(dir, "hero.v2.asset.json"),
		Region:       "helmet",
		PatchPath:    patch,
		OutPath:      filepath.Join(dir, "hero.v3.png"),
	})
	if cerr == nil || cerr.Code != "BROKEN_CHAIN" || cerr.Exit != contract.ExitReject {
		t.Fatalf("want BROKEN_CHAIN exit 1, got %v", cerr)
	}
}

func TestTwoPixelMoveInsideRegionStillPasses(t *testing.T) {
	dir := setup(t)
	patch := shiftPatch(t, dir, "helmet.shift.png", "hero.png", 2)
	res, cerr := Apply(Options{
		ContractPath: filepath.Join(dir, "hero.asset.json"),
		Region:       "helmet",
		PatchPath:    patch,
		OutPath:      filepath.Join(dir, "hero.moved.png"),
	})
	if cerr != nil {
		t.Fatalf("a move inside the region must pass: %v", cerr)
	}
	if res.Receipt.ChangedPx == 0 {
		t.Fatal("the move changed nothing")
	}
}

func TestApplyRefusesUnchangedPatch(t *testing.T) {
	dir := setup(t)
	_, cerr := Apply(Options{
		ContractPath: filepath.Join(dir, "hero.asset.json"),
		Region:       "helmet",
		PatchPath:    cropCopy(t, dir, "helmet.copy.png", "hero.png"),
		OutPath:      filepath.Join(dir, "hero.same.png"),
	})
	if cerr == nil || cerr.Code != "NO_PIXEL_CHANGE" {
		t.Fatalf("want NO_PIXEL_CHANGE, got %v", cerr)
	}
}

func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range fixtures {
		copyFile(t, filepath.Join(fixtureDir, name), filepath.Join(dir, name))
	}
	c, cerr := godot.Init(filepath.Join(dir, "hero.png"), filepath.Join(dir, "hero.tscn"))
	if cerr != nil {
		t.Fatal(cerr)
	}
	if cerr := c.Save(filepath.Join(dir, "hero.asset.json")); cerr != nil {
		t.Fatal(cerr)
	}
	return dir
}

func loadContract(t *testing.T, path string) *contract.AssetContract {
	t.Helper()
	c, cerr := contract.Load(path)
	if cerr != nil {
		t.Fatal(cerr)
	}
	return c
}

func loadPNG(t *testing.T, path string) *image.NRGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return toNRGBA(img)
}

func cropCopy(t *testing.T, dir, name, srcName string) string {
	t.Helper()
	return writePatchImage(t, dir, name, func(patch, src *image.NRGBA, region contract.Rect, mask *Mask) {})
}

// tintPatch crops the source at the helmet bounds and recolours masked pixels
// accepted by keep.
func tintPatch(t *testing.T, dir, name, srcName string, keep func(ax, ay int) bool) string {
	t.Helper()
	return writePatchImageFrom(t, dir, name, srcName, func(patch, src *image.NRGBA, region contract.Rect, mask *Mask) {
		for y := 0; y < region.H; y++ {
			for x := 0; x < region.W; x++ {
				ax, ay := region.X+x, region.Y+y
				if mask.Get(ax, ay) && keep(ax, ay) {
					patch.Set(x, y, color.NRGBA{R: 0x27, G: 0xAE, B: 0x60, A: 0xFF})
				}
			}
		}
	})
}

// shiftPatch moves masked content dx pixels sideways, which stays inside the
// region and therefore must pass.
func shiftPatch(t *testing.T, dir, name, srcName string, dx int) string {
	t.Helper()
	return writePatchImageFrom(t, dir, name, srcName, func(patch, src *image.NRGBA, region contract.Rect, mask *Mask) {
		for y := 0; y < region.H; y++ {
			for x := 0; x < region.W; x++ {
				ax, ay := region.X+x, region.Y+y
				if mask.Get(ax, ay) {
					patch.Set(x, y, src.NRGBAAt(ax-dx, ay))
				}
			}
		}
	})
}

func writePatchImage(t *testing.T, dir, name string, mutate func(patch, src *image.NRGBA, region contract.Rect, mask *Mask)) string {
	return writePatchImageFrom(t, dir, name, "hero.png", mutate)
}

func writePatchImageFrom(t *testing.T, dir, name, srcName string, mutate func(patch, src *image.NRGBA, region contract.Rect, mask *Mask)) string {
	t.Helper()
	c := loadContract(t, filepath.Join(dir, "hero.asset.json"))
	reg, ok := c.Region("helmet")
	if !ok {
		t.Fatal("helmet region missing")
	}
	b := reg.Rect
	src := loadPNG(t, filepath.Join(dir, srcName))
	patch := image.NewNRGBA(image.Rect(0, 0, b.W, b.H))
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			patch.Set(x, y, src.NRGBAAt(b.X+x, b.Y+y))
		}
	}
	mask := Rasterize(reg, c.Source.Width, c.Source.Height)
	mutate(patch, src, b, mask)
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, patch); err != nil {
		t.Fatal(err)
	}
	return path
}

func clone(img *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(img.Bounds())
	copy(out.Pix, img.Pix)
	return out
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func tamperAfterSHA(s string) string {
	const marker = `"after_sha": "`
	i := strings.Index(s, marker)
	if i < 0 {
		return s
	}
	i += len(marker)
	repl := "0"
	if s[i] == '0' {
		repl = "1"
	}
	return s[:i] + repl + s[i+1:]
}
