package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// One unit of upscaling work: a frame of a texture written to <work>/in<scale>/<id>.png.
type job struct {
	id      string
	scale   int
	src     *FImg
	mask    []bool // colour-keyed pixels (pal8 only)
	out     *FImg
	outPath string
	omask   []bool
}

type wadPlan struct {
	rel   string // path relative to data dir, e.g. "main.wad"
	wad   *Wad
	pals  []*palGroup
	tgas  []*tgaItem
	land  []*Entry // terrain palettes regraded in place
	grade map[*Entry]Grade
}

type palGroup struct {
	locked bool   // palette also used by textures we leave alone: map into it, never rewrite it
	palt   *Entry // nil = built-in grey ramp
	texs   []*palItem
	grade  Grade
}

type palItem struct {
	keepFrame bool      // keep the frame size in the header (the game draws some textures at frame size)
	keyIdx    int       // main palette index of colour-keyed pixels (-1 = none)
	keySet    [256]bool // every key-candidate index present (index 0 or pure black); kept exactly
	t         *PalTex
	scale     int
	frames    []*job
	rects     [][4]int
}

type tgaItem struct {
	t     *TGA
	scale int
	j     *job
	grade Grade
	same  bool // enhance at the original size: upscale for detail, then scale back down
}

func matchSpec(spec, rel, path string) bool {
	w, pre, _ := strings.Cut(spec, ":")
	ok, _ := filepath.Match(strings.ToLower(w), strings.ToLower(rel))
	return ok && (pre == "" || strings.HasPrefix(path, pre))
}

func classFor(rel, path, typ string) *AssetClass {
	for i := range AssetClasses {
		a := &AssetClasses[i]
		if a.ChunkType != typ {
			continue
		}
		ex := false
		for _, s := range a.Excludes {
			if matchSpec(s, rel, path) {
				ex = true
			}
		}
		if ex {
			return nil
		}
		for _, s := range a.Sources {
			if matchSpec(s, rel, path) {
				return a
			}
		}
	}
	return nil
}

func scaleFor(a *AssetClass, w, h int) int {
	long := w
	if h > long {
		long = h
	}
	s := a.ScaleLarge
	if long <= 128 {
		s = a.ScaleSmall
	}
	for s > 1 && long*s > a.MaxDim {
		s /= 2
	}
	return s
}

// planWad decodes every texture the sheets cover in one wad.
func planWad(dataDir, rel string) (*wadPlan, error) {
	w, err := ReadWad(filepath.Join(dataDir, rel))
	if err != nil {
		return nil, err
	}
	p := &wadPlan{rel: rel, wad: w}
	pidx := PaletteIndex(w)
	groups := map[*Entry]*palGroup{}
	grey := map[string]*palGroup{}
	var ferr error
	shared := map[*Entry]bool{}
	skippedTGA := map[string]int{} // character folders with a page too big to upscale
	w.Root.Walk(func(e *Entry) {
		if e.IsDir() || ferr != nil {
			return
		}
		a := classFor(rel, e.Path, e.Type)
		if a == nil {
			if e.Type == "TXTR" || e.Type == "ICON" {
				if t, err := ParsePalTex(e); err == nil {
					if pe := FindPalette(pidx, e.Path, t.Tag); pe != nil {
						shared[pe] = true
					}
				}
			}
			return
		}
		g := Grades[a.Grade]
		switch a.Format {
		case "tga24_same":
			// Creature/wizard skins keep their original size: the engine packs every loaded creature's
			// pages together, and enlarged pages made other creatures render broken (Netherfiend black,
			// Phoenix/Silverback wings) depending on which HD creatures were loaded alongside them.
			t, err := ParseTGA(e)
			if err != nil {
				logf("skip %s: %v", e.Path, err)
				return
			}
			it := &tgaItem{t: t, scale: 2, grade: g, same: true}
			it.j = &job{id: fmt.Sprintf("t%04d", len(p.tgas)), scale: 2, src: t.ToF()}
			p.tgas = append(p.tgas, it)
		case "tga24":
			t, err := ParseTGA(e)
			if err != nil {
				logf("skip %s: %v", e.Path, err)
				return
			}
			s := scaleFor(a, t.W, t.H)
			if s < 2 {
				skippedTGA[path.Dir(e.Path)] = s
				return
			}
			it := &tgaItem{t: t, scale: s, grade: g}
			it.j = &job{id: fmt.Sprintf("t%04d", len(p.tgas)), scale: s, src: t.ToF()}
			p.tgas = append(p.tgas, it)
		case "pal8":
			t, err := ParsePalTex(e)
			if err != nil {
				logf("skip %s: %v", e.Path, err)
				return
			}
			s := scaleFor(a, t.W, t.H)
			pe := FindPalette(pidx, e.Path, t.Tag)
			if len(t.Tail) > 0 && !t.HasAlpha() {
				logf("keep %s: unknown extra data (%d bytes)", e.Path, len(t.Tail))
				s = 1
			}
			if s < 2 {
				if pe != nil {
					shared[pe] = true // left alone: its palette must not be rewritten
				}
				return
			}
			var grp *palGroup
			if pe == nil {
				key := e.Path
				grp = grey[key]
				if grp == nil {
					grp = &palGroup{grade: g}
					grey[key] = grp
					p.pals = append(p.pals, grp)
				}
			} else {
				grp = groups[pe]
				if grp == nil {
					grp = &palGroup{palt: pe, grade: g}
					groups[pe] = grp
					p.pals = append(p.pals, grp)
				}
				if a.Palette == "keep_original" {
					grp.locked = true // index order matters to the game (menu widgets): map into the original palette
				}
			}
			grp.texs = append(grp.texs, &palItem{t: t, scale: s, keepFrame: a.FrameHeader == "keep"})
		case "palette_land":
			if e.Type == "PALT" && e.Name == "LAND" {
				p.land = append(p.land, e)
				if p.grade == nil {
					p.grade = map[*Entry]Grade{}
				}
				p.grade[e] = g
			}
		}
	})
	// A character's texture pages are used together: they must all get the same scale, or parts of the
	// model (wings, limbs) sample the wrong place. Use the smallest scale any page in the folder allows.
	minS := map[string]int{}
	for _, it := range p.tgas {
		if it.same {
			continue
		}
		d := path.Dir(it.t.E.Path)
		if s, ok := minS[d]; !ok || it.scale < s {
			minS[d] = it.scale
		}
	}
	for d, s := range skippedTGA {
		if s < minS[d] || minS[d] == 0 {
			minS[d] = s
		}
	}
	kept := p.tgas[:0]
	for _, it := range p.tgas {
		if it.same {
			it.j.id = fmt.Sprintf("t%04d", len(kept))
			kept = append(kept, it)
			continue
		}
		s := minS[path.Dir(it.t.E.Path)]
		if s < 2 {
			continue
		}
		it.scale, it.j.scale = s, s
		it.j.id = fmt.Sprintf("t%04d", len(kept))
		kept = append(kept, it)
	}
	p.tgas = kept
	for _, grp := range p.pals {
		if grp.palt != nil && shared[grp.palt] {
			grp.locked = true
		}
	}
	// decode pal8 frames to RGB
	for gi, grp := range p.pals {
		pal := grayPalette()
		if grp.palt != nil {
			if pal, err = PaletteOf(grp.palt); err != nil {
				return nil, err
			}
		}
		for ti, it := range grp.texs {
			it.rects = it.t.FrameRects()
			// Colour key: the game hides index 0 or pure black (palettes sorted dark->bright key index 0,
			// bright->dark ones key index 255). Keep those pixels on their original index.
			it.keyIdx = -1
			if it.t.Keyed() {
				var cnt [256]int
				for _, ix := range it.t.Idx {
					if ix == 0 || pal[ix] == [3]uint8{} {
						cnt[ix]++
						it.keySet[ix] = true
					}
				}
				best := 0
				for i := range cnt {
					if cnt[i] > best {
						best, it.keyIdx = cnt[i], i
					}
				}
			}
			for fi, r := range it.rects {
				f := NewF(r[2], r[3])
				var mask []bool
				if it.keyIdx >= 0 {
					mask = make([]bool, r[2]*r[3])
				}
				for y := 0; y < r[3]; y++ {
					for x := 0; x < r[2]; x++ {
						ix := it.t.Idx[(r[1]+y)*it.t.W+r[0]+x]
						k := y*r[2] + x
						c := pal[ix]
						f.P[k*3], f.P[k*3+1], f.P[k*3+2] = float32(c[0]), float32(c[1]), float32(c[2])
						if mask != nil && (ix == 0 || pal[ix] == [3]uint8{}) {
							mask[k] = true
						}
					}
				}
				FillMasked(f, mask)
				if mask == nil && it.t.HasAlpha() {
					// give fully transparent pixels their neighbours' colour so edges don't halo
					FillMasked(f, it.t.AlphaZero(r))
				}
				it.frames = append(it.frames, &job{id: fmt.Sprintf("p%03d_%03d_%02d", gi, ti, fi), scale: it.scale, src: f, mask: mask})
			}
		}
	}
	return p, nil
}

func (p *wadPlan) jobs() []*job {
	var js []*job
	for _, it := range p.tgas {
		js = append(js, it.j)
	}
	for _, g := range p.pals {
		for _, it := range g.texs {
			js = append(js, it.frames...)
		}
	}
	return js
}

func upscaleMask(m []bool, w, h, s int) []bool {
	if m == nil {
		return nil
	}
	f := NewF(w, h)
	for i, v := range m {
		if v {
			f.P[i*3] = 255
		}
	}
	r := Resize(f, w*s, h*s, false)
	out := make([]bool, w*s*h*s)
	for i := range out {
		out[i] = r.P[i*3] > 127
	}
	return out
}

// finish grades the upscaled jobs and writes them back into the wad.
func (p *wadPlan) finish(outPath string) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, it := range p.tgas {
		it := it
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			o := it.j.result()
			if it.same {
				o = Resize(o, it.t.W, it.t.H, false)
			}
			ApplyGrade(o, it.grade)
			if it.same {
				// Keep the engine's see-through test exactly as in the original: it converts skins to 16-bit,
				// where any colour below (8,4,8) becomes 0 = transparent. Pixels in that range keep their
				// original value; every other pixel is kept out of it.
				src := it.t.ToF()
				dark := func(r, g, b uint8) bool { return r < 8 && g < 4 && b < 8 }
				for i := 0; i < it.t.W*it.t.H; i++ {
					sr, sg, sb := clamp8(src.P[i*3]), clamp8(src.P[i*3+1]), clamp8(src.P[i*3+2])
					if dark(sr, sg, sb) {
						o.P[i*3], o.P[i*3+1], o.P[i*3+2] = src.P[i*3], src.P[i*3+1], src.P[i*3+2]
					} else if dark(clamp8(o.P[i*3]), clamp8(o.P[i*3+1]), clamp8(o.P[i*3+2])) {
						o.P[i*3+1] = 4
					}
				}
			}
			it.t.E.SetData(it.t.Encode(o))
			it.j.out, it.j.src = nil, nil
		}()
	}
	wg.Wait()
	for _, grp := range p.pals {
		var samples [][]float32
		for _, it := range grp.texs {
			for _, j := range it.frames {
				ApplyGrade(j.result(), grp.grade)
				j.omask = upscaleMask(j.mask, j.src.W, j.src.H, j.scale)
				if j.omask == nil {
					samples = append(samples, j.out.P)
				} else {
					var px []float32
					for i, k := range j.omask {
						if !k {
							px = append(px, j.out.P[i*3:i*3+3]...)
						}
					}
					samples = append(samples, px)
				}
			}
		}
		var newPal [256][3]uint8
		var m *Mapper
		if grp.palt == nil {
			// grey ramp palette is fixed: map luminance directly
			newPal = grayPalette()
		} else {
			old, _ := PaletteOf(grp.palt)
			// reserved: index 0 and every key index stay exactly as they were
			reserved := map[int]bool{0: true}
			keyed := false
			for _, it := range grp.texs {
				if it.keyIdx >= 0 {
					for i, k := range it.keySet {
						if k {
							reserved[i] = true
						}
					}
					keyed = true
				}
			}
			isKey := func(c [3]uint8) bool { return keyed && c == [3]uint8{} }
			var free []int
			for i := 0; i < 256; i++ {
				if reserved[i] {
					newPal[i] = old[i]
				} else if !grp.locked || !isKey(old[i]) {
					free = append(free, i)
				}
			}
			if grp.locked {
				newPal = old
				var cols [][3]uint8
				for _, i := range free {
					cols = append(cols, old[i])
				}
				m = NewMapperIdx(cols, free)
			} else {
				q := Quantize(samples, len(free))
				for i, c := range q {
					if isKey(c) {
						c = [3]uint8{1, 1, 1} // opaque pixels must never land on the key colour
						q[i] = c
					}
					newPal[free[i]] = c
				}
				m = NewMapperIdx(q, free[:len(q)])
			}
		}
		for _, it := range grp.texs {
			W, H := it.t.W*it.scale, it.t.H*it.scale
			idx := make([]uint8, W*H)
			for fi, r := range it.rects {
				j := it.frames[fi]
				ox, oy := r[0]*it.scale, r[1]*it.scale
				fw := j.out.W
				for y := 0; y < j.out.H; y++ {
					for x := 0; x < fw; x++ {
						k := y*fw + x
						var v uint8
						if j.omask != nil && j.omask[k] {
							// keep the source pixel's own key index (index 0 and black may both occur)
							v = uint8(it.keyIdx)
							if six := it.t.Idx[(r[1]+y/it.scale)*it.t.W+r[0]+x/it.scale]; it.keySet[six] {
								v = six
							}
						} else if m == nil {
							l := 0.299*j.out.P[k*3] + 0.587*j.out.P[k*3+1] + 0.114*j.out.P[k*3+2]
							v = clamp8(l)
						} else {
							v = m.Map(j.out.P[k*3], j.out.P[k*3+1], j.out.P[k*3+2])
						}
						idx[(oy+y)*W+ox+x] = v
					}
				}
				j.out, j.src = nil, nil
			}
			it.t.E.SetData(it.t.Encode(it.scale, idx, it.keepFrame))
		}
		if grp.palt != nil && !grp.locked {
			SetPalette(grp.palt, newPal)
		}
	}
	for _, e := range p.land {
		pal, err := PaletteOf(e)
		if err != nil {
			return err
		}
		g := p.grade[e]
		f := NewF(256, 1)
		for i := 0; i < 256; i++ {
			f.P[i*3], f.P[i*3+1], f.P[i*3+2] = float32(pal[i][0]), float32(pal[i][1]), float32(pal[i][2])
		}
		g.SharpenAmount = 0
		ApplyGrade(f, g)
		for i := 0; i < 256; i++ {
			pal[i] = [3]uint8{clamp8(f.P[i*3]), clamp8(f.P[i*3+1]), clamp8(f.P[i*3+2])}
		}
		SetPalette(e, pal)
	}
	os.MkdirAll(filepath.Dir(outPath), 0o755)
	tmp := outPath + ".tmp"
	if err := p.wad.Write(tmp); err != nil {
		return err
	}
	os.Remove(outPath)
	return os.Rename(tmp, outPath)
}

// sourceWads lists every wad (relative to data dir) that some asset class touches.
func sourceWads(dataDir string) []string {
	seen := map[string]bool{}
	var out []string
	var all []string
	filepath.Walk(dataDir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && strings.EqualFold(filepath.Ext(p), ".wad") {
			r, _ := filepath.Rel(dataDir, p)
			all = append(all, filepath.ToSlash(r))
		}
		return nil
	})
	for _, rel := range all {
		for _, a := range AssetClasses {
			for _, s := range a.Sources {
				w, _, _ := strings.Cut(s, ":")
				ok, _ := filepath.Match(strings.ToLower(w), strings.ToLower(rel))
				for _, x := range a.Excludes { // a whole-wad exclude means the class leaves this wad alone
					if !strings.Contains(x, ":") {
						if ex, _ := filepath.Match(strings.ToLower(x), strings.ToLower(rel)); ex {
							ok = false
						}
					}
				}
				if ok && !seen[rel] {
					seen[rel] = true
					out = append(out, rel)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}
