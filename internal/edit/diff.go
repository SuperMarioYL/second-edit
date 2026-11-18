package edit

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"math/bits"

	"github.com/SuperMarioYL/second-edit/internal/contract"
)

// Mask is a 1-bit pixel set over an image.
type Mask struct {
	W, H   int
	stride int
	bits   []uint64
}

// NewMask allocates an empty mask.
func NewMask(w, h int) *Mask {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	stride := (w + 63) / 64
	return &Mask{W: w, H: h, stride: stride, bits: make([]uint64, stride*h)}
}

// Set marks a pixel; out-of-bounds writes are ignored.
func (m *Mask) Set(x, y int) {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return
	}
	m.bits[y*m.stride+x/64] |= 1 << uint(x%64)
}

// Get reports whether a pixel is marked.
func (m *Mask) Get(x, y int) bool {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return false
	}
	return m.bits[y*m.stride+x/64]&(1<<uint(x%64)) != 0
}

// Count is the number of marked pixels.
func (m *Mask) Count() int {
	n := 0
	for _, w := range m.bits {
		n += bits.OnesCount64(w)
	}
	return n
}

// Dilate grows the mask by r pixels in each direction (a square kernel).
func (m *Mask) Dilate(r int) *Mask {
	if r <= 0 {
		return m
	}
	rows := NewMask(m.W, m.H)
	for y := 0; y < m.H; y++ {
		for x := 0; x < m.W; x++ {
			if !m.Get(x, y) {
				continue
			}
			for dx := -r; dx <= r; dx++ {
				rows.Set(x+dx, y)
			}
		}
	}
	out := NewMask(m.W, m.H)
	for y := 0; y < m.H; y++ {
		for x := 0; x < m.W; x++ {
			if !rows.Get(x, y) {
				continue
			}
			for dy := -r; dy <= r; dy++ {
				out.Set(x, y+dy)
			}
		}
	}
	return out
}

// Rasterize turns a region into a mask. Polygon regions are sampled at pixel
// centres; rect regions cover their rectangle.
func Rasterize(r contract.Region, w, h int) *Mask {
	m := NewMask(w, h)
	rect := r.Rect
	if r.Shape == "polygon" && len(r.Polygon) >= 3 {
		for y := rect.Y; y < rect.Y+rect.H; y++ {
			for x := rect.X; x < rect.X+rect.W; x++ {
				if pointInPolygon(float64(x)+0.5, float64(y)+0.5, r.Polygon) {
					m.Set(x, y)
				}
			}
		}
		return m
	}
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		for x := rect.X; x < rect.X+rect.W; x++ {
			m.Set(x, y)
		}
	}
	return m
}

func pointInPolygon(x, y float64, poly [][2]float64) bool {
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

// FirstOutside reports how many patch pixels differ from the source outside the
// mask, and where the first one sits in source coordinates.
func FirstOutside(src, patch *image.NRGBA, offX, offY int, mask *Mask) (int, image.Point, bool) {
	pb := patch.Bounds()
	count := 0
	first := image.Point{}
	for y := pb.Min.Y; y < pb.Max.Y; y++ {
		for x := pb.Min.X; x < pb.Max.X; x++ {
			sx, sy := offX+x, offY+y
			if mask.Get(sx, sy) {
				continue
			}
			if !samePixel(src, patch, sx, sy, x, y) {
				if count == 0 {
					first = image.Pt(sx, sy)
				}
				count++
			}
		}
	}
	return count, first, count > 0
}

// Composite copies the patch into the source for every masked pixel and leaves
// every other pixel untouched.
func Composite(src, patch *image.NRGBA, offX, offY int, mask *Mask) *image.NRGBA {
	out := image.NewNRGBA(src.Bounds())
	copy(out.Pix, src.Pix)
	pb := patch.Bounds()
	for y := pb.Min.Y; y < pb.Max.Y; y++ {
		for x := pb.Min.X; x < pb.Max.X; x++ {
			sx, sy := offX+x, offY+y
			if !mask.Get(sx, sy) {
				continue
			}
			si := out.PixOffset(sx, sy)
			pi := patch.PixOffset(x, y)
			copy(out.Pix[si:si+4], patch.Pix[pi:pi+4])
		}
	}
	return out
}

// ChangedInMask counts pixels that differ from the source inside the mask.
func ChangedInMask(src, out *image.NRGBA, mask *Mask) int {
	n := 0
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !mask.Get(x, y) {
				continue
			}
			i := src.PixOffset(x, y)
			j := out.PixOffset(x, y)
			if src.Pix[i] != out.Pix[j] || src.Pix[i+1] != out.Pix[j+1] ||
				src.Pix[i+2] != out.Pix[j+2] || src.Pix[i+3] != out.Pix[j+3] {
				n++
			}
		}
	}
	return n
}

// UntouchedHash hashes every pixel outside the mask, in row-major order. This is
// the proof a mask cannot give: the complement of the edit is byte-identical.
func UntouchedHash(src *image.NRGBA, mask *Mask) string {
	h := sha256.New()
	var header [8]byte
	b := src.Bounds()
	binary.BigEndian.PutUint32(header[0:4], uint32(b.Dx()))
	binary.BigEndian.PutUint32(header[4:8], uint32(b.Dy()))
	h.Write(header[:])
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if mask.Get(x, y) {
				continue
			}
			i := src.PixOffset(x, y)
			h.Write(src.Pix[i : i+4])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func samePixel(a, b *image.NRGBA, ax, ay, bx, by int) bool {
	i := a.PixOffset(ax, ay)
	j := b.PixOffset(bx, by)
	return a.Pix[i] == b.Pix[j] && a.Pix[i+1] == b.Pix[j+1] &&
		a.Pix[i+2] == b.Pix[j+2] && a.Pix[i+3] == b.Pix[j+3]
}
