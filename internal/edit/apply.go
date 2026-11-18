package edit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuperMarioYL/second-edit/internal/contract"
	"github.com/SuperMarioYL/second-edit/internal/engine/godot"
	"github.com/SuperMarioYL/second-edit/internal/receipt"
)

// Options is one apply transaction.
type Options struct {
	ContractPath string
	Region       string
	PatchPath    string
	OutPath      string
	Feather      int
}

// Result is what a passing apply leaves behind.
type Result struct {
	ReceiptPath string
	AssetPath   string
	OutPath     string
	Receipt     receipt.Receipt
}

// Apply composites a patch into a named region and writes only when every check
// passes. Every refusal returns before any file is created.
func Apply(opts Options) (*Result, *contract.Error) {
	if opts.Feather < 0 || opts.Feather > 16 {
		return nil, contract.Fault("FEATHER_INVALID", "feather must be between 0 and 16")
	}
	c, cerr := contract.Load(opts.ContractPath)
	if cerr != nil {
		return nil, cerr
	}
	baseDir := filepath.Dir(opts.ContractPath)
	srcPath := contract.Resolve(baseDir, c.Source.File)

	src, beforeSHA, cerr := readPNG(srcPath)
	if cerr != nil {
		return nil, cerr
	}
	if beforeSHA != c.Source.SHA256 {
		return nil, contract.Fault("SOURCE_SHA_MISMATCH",
			"%s is %s but the contract pins %s; re-run init", c.Source.File, beforeSHA[:12], c.Source.SHA256[:12])
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w != c.Source.Width || h != c.Source.Height {
		return nil, contract.Fault("SOURCE_SIZE_MISMATCH", "%s is %dx%d, contract says %dx%d", c.Source.File, w, h, c.Source.Width, c.Source.Height)
	}

	region, ok := c.Region(opts.Region)
	if !ok {
		return nil, contract.Fault("REGION_UNKNOWN", "%s has no region %q", opts.ContractPath, opts.Region)
	}
	if region.Rect.W <= 0 || region.Rect.H <= 0 ||
		region.Rect.X < 0 || region.Rect.Y < 0 ||
		region.Rect.X+region.Rect.W > w || region.Rect.Y+region.Rect.H > h {
		return nil, contract.Fault("REGION_OUT_OF_BOUNDS", "region %q is %v, outside %dx%d", region.Name, region.Rect, w, h)
	}
	mask := Rasterize(region, w, h)
	if mask.Count() == 0 {
		return nil, contract.Fault("REGION_EMPTY", "region %q covers no pixels", region.Name)
	}

	patch, _, cerr := readPNG(opts.PatchPath)
	if cerr != nil {
		return nil, cerr
	}
	if patch.Bounds().Dx() != region.Rect.W || patch.Bounds().Dy() != region.Rect.H {
		return nil, contract.Fault("PATCH_SIZE_MISMATCH", "patch is %dx%d, region %q needs %dx%d",
			patch.Bounds().Dx(), patch.Bounds().Dy(), region.Name, region.Rect.W, region.Rect.H)
	}

	if n, first, found := FirstOutside(src, patch, region.Rect.X, region.Rect.Y, mask); found {
		return nil, contract.Violation("OUT_OF_REGION", "%d px at (%d, %d) outside %q", n, first.X, first.Y, region.Name)
	}

	out := Composite(src, patch, region.Rect.X, region.Rect.Y, mask)
	changed := ChangedInMask(src, out, mask)
	if changed == 0 {
		return nil, contract.Violation("NO_PIXEL_CHANGE", "patch reproduces %q exactly; nothing to record", region.Name)
	}
	pct := round1(float64(changed) * 100 / float64(w*h))
	if max, has := c.MaxChangedPct(); has && pct > max {
		return nil, contract.Violation("MAX_CHANGED_PCT", "%.1f%% of pixels would change, invariant allows %.1f%%", pct, max)
	}

	scenePath := contract.Resolve(baseDir, c.Scene)
	scene, cerr := godot.Parse(scenePath)
	if cerr != nil {
		return nil, cerr
	}
	checks := godot.Validate(c.Facts, scene, w, h)
	for _, chk := range checks {
		if chk.Status != "ok" {
			return nil, contract.Violation(chk.Code, "%s", chk.Detail)
		}
	}

	receiptsDir := filepath.Join(filepath.Dir(opts.OutPath), "receipts")
	if cerr := receipt.VerifyChain(receiptsDir, beforeSHA); cerr != nil {
		return nil, cerr
	}
	id, parent, cerr := receipt.Next(receiptsDir)
	if cerr != nil {
		return nil, cerr
	}

	if samePath(opts.OutPath, srcPath) {
		return nil, contract.Fault("OUT_IS_SOURCE", "--out must not overwrite the contract's source")
	}

	pngData, afterSHA, cerr := encodePNG(out)
	if cerr != nil {
		return nil, cerr
	}
	if err := os.MkdirAll(filepath.Dir(opts.OutPath), 0o755); err != nil {
		return nil, contract.Fault("OUT_UNWRITABLE", "cannot create output directory: %v", err)
	}
	if err := os.WriteFile(opts.OutPath, pngData, 0o644); err != nil {
		return nil, contract.Fault("OUT_UNWRITABLE", "cannot write %s: %v", opts.OutPath, err)
	}

	next := *c
	next.Source = contract.SourceRef{
		File:   filepath.Base(opts.OutPath),
		SHA256: afterSHA,
		Width:  w,
		Height: h,
	}
	assetPath := strings.TrimSuffix(opts.OutPath, filepath.Ext(opts.OutPath)) + ".asset.json"
	if cerr := next.Save(assetPath); cerr != nil {
		return nil, cerr
	}

	r := receipt.Receipt{
		SchemaVersion: contract.SchemaVersion,
		ID:            id,
		ParentReceipt: parent,
		AssetID:       c.ID,
		Region:        region.Name,
		BeforeSHA:     beforeSHA,
		AfterSHA:      afterSHA,
		MaskAreaPx:    mask.Count(),
		ChangedPx:     changed,
		ChangedPct:    pct,
		UntouchedSHA:  UntouchedHash(src, mask.Dilate(opts.Feather)),
		Checks:        checks,
		Verdict:       "PASS",
		Output:        opts.OutPath,
	}
	receiptPath, cerr := receipt.Save(receiptsDir, r)
	if cerr != nil {
		return nil, cerr
	}

	return &Result{
		ReceiptPath: receiptPath,
		AssetPath:   assetPath,
		OutPath:     opts.OutPath,
		Receipt:     r,
	}, nil
}

// Inspect re-runs the non-destructive half of apply: source identity, engine
// facts and receipt chain, with no patch and no write.
type Inspection struct {
	Checks  []contract.Check
	Chain   []contract.Check
	Receipt int
}

// Inspect validates an existing asset contract against its scene and receipts.
func Inspect(contractPath, sceneOverride string) (*Inspection, *contract.Error) {
	c, cerr := contract.Load(contractPath)
	if cerr != nil {
		return nil, cerr
	}
	baseDir := filepath.Dir(contractPath)
	src, sha, cerr := readPNG(contract.Resolve(baseDir, c.Source.File))
	if cerr != nil {
		return nil, cerr
	}
	checks := []contract.Check{{Name: "source", Status: "ok"}}
	if sha != c.Source.SHA256 {
		checks[0] = contract.Check{
			Name: "source", Status: "violated", Code: "SOURCE_SHA_MISMATCH",
			Detail: "the PNG is " + sha[:12] + ", the contract pins " + c.Source.SHA256[:12],
		}
		return &Inspection{Checks: checks}, nil
	}
	if src.Bounds().Dx() != c.Source.Width || src.Bounds().Dy() != c.Source.Height {
		checks[0] = contract.Check{
			Name: "source", Status: "violated", Code: "SOURCE_SIZE_MISMATCH",
			Detail: "the PNG changed dimensions",
		}
		return &Inspection{Checks: checks}, nil
	}
	scenePath := contract.Resolve(baseDir, c.Scene)
	if sceneOverride != "" {
		scenePath = sceneOverride
	}
	scene, cerr := godot.Parse(scenePath)
	if cerr != nil {
		return nil, cerr
	}
	checks = append(checks, godot.Validate(c.Facts, scene, c.Source.Width, c.Source.Height)...)

	receiptsDir := filepath.Join(baseDir, "receipts")
	check := contract.Check{Name: "chain", Status: "ok"}
	if cerr := receipt.VerifyChain(receiptsDir, sha); cerr != nil {
		check = contract.Check{Name: "chain", Status: "violated", Code: cerr.Code, Detail: cerr.Msg}
	}
	chain := []contract.Check{check}
	return &Inspection{Checks: checks, Chain: chain, Receipt: receipt.Count(receiptsDir)}, nil
}

// Failed returns the first violated check, if any.
func Failed(checks ...[]contract.Check) (contract.Check, bool) {
	for _, group := range checks {
		for _, c := range group {
			if c.Status != "ok" {
				return c, true
			}
		}
	}
	return contract.Check{}, false
}

func readPNG(path string) (*image.NRGBA, string, *contract.Error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", contract.Fault("IMAGE_UNREADABLE", "cannot read %s: %v", path, err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", contract.Fault("NOT_A_PNG", "%s is not a PNG: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return toNRGBA(img), hex.EncodeToString(sum[:]), nil
}

func encodePNG(img *image.NRGBA) ([]byte, string, *contract.Error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, "", contract.Fault("OUT_UNWRITABLE", "cannot encode PNG: %v", err)
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	n := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(n, n.Bounds(), img, b.Min, draw.Src)
	return n
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && aa == bb
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
