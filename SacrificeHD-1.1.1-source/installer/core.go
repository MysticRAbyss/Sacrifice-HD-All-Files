package main

// Setup for Sacrifice HD, Sacrifice: Hydro and Sacrifice Hydro HD - the parts that do not need
// Windows, so they can be tested anywhere.
//
// The setup carries no files of its own: the download holds the setup next to a "files" folder,
// laid out as it goes into the Sacrifice folder. Installing copies that folder and writes a list of
// what it copied; the uninstaller (the same program, shipped in the mod's folder) removes exactly
// that list.

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed edition.json
var editionJSON []byte

type editionInfo struct {
	ID          string   `json:"id"`          // Apps & features key, e.g. "SacrificeHD"
	Title       string   `json:"title"`       // "Sacrifice HD" (also the shortcut name)
	Version     string   `json:"version"`     // "1.1.1"
	ModFolder   string   `json:"modFolder"`   // folder inside the Sacrifice folder
	Launcher    string   `json:"launcher"`    // program the shortcuts start, relative to the Sacrifice folder
	Uninstaller string   `json:"uninstaller"` // relative to the Sacrifice folder
	NeedsExe    bool     `json:"needsSacrificeExe"`
	RootFiles   []string `json:"rootFiles"`   // files that go next to Sacrifice.exe (others' copies are set aside)
	BackupDir   string   `json:"backupDir"`   // where those are set aside, relative to the Sacrifice folder
	Restore     []string `json:"restore"`     // launcher arguments that put the original game files back ("{game}")
	AlsoRemove  []string `json:"alsoRemove"`  // made by the mod while it runs, removed on uninstall
	HDCache     bool     `json:"hdCache"`     // remove %LOCALAPPDATA%\SacrificeHD on uninstall (unless another HD mod still uses it)
	AdminLink   bool     `json:"adminLink"`   // under Program Files the shortcut runs as administrator
	UserFiles   []string `json:"userFiles"`   // the player's settings in the mod folder: kept on update
	Notes       []string `json:"notes"`       // shown after installing
	SizeKB      int      `json:"sizeKB"`
}

var edition = func() editionInfo {
	var e editionInfo
	if err := json.Unmarshal(editionJSON, &e); err != nil {
		panic(err)
	}
	return e
}()

const manifestName = "install-manifest.txt"

func manifestPath(game string) string { return filepath.Join(game, edition.ModFolder, manifestName) }

// originalGameFiles are files of the original Sacrifice that the mods cannot run without
var originalGameFiles = []string{
	"data/menus.wad", "data/main.wad", "data/models.wad", "data/spells.wad", "data/sounds.wad",
	"data/saxs.wad", "data/interfac.wad",
}

// missingGameFiles lists what is missing for folder to be a Sacrifice folder (nothing: it is one)
func missingGameFiles(folder string) []string {
	if folder == "" {
		return []string{"(no folder)"}
	}
	var missing []string
	if edition.NeedsExe {
		if st, err := os.Stat(filepath.Join(folder, "Sacrifice.exe")); err != nil || st.IsDir() {
			missing = append(missing, "Sacrifice.exe")
		}
	}
	for _, f := range originalGameFiles {
		if st, err := os.Stat(filepath.Join(folder, filepath.FromSlash(f))); err != nil || st.Size() == 0 {
			missing = append(missing, f)
		}
	}
	if st, err := os.Stat(filepath.Join(folder, "maps")); err != nil || !st.IsDir() {
		missing = append(missing, "maps")
	}
	return missing
}

// normalizeGameFolder accepts the Sacrifice folder or a folder inside it and returns the Sacrifice folder
func normalizeGameFolder(p string) string {
	p = strings.Trim(strings.TrimSpace(p), `"`)
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	for i := 0; i < 3 && p != "." && p != filepath.Dir(p); i++ {
		if len(missingGameFiles(p)) == 0 {
			return p
		}
		p = filepath.Dir(p)
	}
	return ""
}

func findGameFolder(candidates []string) string {
	seen := map[string]bool{}
	for _, c := range candidates {
		if c == "" || seen[strings.ToLower(c)] {
			continue
		}
		seen[strings.ToLower(c)] = true
		if f := normalizeGameFolder(c); f != "" {
			return f
		}
	}
	return ""
}

func commonGameFolders(drives []string) []string {
	var l []string
	for _, d := range drives {
		for _, p := range []string{
			`GOG Games\Sacrifice`, `Program Files (x86)\GOG Galaxy\Games\Sacrifice`, `Program Files (x86)\GOG.com\Sacrifice`,
			`Program Files (x86)\Steam\steamapps\common\Sacrifice`, `Program Files\Steam\steamapps\common\Sacrifice`,
			`SteamLibrary\steamapps\common\Sacrifice`, `Steam\steamapps\common\Sacrifice`,
			`Games\Sacrifice`, `Games\GOG\Sacrifice`, `Sacrifice`,
			`Program Files (x86)\Shiny\Sacrifice`, `Program Files\Shiny\Sacrifice`, `Program Files (x86)\Interplay\Sacrifice`,
		} {
			l = append(l, d+`:\`+p)
		}
	}
	return l
}

// steamLibraryFolders reads Steam's libraryfolders.vdf and returns where Sacrifice would be
func steamLibraryFolders(steamPath string) []string {
	var l []string
	if steamPath == "" {
		return l
	}
	l = append(l, filepath.Join(steamPath, "steamapps", "common", "Sacrifice"))
	f, err := os.Open(filepath.Join(steamPath, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return l
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), `"`)
		var vals []string
		for i := 1; i < len(fields); i += 2 {
			vals = append(vals, fields[i])
		}
		if len(vals) == 2 && (vals[0] == "path" || isDigits(vals[0])) && strings.Contains(vals[1], `:`) {
			lib := strings.ReplaceAll(vals[1], `\\`, `\`)
			l = append(l, filepath.Join(lib, "steamapps", "common", "Sacrifice"))
		}
	}
	return l
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// packageFiles lists the files of the download's "files" folder (relative, with / separators)
func packageFiles(filesDir string) ([]string, error) {
	var l []string
	err := filepath.Walk(filesDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(filesDir, p)
		if err != nil {
			return err
		}
		l = append(l, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(l)
	return l, err
}

func isUserFile(rel string) bool {
	for _, u := range edition.UserFiles {
		if strings.EqualFold(rel, edition.ModFolder+"/"+u) {
			return true
		}
	}
	return false
}

func isRootFile(rel string) bool {
	for _, r := range edition.RootFiles {
		if strings.EqualFold(rel, r) {
			return true
		}
	}
	return false
}

func sameFile(a, b string) bool {
	fa, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	fb, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return string(fa) == string(fb)
}

// installFiles copies the package into game. The player's settings are kept when they already
// exist; another mod's copies of the root files (graphics/input DLLs) are set aside first. It returns
// the installed files (relative, / separators) for the uninstaller.
func installFiles(filesDir, game string, progress func(done, total int)) ([]string, error) {
	files, err := packageFiles(filesDir)
	if err != nil {
		return nil, fmt.Errorf("the download is incomplete (%v); unzip all of it and run the setup again", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("the \"files\" folder next to the setup is empty; unzip all of the download and run the setup again")
	}
	var installed []string
	for i, rel := range files {
		if progress != nil {
			progress(i, len(files))
		}
		src := filepath.Join(filesDir, filepath.FromSlash(rel))
		dst := filepath.Join(game, filepath.FromSlash(rel))
		if isUserFile(rel) {
			if _, err := os.Stat(dst); err == nil {
				continue // keep the player's settings
			}
		}
		if isRootFile(rel) && edition.BackupDir != "" {
			if _, err := os.Stat(dst); err == nil && !sameFile(src, dst) {
				bk := filepath.Join(game, filepath.FromSlash(edition.BackupDir), filepath.Base(rel))
				if _, err := os.Stat(bk); err != nil {
					os.MkdirAll(filepath.Dir(bk), 0755)
					if err := os.Rename(dst, bk); err != nil {
						return installed, fmt.Errorf("could not set aside %s: %v (is the game running?)", rel, err)
					}
				}
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return installed, err
		}
		if err := copyFile(src, dst); err != nil {
			return installed, fmt.Errorf("%s: %v (is the game running?)", rel, err)
		}
		if !isUserFile(rel) {
			installed = append(installed, rel)
		}
	}
	if progress != nil {
		progress(len(files), len(files))
	}
	return installed, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func writeManifest(game string, files []string) error {
	files = append(files, edition.ModFolder+"/"+manifestName)
	sort.Strings(files)
	return os.WriteFile(manifestPath(game), []byte(strings.Join(files, "\r\n")+"\r\n"), 0644)
}

// removeFiles deletes what was installed (but not the running uninstaller, self), puts set-aside
// root files back, and with everything also the player's settings and saved games. It returns the
// files that could not be deleted.
func removeFiles(game, self string, everything bool) []string {
	var failed []string
	b, _ := os.ReadFile(manifestPath(game))
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.Contains(l, "..") || filepath.IsAbs(l) {
			continue
		}
		p := filepath.Join(game, filepath.FromSlash(l))
		if self != "" && strings.EqualFold(filepath.Clean(p), filepath.Clean(self)) {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			failed = append(failed, l)
		}
	}
	if edition.BackupDir != "" {
		for _, r := range edition.RootFiles {
			bk := filepath.Join(game, filepath.FromSlash(edition.BackupDir), r)
			if _, err := os.Stat(bk); err == nil {
				os.Rename(bk, filepath.Join(game, r))
			}
		}
	}
	for _, a := range edition.AlsoRemove {
		os.Remove(filepath.Join(game, filepath.FromSlash(a)))
	}
	mod := filepath.Join(game, edition.ModFolder)
	if everything {
		for _, u := range edition.UserFiles {
			os.Remove(filepath.Join(mod, u))
		}
		os.RemoveAll(filepath.Join(mod, "saves"))
	}
	removeEmptyDirs(mod)
	return failed
}

// removeEmptyDirs removes dir and the empty folders below it (those that are not empty stay)
func removeEmptyDirs(dir string) {
	var dirs []string
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		os.Remove(d)
	}
}

func restoreArgs(game string) []string {
	var a []string
	for _, s := range edition.Restore {
		a = append(a, strings.ReplaceAll(s, "{game}", game))
	}
	return a
}
