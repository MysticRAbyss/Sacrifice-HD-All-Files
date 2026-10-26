package main

// Reader/writer for Sacrifice (Shiny, 2000) .wad archives.
// Layout: "WAD>" u32 dirOffset u32 dirSize u32 0 u32 1, then the file bodies in directory order,
// then a zlib-compressed directory of 28-byte entries:
// name[4] type[4] (both stored reversed) compSize uncompSize count flags time.
// flags bit0 = folder (owns the next `count` entries, depth first), bit1 = body is zlib-compressed.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

type Entry struct {
	Name, Type         string
	CSize, USize       uint32
	Count, Flags, Time uint32
	Children           []*Entry
	Raw                []byte // body as stored
	data               []byte // decoded body (lazy)
	dirty              bool
	Path               string
}

func (e *Entry) IsDir() bool { return e.Flags&1 != 0 }

func (e *Entry) Data() ([]byte, error) {
	if e.data != nil {
		return e.data, nil
	}
	if e.Flags&2 == 0 {
		e.data = e.Raw
		return e.data, nil
	}
	r, err := zlib.NewReader(bytes.NewReader(e.Raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.Path, err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.Path, err)
	}
	e.data = b
	return b, nil
}

func (e *Entry) SetData(b []byte) { e.data = b; e.dirty = true }

func (e *Entry) Walk(fn func(*Entry)) {
	fn(e)
	for _, c := range e.Children {
		c.Walk(fn)
	}
}

type Wad struct {
	Root   *Entry
	H0, H1 uint32
}

func rev4(b []byte) string { return string([]byte{b[3], b[2], b[1], b[0]}) }
func unrev4(s string) []byte {
	b := []byte(s + "\x00\x00\x00\x00")[:4]
	return []byte{b[3], b[2], b[1], b[0]}
}

func ReadWad(path string) (*Wad, error) {
	d, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(d) < 20 || string(d[:4]) != "WAD>" {
		return nil, errors.New(path + ": not a Sacrifice wad")
	}
	off := binary.LittleEndian.Uint32(d[4:])
	h0 := binary.LittleEndian.Uint32(d[12:])
	h1 := binary.LittleEndian.Uint32(d[16:])
	if int(off) > len(d) {
		return nil, errors.New(path + ": bad directory offset")
	}
	zr, err := zlib.NewReader(bytes.NewReader(d[off:]))
	if err != nil {
		return nil, err
	}
	dir, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var ents []*Entry
	for i := 0; i+28 <= len(dir); i += 28 {
		u := func(k int) uint32 { return binary.LittleEndian.Uint32(dir[i+k:]) }
		ents = append(ents, &Entry{Name: rev4(dir[i : i+4]), Type: rev4(dir[i+4 : i+8]),
			CSize: u(8), USize: u(12), Count: u(16), Flags: u(20), Time: u(24)})
	}
	k := 0
	var build func(prefix string) (*Entry, error)
	build = func(prefix string) (*Entry, error) {
		if k >= len(ents) {
			return nil, errors.New(path + ": truncated directory")
		}
		e := ents[k]
		k++
		e.Path = prefix + "/" + e.Name
		if e.IsDir() {
			for j := uint32(0); j < e.Count; j++ {
				c, err := build(e.Path)
				if err != nil {
					return nil, err
				}
				e.Children = append(e.Children, c)
			}
		}
		return e, nil
	}
	root, err := build("")
	if err != nil {
		return nil, err
	}
	pos := uint32(20)
	var berr error
	root.Walk(func(e *Entry) {
		if e.IsDir() || berr != nil {
			return
		}
		if pos+e.CSize > off {
			berr = errors.New(path + ": body overruns directory")
			return
		}
		e.Raw = d[pos : pos+e.CSize]
		pos += e.CSize
	})
	if berr != nil {
		return nil, berr
	}
	return &Wad{Root: root, H0: h0, H1: h1}, nil
}

func (w *Wad) Write(path string) error {
	var body bytes.Buffer
	var werr error
	w.Root.Walk(func(e *Entry) {
		if e.IsDir() || werr != nil {
			return
		}
		if e.dirty {
			var zb bytes.Buffer
			zw, _ := zlib.NewWriterLevel(&zb, 6)
			zw.Write(e.data)
			zw.Close()
			e.Raw = zb.Bytes()
			e.USize = uint32(len(e.data))
			e.Flags |= 2
			e.dirty = false
		}
		e.CSize = uint32(len(e.Raw))
		body.Write(e.Raw)
	})
	if werr != nil {
		return werr
	}
	var sizes func(e *Entry) (uint32, uint32)
	sizes = func(e *Entry) (uint32, uint32) {
		if !e.IsDir() {
			return e.CSize, e.USize
		}
		var c, u uint32
		for _, ch := range e.Children {
			a, b := sizes(ch)
			c += a
			u += b
		}
		e.CSize, e.USize, e.Count = c, u, uint32(len(e.Children))
		return c, u
	}
	sizes(w.Root)
	var dir bytes.Buffer
	w.Root.Walk(func(e *Entry) {
		dir.Write(unrev4(e.Name))
		dir.Write(unrev4(e.Type))
		for _, v := range []uint32{e.CSize, e.USize, e.Count, e.Flags, e.Time} {
			binary.Write(&dir, binary.LittleEndian, v)
		}
	})
	var zd bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&zd, 9)
	zw.Write(dir.Bytes())
	zw.Close()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	hdr := make([]byte, 20)
	copy(hdr, "WAD>")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(20+body.Len()))
	binary.LittleEndian.PutUint32(hdr[8:], uint32(dir.Len()))
	binary.LittleEndian.PutUint32(hdr[12:], w.H0)
	binary.LittleEndian.PutUint32(hdr[16:], w.H1)
	_, e1 := f.Write(hdr)
	_, e2 := f.Write(body.Bytes())
	_, e3 := f.Write(zd.Bytes())
	e4 := f.Close()
	return errors.Join(e1, e2, e3, e4)
}
