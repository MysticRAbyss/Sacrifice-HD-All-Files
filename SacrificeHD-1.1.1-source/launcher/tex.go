package main

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// Paletted texture chunk (TXTR / ICON): 36-byte header then w*h palette indices.
// header u32: [0]w [1]h [2]tag (name of the PALT chunk, reversed fourcc) [3]flags [4]? [5]frames [6]frameW [7]frameH [8]?
// flags bit 2 (0x4) = index 0 is transparent (colour key).
type PalTex struct {
	E              *Entry
	W, H           int
	Tag            string
	Flags          uint32
	Frames, FW, FH int
	Idx            []uint8
	Tail           []byte
}

func ParsePalTex(e *Entry) (*PalTex, error) {
	d, err := e.Data()
	if err != nil {
		return nil, err
	}
	if len(d) < 36 {
		return nil, fmt.Errorf("%s: short texture", e.Path)
	}
	u := func(i int) int { return int(binary.LittleEndian.Uint32(d[i*4:])) }
	t := &PalTex{E: e, W: u(0), H: u(1), Tag: rev4(d[8:12]), Flags: uint32(u(3)), Frames: u(5), FW: u(6), FH: u(7)}
	n := t.W * t.H
	if t.W <= 0 || t.H <= 0 || t.W > 4096 || t.H > 4096 || 36+n > len(d) {
		return nil, fmt.Errorf("%s: bad texture size %dx%d", e.Path, t.W, t.H)
	}
	t.Idx = d[36 : 36+n]
	t.Tail = d[36+n:]
	return t, nil
}

func (t *PalTex) Keyed() bool { return t.Flags&4 != 0 }

// Frames as rectangles in the atlas.
func (t *PalTex) FrameRects() [][4]int {
	if t.Frames > 1 && t.FW > 0 && t.FH > 0 && t.W%t.FW == 0 && t.H%t.FH == 0 && (t.W/t.FW)*(t.H/t.FH) >= t.Frames {
		var r [][4]int
		for y := 0; y < t.H; y += t.FH {
			for x := 0; x < t.W; x += t.FW {
				r = append(r, [4]int{x, y, t.FW, t.FH})
			}
		}
		return r
	}
	return [][4]int{{0, 0, t.W, t.H}}
}

func (t *PalTex) Encode(scale int, idx []uint8, keepFrame bool) []byte {
	d, _ := t.E.Data()
	h := append([]byte(nil), d[:36]...)
	binary.LittleEndian.PutUint32(h[0:], uint32(t.W*scale))
	binary.LittleEndian.PutUint32(h[4:], uint32(t.H*scale))
	if t.FW > 0 && t.FH > 0 && !keepFrame {
		binary.LittleEndian.PutUint32(h[24:], uint32(t.FW*scale))
		binary.LittleEndian.PutUint32(h[28:], uint32(t.FH*scale))
	}
	out := append(h, idx...)
	if t.HasAlpha() && scale > 1 {
		return append(out, t.ScaleAlpha(scale)...)
	}
	return append(out, t.Tail...)
}

// HasAlpha: textures with flag 0x10000 carry an 8-bit alpha plane (W*H bytes) after the indices.
func (t *PalTex) HasAlpha() bool { return t.Flags&0x10000 != 0 && len(t.Tail) == t.W*t.H }

// AlphaZero marks pixels of a frame that are fully transparent in the alpha plane.
func (t *PalTex) AlphaZero(r [4]int) []bool {
	m := make([]bool, r[2]*r[3])
	for y := 0; y < r[3]; y++ {
		for x := 0; x < r[2]; x++ {
			m[y*r[2]+x] = t.Tail[(r[1]+y)*t.W+r[0]+x] == 0
		}
	}
	return m
}

// ScaleAlpha resizes the alpha plane frame by frame so it matches the upscaled indices.
func (t *PalTex) ScaleAlpha(scale int) []byte {
	W := t.W * scale
	out := make([]byte, W*t.H*scale)
	for _, r := range t.FrameRects() {
		f := NewF(r[2], r[3])
		for y := 0; y < r[3]; y++ {
			for x := 0; x < r[2]; x++ {
				f.P[(y*r[2]+x)*3] = float32(t.Tail[(r[1]+y)*t.W+r[0]+x])
			}
		}
		g := Resize(f, r[2]*scale, r[3]*scale, false)
		for y := 0; y < g.H; y++ {
			for x := 0; x < g.W; x++ {
				out[(r[1]*scale+y)*W+r[0]*scale+x] = clamp8(g.P[(y*g.W+x)*3])
			}
		}
	}
	return out
}

// PALT chunk: u32 a, u32 b, 256 RGB triples, then the source file name.
func PaletteOf(e *Entry) ([256][3]uint8, error) {
	var p [256][3]uint8
	d, err := e.Data()
	if err != nil {
		return p, err
	}
	if len(d) < 8+768 {
		return p, fmt.Errorf("%s: short palette", e.Path)
	}
	for i := 0; i < 256; i++ {
		copy(p[i][:], d[8+i*3:8+i*3+3])
	}
	return p, nil
}

func SetPalette(e *Entry, p [256][3]uint8) {
	d, _ := e.Data()
	nd := append([]byte(nil), d...)
	for i := 0; i < 256; i++ {
		copy(nd[8+i*3:], p[i][:])
	}
	e.SetData(nd)
}

func grayPalette() [256][3]uint8 {
	var p [256][3]uint8
	for i := range p {
		p[i] = [3]uint8{uint8(i), uint8(i), uint8(i)}
	}
	return p
}

// FindPalette resolves a texture's palette tag: the PALT chunk with that name in the same folder or the
// nearest enclosing folder. Returns nil for the engine's built-in grey ramp ("GrAy").
func FindPalette(index map[string]map[string]*Entry, path, tag string) *Entry {
	parts := strings.Split(path, "/")
	parts = parts[:len(parts)-1]
	for len(parts) > 0 {
		f := strings.Join(parts, "/")
		if m, ok := index[f]; ok {
			if e, ok := m[tag]; ok {
				return e
			}
		}
		parts = parts[:len(parts)-1]
	}
	return nil
}

func PaletteIndex(w *Wad) map[string]map[string]*Entry {
	idx := map[string]map[string]*Entry{}
	w.Root.Walk(func(e *Entry) {
		if e.Type == "PALT" && !e.IsDir() {
			f := e.Path[:strings.LastIndex(e.Path, "/")]
			if idx[f] == nil {
				idx[f] = map[string]*Entry{}
			}
			idx[f][e.Name] = e
		}
	})
	return idx
}

// SXTX chunk: an uncompressed 24-bit TGA (type 2), bottom-up rows of BGR.
type TGA struct {
	E    *Entry
	W, H int
	Hdr  []byte
	Pix  []byte // BGR
	Tail []byte
}

func ParseTGA(e *Entry) (*TGA, error) {
	d, err := e.Data()
	if err != nil {
		return nil, err
	}
	if len(d) < 18 || d[2] != 2 || d[16] != 24 {
		return nil, fmt.Errorf("%s: unsupported TGA", e.Path)
	}
	idl := int(d[0])
	w := int(binary.LittleEndian.Uint16(d[12:]))
	h := int(binary.LittleEndian.Uint16(d[14:]))
	off := 18 + idl
	n := w * h * 3
	if off+n > len(d) {
		return nil, fmt.Errorf("%s: truncated TGA", e.Path)
	}
	return &TGA{E: e, W: w, H: h, Hdr: d[:off], Pix: d[off : off+n], Tail: d[off+n:]}, nil
}

func (t *TGA) ToF() *FImg {
	f := NewF(t.W, t.H)
	for i := 0; i < t.W*t.H; i++ {
		f.P[i*3], f.P[i*3+1], f.P[i*3+2] = float32(t.Pix[i*3+2]), float32(t.Pix[i*3+1]), float32(t.Pix[i*3])
	}
	return f
}

func (t *TGA) Encode(f *FImg) []byte {
	h := append([]byte(nil), t.Hdr...)
	binary.LittleEndian.PutUint16(h[12:], uint16(f.W))
	binary.LittleEndian.PutUint16(h[14:], uint16(f.H))
	px := make([]byte, f.W*f.H*3)
	for i := 0; i < f.W*f.H; i++ {
		px[i*3+2], px[i*3+1], px[i*3] = clamp8(f.P[i*3]), clamp8(f.P[i*3+1]), clamp8(f.P[i*3+2])
	}
	out := append(h, px...)
	return append(out, t.Tail...)
}
