package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const cacheVersion = "sachd-16"

type srcInfo struct {
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
	Rules string `json:"rules"` // hash of the sheet rows that apply to this wad
}

type manifest struct {
	Version string             `json:"version"`
	Sources map[string]srcInfo `json:"sources"`
	Backend string             `json:"backend"`
}

type layout struct {
	game, data, mod, cache, backup, journal, gameCacheHD string
}

func newLayout(game, mod string) layout {
	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		la = filepath.Join(mod, "localcache")
	}
	return layout{
		game: game, data: filepath.Join(game, "data"), mod: mod,
		cache:       filepath.Join(la, "SacrificeHD", "cache"),
		backup:      filepath.Join(mod, "backup"),
		journal:     filepath.Join(mod, "swapped.json"),
		gameCacheHD: filepath.Join(mod, "gamecache_hd"),
	}
}

func readManifest(p string) manifest {
	var m manifest
	b, err := os.ReadFile(p)
	if err == nil {
		json.Unmarshal(b, &m)
	}
	if m.Sources == nil {
		m.Sources = map[string]srcInfo{}
	}
	return m
}

func writeJSON(p string, v any) error {
	b, _ := json.MarshalIndent(v, "", " ")
	return os.WriteFile(p, b, 0o644)
}

func statInfo(p string) (srcInfo, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return srcInfo{}, err
	}
	return srcInfo{Size: fi.Size(), MTime: fi.ModTime().Unix()}, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	os.MkdirAll(filepath.Dir(dst), 0o755)
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// moveGameCache moves the engine's own character cache files (cache/*.000) between the game's
// cache folder and dir, so HD and vanilla runs never reuse each other's prebuilt textures.
func moveGameCache(from, to string) {
	os.MkdirAll(to, 0o755)
	fs, _ := filepath.Glob(filepath.Join(from, "*.000"))
	for _, f := range fs {
		dst := filepath.Join(to, filepath.Base(f))
		os.Remove(dst)
		if err := os.Rename(f, dst); err != nil {
			copyFile(f, dst)
			os.Remove(f)
		}
	}
}

// restore puts every original wad back (used at exit and after a crash of the launcher itself).
// The backup folder is the source of truth: anything in it is an original that belongs in data/.
// The journal is only removed once the backup folder is empty, so a failed restore (files locked by a
// running game) is retried on the next launch.
func (l layout) restore() {
	var rels []string
	filepath.WalkDir(l.backup, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".wad") {
			if rel, err := filepath.Rel(l.backup, p); err == nil {
				rels = append(rels, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	_, jerr := os.Stat(l.journal)
	if len(rels) == 0 {
		if jerr == nil {
			os.Remove(l.journal)
		}
		l.useCache(wadStamp(l.data, "vanilla"))
		return
	}
	failed := 0
	for _, rel := range rels {
		orig := filepath.Join(l.backup, filepath.FromSlash(rel))
		dst := filepath.Join(l.data, filepath.FromSlash(rel))
		if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
			logf("restore %s: %v", rel, err)
			failed++
			continue
		}
		if err := os.Rename(orig, dst); err != nil {
			logf("restore %s: %v", rel, err)
			failed++
		}
	}
	if failed > 0 {
		logf("originals NOT fully restored (%d of %d failed) - is the game still running? will retry next launch", failed, len(rels))
		return
	}
	l.useCache(wadStamp(l.data, "vanilla"))
	os.Remove(l.journal)
	logf("originals restored (%d wads)", len(rels))
}

func (l layout) swapIn(rels []string) error {
	var done []string
	l.useCache(wadStamp(l.data, "vanilla")) // park the current set correctly before anything moves
	for _, rel := range rels {
		hd := filepath.Join(l.cache, filepath.FromSlash(rel))
		if _, err := os.Stat(hd); err != nil {
			continue
		}
		dst := filepath.Join(l.data, filepath.FromSlash(rel))
		bk := filepath.Join(l.backup, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(bk), 0o755)
		if _, err := os.Stat(bk); err == nil {
			return fmt.Errorf("backup of %s already exists; refusing to overwrite it", rel)
		}
		if err := os.Rename(dst, bk); err != nil {
			return fmt.Errorf("moving original %s aside: %w", rel, err)
		}
		done = append(done, rel)
		writeJSON(l.journal, done)
		if err := copyFile(hd, dst); err != nil {
			return fmt.Errorf("installing HD %s: %w", rel, err)
		}
		// Keep the original wad's timestamp: a newer wad makes the game rebuild its character texture
		// cache during the match, and creatures built that way (Netherfiend, Phoenix, Silverback) render
		// broken. The cache itself is matched to the HD wads by useCache.
		if fi, err := os.Stat(bk); err == nil {
			os.Chtimes(dst, fi.ModTime(), fi.ModTime())
		}
	}
	// the engine builds its character cache once and never checks it against the wads again, so a
	// cache built from other textures (older build, or vanilla) breaks models: only reuse an exact match
	if len(done) > 0 {
		l.useCache(wadStamp(l.cache, cacheVersion))
	}
	logf("HD wads in place: %s", strings.Join(done, ", "))
	return nil
}

// rulesHash ties a cached wad to the sheet rows that apply to it, so a sheet change rebuilds
// exactly the wads it affects.
func rulesHash(rel string) string {
	h := sha256.New()
	for _, a := range AssetClasses {
		hit := false
		for _, s := range append(append([]string{}, a.Sources...), a.Excludes...) {
			w, _, _ := strings.Cut(s, ":")
			if ok, _ := filepath.Match(strings.ToLower(w), strings.ToLower(rel)); ok {
				hit = true
			}
		}
		if hit {
			fmt.Fprintf(h, "%v|%v|", a, Grades[a.Grade])
		}
	}
	fmt.Fprintf(h, "%v", Upscalers)
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// wadStamp identifies the exact set of wads in dir (name, size, time) for matching engine caches.
func wadStamp(dir, tag string) string {
	var parts []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".wad") {
			if fi, err := d.Info(); err == nil {
				rel, _ := filepath.Rel(dir, p)
				parts = append(parts, fmt.Sprintf("%s:%d:%d", filepath.ToSlash(rel), fi.Size(), fi.ModTime().Unix()))
			}
		}
		return nil
	})
	sort.Strings(parts)
	return tag + "\n" + strings.Join(parts, "\n")
}

func readStamp(dir string) string {
	b, _ := os.ReadFile(filepath.Join(dir, "stamp.txt"))
	return string(b)
}

func writeStamp(dir, s string) {
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "stamp.txt"), []byte(s), 0o644)
}

// clearGameCache deletes the engine's character cache files (*.000) in dir.
func clearGameCache(dir string) {
	fs, _ := filepath.Glob(filepath.Join(dir, "*.000"))
	for _, f := range fs {
		os.Remove(f)
	}
	os.Remove(filepath.Join(dir, "stamp.txt"))
}

// useCache prepares the game's character texture cache (cache/*.000) for the wads about to be used.
// The engine stores these at whatever detail it picked when it built them (it lowers detail on the
// fly when the frame rate dips) and reuses them without checking, which left some creatures broken
// (Netherfiend black, Phoenix/Silverback wings detached) even with original textures. So no cache is
// ever carried between runs: the game rebuilds each creature's cache the first time it appears.
func (l layout) useCache(want string) {
	gc := filepath.Join(l.game, "cache")
	clearGameCache(gc)
	clearGameCache(l.gameCacheHD)
	clearGameCache(filepath.Join(l.mod, "gamecache_vanilla"))
	os.MkdirAll(gc, 0o755)
	os.WriteFile(filepath.Join(gc, "SacrificeHD.txt"), []byte(want), 0o644)
}
