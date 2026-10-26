package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

// RGB float image, values 0..255.
type FImg struct {
	W, H int
	P    []float32 // 3 per pixel
}

func NewF(w, h int) *FImg { return &FImg{w, h, make([]float32, w*h*3)} }

func (f *FImg) ToNRGBA(mask []bool) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, f.W, f.H))
	for i := 0; i < f.W*f.H; i++ {
		for c := 0; c < 3; c++ {
			im.Pix[i*4+c] = clamp8(f.P[i*3+c])
		}
		im.Pix[i*4+3] = 255
		if mask != nil && mask[i] {
			im.Pix[i*4+3] = 0
		}
	}
	return im
}

func clamp8(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func FromImage(im image.Image) *FImg {
	b := im.Bounds()
	f := NewF(b.Dx(), b.Dy())
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			c := color.NRGBAModel.Convert(im.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			i := (y*f.W + x) * 3
			f.P[i], f.P[i+1], f.P[i+2] = float32(c.R), float32(c.G), float32(c.B)
		}
	}
	return f
}

func SavePNG(path string, im image.Image) error {
	fh, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(fh, im); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}

func LoadPNG(path string) (image.Image, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return png.Decode(fh)
}

// FillMasked replaces masked pixels by the average of unmasked neighbours (repeated), so an
// upscaler does not bleed the key colour into visible edges.
func FillMasked(f *FImg, mask []bool) {
	if mask == nil {
		return
	}
	m := append([]bool(nil), mask...)
	for pass := 0; pass < 8; pass++ {
		changed := false
		nm := append([]bool(nil), m...)
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				i := y*f.W + x
				if !m[i] {
					continue
				}
				var s [3]float32
				n := 0
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						xx, yy := x+dx, y+dy
						if xx < 0 || yy < 0 || xx >= f.W || yy >= f.H {
							continue
						}
						j := yy*f.W + xx
						if !m[j] {
							for c := 0; c < 3; c++ {
								s[c] += f.P[j*3+c]
							}
							n++
						}
					}
				}
				if n > 0 {
					for c := 0; c < 3; c++ {
						f.P[i*3+c] = s[c] / float32(n)
					}
					nm[i] = false
					changed = true
				}
			}
		}
		m = nm
		if !changed {
			break
		}
	}
}

func lanczos(x float64) float64 {
	const a = 3.0
	if x == 0 {
		return 1
	}
	if x <= -a || x >= a {
		return 0
	}
	px := math.Pi * x
	return a * math.Sin(px) * math.Sin(px/a) / (px * px)
}

// Resize with a separable Lanczos-3 filter (wrapping edges when tile is true).
func Resize(f *FImg, w, h int, tile bool) *FImg {
	tmp := resize1(f.P, f.W, f.H, w, true, tile)
	out := resize1(tmp, w, f.H, h, false, tile)
	return &FImg{w, h, out}
}

func resize1(src []float32, sw, sh, dn int, horiz bool, tile bool) []float32 {
	sn := sw
	if !horiz {
		sn = sh
	}
	scale := float64(sn) / float64(dn)
	support := 3.0
	if scale > 1 {
		support *= scale
	}
	var dw, dh int
	if horiz {
		dw, dh = dn, sh
	} else {
		dw, dh = sw, dn
	}
	dst := make([]float32, dw*dh*3)
	type tap struct {
		i int
		w float32
	}
	taps := make([][]tap, dn)
	for d := 0; d < dn; d++ {
		center := (float64(d)+0.5)*scale - 0.5
		lo := int(math.Floor(center - support))
		hi := int(math.Ceil(center + support))
		var ts []tap
		sum := 0.0
		for s := lo; s <= hi; s++ {
			x := float64(s) - center
			if scale > 1 {
				x /= scale
			}
			wt := lanczos(x)
			if wt == 0 {
				continue
			}
			si := s
			if tile {
				si = ((s % sn) + sn) % sn
			} else if si < 0 {
				si = 0
			} else if si >= sn {
				si = sn - 1
			}
			ts = append(ts, tap{si, float32(wt)})
			sum += wt
		}
		for k := range ts {
			ts[k].w /= float32(sum)
		}
		taps[d] = ts
	}
	if horiz {
		for y := 0; y < dh; y++ {
			for x := 0; x < dw; x++ {
				var r, g, b float32
				for _, t := range taps[x] {
					j := (y*sw + t.i) * 3
					r += src[j] * t.w
					g += src[j+1] * t.w
					b += src[j+2] * t.w
				}
				k := (y*dw + x) * 3
				dst[k], dst[k+1], dst[k+2] = r, g, b
			}
		}
	} else {
		for y := 0; y < dh; y++ {
			for x := 0; x < dw; x++ {
				var r, g, b float32
				for _, t := range taps[y] {
					j := (t.i*sw + x) * 3
					r += src[j] * t.w
					g += src[j+1] * t.w
					b += src[j+2] * t.w
				}
				k := (y*dw + x) * 3
				dst[k], dst[k+1], dst[k+2] = r, g, b
			}
		}
	}
	return dst
}

// blur is a separable gaussian used by the unsharp mask.
func blur(f *FImg, sigma float64) *FImg {
	r := int(math.Ceil(sigma * 3))
	k := make([]float32, 2*r+1)
	var s float32
	for i := -r; i <= r; i++ {
		k[i+r] = float32(math.Exp(-float64(i*i) / (2 * sigma * sigma)))
		s += k[i+r]
	}
	for i := range k {
		k[i] /= s
	}
	tmp := NewF(f.W, f.H)
	out := NewF(f.W, f.H)
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			var a [3]float32
			for i := -r; i <= r; i++ {
				xx := x + i
				if xx < 0 {
					xx = 0
				} else if xx >= f.W {
					xx = f.W - 1
				}
				j := (y*f.W + xx) * 3
				for c := 0; c < 3; c++ {
					a[c] += f.P[j+c] * k[i+r]
				}
			}
			copy(tmp.P[(y*f.W+x)*3:], a[:])
		}
	}
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			var a [3]float32
			for i := -r; i <= r; i++ {
				yy := y + i
				if yy < 0 {
					yy = 0
				} else if yy >= f.H {
					yy = f.H - 1
				}
				j := (yy*f.W + x) * 3
				for c := 0; c < 3; c++ {
					a[c] += tmp.P[j+c] * k[i+r]
				}
			}
			copy(out.P[(y*f.W+x)*3:], a[:])
		}
	}
	return out
}

// ApplyGrade: unsharp mask, contrast around mid-grey, saturation.
func ApplyGrade(f *FImg, g Grade) {
	if g.SharpenAmount > 0 && g.SharpenRadius > 0 {
		b := blur(f, g.SharpenRadius)
		for i := range f.P {
			f.P[i] += (f.P[i] - b.P[i]) * float32(g.SharpenAmount)
		}
	}
	for i := 0; i < len(f.P); i += 3 {
		r, gg, b := f.P[i], f.P[i+1], f.P[i+2]
		l := 0.299*r + 0.587*gg + 0.114*b
		sat := float32(g.Saturation)
		r = l + (r-l)*sat
		gg = l + (gg-l)*sat
		b = l + (b-l)*sat
		con := float32(g.Contrast)
		r = (r-128)*con + 128
		gg = (gg-128)*con + 128
		b = (b-128)*con + 128
		f.P[i], f.P[i+1], f.P[i+2] = clampF(r), clampF(gg), clampF(b)
	}
}

func clampF(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
