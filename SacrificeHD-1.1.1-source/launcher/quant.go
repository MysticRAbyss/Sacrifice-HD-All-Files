package main

import "sort"

// Palette quantization: median cut on a 5-bit histogram, refined with k-means,
// then a 15-bit lookup table maps pixels to the nearest entry.

type colorCount struct {
	r, g, b float64
	n       float64
}

func Quantize(samples [][]float32, ncolors int) [][3]uint8 {
	var hist [32768]float64
	var sum [32768][3]float64
	for _, px := range samples {
		for i := 0; i+2 < len(px); i += 3 {
			r, g, b := clamp8(px[i]), clamp8(px[i+1]), clamp8(px[i+2])
			k := int(r>>3)<<10 | int(g>>3)<<5 | int(b>>3)
			hist[k]++
			sum[k][0] += float64(r)
			sum[k][1] += float64(g)
			sum[k][2] += float64(b)
		}
	}
	var cs []colorCount
	for k, n := range hist {
		if n > 0 {
			cs = append(cs, colorCount{sum[k][0] / n, sum[k][1] / n, sum[k][2] / n, n})
		}
	}
	if len(cs) == 0 {
		return [][3]uint8{{0, 0, 0}}
	}
	boxes := [][]colorCount{cs}
	for len(boxes) < ncolors {
		// split the box with the largest weighted range
		bi, best := -1, 0.0
		for i, bx := range boxes {
			if len(bx) < 2 {
				continue
			}
			_, rng := widest(bx)
			var w float64
			for _, c := range bx {
				w += c.n
			}
			if s := rng * w; s > best {
				best, bi = s, i
			}
		}
		if bi < 0 {
			break
		}
		bx := boxes[bi]
		ax, _ := widest(bx)
		sort.Slice(bx, func(i, j int) bool { return ch(bx[i], ax) < ch(bx[j], ax) })
		var tot, acc float64
		for _, c := range bx {
			tot += c.n
		}
		cut := 1
		for i, c := range bx {
			acc += c.n
			if acc >= tot/2 {
				cut = i + 1
				break
			}
		}
		if cut >= len(bx) {
			cut = len(bx) - 1
		}
		boxes[bi] = bx[:cut]
		boxes = append(boxes, bx[cut:])
	}
	cent := make([][3]float64, len(boxes))
	for i, bx := range boxes {
		var w float64
		for _, c := range bx {
			cent[i][0] += c.r * c.n
			cent[i][1] += c.g * c.n
			cent[i][2] += c.b * c.n
			w += c.n
		}
		for k := 0; k < 3; k++ {
			cent[i][k] /= w
		}
	}
	// k-means refinement on the histogram cells
	for it := 0; it < 4; it++ {
		acc := make([][4]float64, len(cent))
		for _, c := range cs {
			j := nearestF(cent, c.r, c.g, c.b)
			acc[j][0] += c.r * c.n
			acc[j][1] += c.g * c.n
			acc[j][2] += c.b * c.n
			acc[j][3] += c.n
		}
		for j := range cent {
			if acc[j][3] > 0 {
				for k := 0; k < 3; k++ {
					cent[j][k] = acc[j][k] / acc[j][3]
				}
			}
		}
	}
	out := make([][3]uint8, len(cent))
	for i, c := range cent {
		out[i] = [3]uint8{clamp8(float32(c[0])), clamp8(float32(c[1])), clamp8(float32(c[2]))}
	}
	return out
}

func ch(c colorCount, ax int) float64 {
	switch ax {
	case 0:
		return c.r
	case 1:
		return c.g
	}
	return c.b
}

func widest(bx []colorCount) (int, float64) {
	lo := [3]float64{1e9, 1e9, 1e9}
	hi := [3]float64{-1, -1, -1}
	for _, c := range bx {
		for k, v := range [3]float64{c.r, c.g, c.b} {
			if v < lo[k] {
				lo[k] = v
			}
			if v > hi[k] {
				hi[k] = v
			}
		}
	}
	ax, r := 0, 0.0
	for k := 0; k < 3; k++ {
		if hi[k]-lo[k] > r {
			ax, r = k, hi[k]-lo[k]
		}
	}
	return ax, r
}

func nearestF(cent [][3]float64, r, g, b float64) int {
	bi, bd := 0, 1e18
	for i, c := range cent {
		dr, dg, db := c[0]-r, c[1]-g, c[2]-b
		d := dr*dr*0.3 + dg*dg*0.59 + db*db*0.11
		if d < bd {
			bd, bi = d, i
		}
	}
	return bi
}

// Mapper maps RGB to palette indices offset by `base` (entries below base are reserved).
type Mapper struct {
	lut  []uint8
	pal  [][3]uint8
	base int
}

func NewMapper(pal [][3]uint8, base int) *Mapper {
	m := &Mapper{lut: make([]uint8, 1<<18), pal: pal, base: base}
	cent := make([][3]float64, len(pal))
	for i, p := range pal {
		cent[i] = [3]float64{float64(p[0]), float64(p[1]), float64(p[2])}
	}
	for k := range m.lut {
		r := float64((k>>12)&63)*4 + 2
		g := float64((k>>6)&63)*4 + 2
		b := float64(k&63)*4 + 2
		m.lut[k] = uint8(nearestF(cent, r, g, b) + base)
	}
	return m
}

func (m *Mapper) Map(r, g, b float32) uint8 {
	k := int(clamp8(r)>>2)<<12 | int(clamp8(g)>>2)<<6 | int(clamp8(b)>>2)
	return m.lut[k]
}

// NewMapperIdx maps RGB to the nearest colour in pal, returning idx[i] for pal[i].
func NewMapperIdx(pal [][3]uint8, idx []int) *Mapper {
	m := NewMapper(pal, 0)
	for k, v := range m.lut {
		m.lut[k] = uint8(idx[v])
	}
	return m
}
