package main

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeGame(t *testing.T) string {
	g := t.TempDir()
	os.MkdirAll(filepath.Join(g, "data"), 0755)
	os.MkdirAll(filepath.Join(g, "maps"), 0755)
	for _, f := range originalGameFiles {
		os.WriteFile(filepath.Join(g, filepath.FromSlash(f)), []byte("x"), 0644)
	}
	os.WriteFile(filepath.Join(g, "Sacrifice.exe"), []byte("exe"), 0644)
	return g
}

func write(t *testing.T, p, s string) {
	os.MkdirAll(filepath.Dir(p), 0755)
	if err := os.WriteFile(p, []byte(s), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallUpdateUninstall(t *testing.T) {
	edition.ModFolder = "Mod"
	edition.UserFiles = []string{"settings.txt"}
	edition.RootFiles = []string{"DDraw.dll"}
	edition.BackupDir = "Mod/original_dlls"
	edition.AlsoRemove = []string{"dgVoodoo.conf"}
	g := fakeGame(t)
	if normalizeGameFolder(filepath.Join(g, "Mod")) != g || normalizeGameFolder(t.TempDir()) != "" {
		t.Fatal("normalize")
	}
	pkg := t.TempDir()
	write(t, filepath.Join(pkg, "Mod", "Game.exe"), "v1")
	write(t, filepath.Join(pkg, "Mod", "settings.txt"), "default")
	write(t, filepath.Join(pkg, "Mod", "Uninstall.exe"), "u")
	write(t, filepath.Join(pkg, "Mod", "sub", "a.json"), "a")
	write(t, filepath.Join(pkg, "DDraw.dll"), "ours")
	write(t, filepath.Join(g, "DDraw.dll"), "someone else's")
	files, err := installFiles(pkg, g, nil)
	if err != nil || len(files) != 4 {
		t.Fatal(err, files)
	}
	writeManifest(g, files)
	if b, _ := os.ReadFile(filepath.Join(g, "Mod", "original_dlls", "DDraw.dll")); string(b) != "someone else's" {
		t.Fatal("not set aside")
	}
	write(t, filepath.Join(g, "Mod", "settings.txt"), "mine")
	write(t, filepath.Join(g, "Mod", "saves", "s.rcp"), "s")
	write(t, filepath.Join(g, "dgVoodoo.conf"), "made at run time")
	write(t, filepath.Join(pkg, "Mod", "Game.exe"), "v2")
	if _, err := installFiles(pkg, g, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(g, "Mod", "settings.txt")); string(b) != "mine" {
		t.Fatal("settings overwritten")
	}
	if b, _ := os.ReadFile(filepath.Join(g, "Mod", "original_dlls", "DDraw.dll")); string(b) != "someone else's" {
		t.Fatal("backup overwritten by our own copy")
	}
	self := filepath.Join(g, "Mod", "Uninstall.exe")
	if f := removeFiles(g, self, false); len(f) != 0 {
		t.Fatal(f)
	}
	if b, _ := os.ReadFile(filepath.Join(g, "DDraw.dll")); string(b) != "someone else's" {
		t.Fatal("original DLL not put back")
	}
	for _, p := range []string{"Mod/Game.exe", "Mod/sub", "dgVoodoo.conf", "Mod/original_dlls"} {
		if _, err := os.Stat(filepath.Join(g, p)); err == nil {
			t.Fatal("left:", p)
		}
	}
	for _, p := range []string{"Mod/settings.txt", "Mod/saves/s.rcp", "Mod/Uninstall.exe", "Sacrifice.exe", "data/menus.wad"} {
		if _, err := os.Stat(filepath.Join(g, p)); err != nil {
			t.Fatal("removed:", p)
		}
	}
	removeFiles(g, self, true)
	if _, err := os.Stat(filepath.Join(g, "Mod", "saves")); err == nil {
		t.Fatal("saves kept")
	}
}

func TestMissingFiles(t *testing.T) {
	edition.NeedsExe = true
	g := fakeGame(t)
	os.Remove(filepath.Join(g, "Sacrifice.exe"))
	if m := missingGameFiles(g); len(m) != 1 || m[0] != "Sacrifice.exe" {
		t.Fatal(m)
	}
	edition.NeedsExe = false
	if len(missingGameFiles(g)) != 0 {
		t.Fatal("hydro does not need the exe")
	}
}
