// Builds the two Sacrifice HD 1.1.1 downloads from this source folder and the three official
// third-party downloads (see BUILD.txt):
//
//	go run . -thirdparty <folder with the three zips> -out <output folder>
//
// It checks every third-party file against the checksums below, builds SacrificeHD.exe and the
// installer, puts everything in place and prints the checksum of every file it made, so a build
// can be compared with the published one.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const version = "1.1.1"

// the third-party files the mod ships, by the name they have inside their official zip
type thirdParty struct {
	inZip  string // path inside the official zip
	dest   string // where it goes in the mod (relative to the Sacrifice folder)
	sha256 string
}

var thirdPartyFiles = []thirdParty{
	// dgVoodoo2 2.87.5 by Dege (dgVoodoo2_87_5.zip, http://dege.fw.hu/dgVoodoo2/)
	{"MS/x86/DDraw.dll", "DDraw.dll", "612a24408a090a3c6f3886557fa18034ee742e94ad0a40ebdf854d2816176c2e"},
	{"MS/x86/D3DImm.dll", "D3DImm.dll", "93c534f2d17419ea78f15551f7e0aac78b3c503733a840914fa063708a5afe8e"},
	{"dgVoodoo.conf", "SacrificeHD/dgVoodoo.template.conf", ""}, // checked against third-party/dgVoodoo.template.conf
	// dinputto8 by Elisha Riedlinger (dinputto8.zip from https://github.com/elishacloud/dinputto8/releases)
	{"dinput.dll", "dinput.dll", "82b3f6ee8b99c727a37c345984da6cfbed9ea730c12efc99e93983d47a63c4d8"},
	// Real-ESRGAN v0.2.5.0 (realesrgan-ncnn-vulkan-20220424-windows.zip, https://github.com/xinntao/Real-ESRGAN/releases)
	{"realesrgan-ncnn-vulkan.exe", "SacrificeHD/esrgan/realesrgan-ncnn-vulkan.exe", "07e49f7cbb4ede01ae4dd4c399d3a7e5846e3d2085c3128eff881e55cb7b1a0c"},
	{"vcomp140.dll", "SacrificeHD/esrgan/vcomp140.dll", ""},
	{"models/realesr-animevideov3-x2.bin", "SacrificeHD/esrgan/models/realesr-animevideov3-x2.bin", ""},
	{"models/realesr-animevideov3-x2.param", "SacrificeHD/esrgan/models/realesr-animevideov3-x2.param", ""},
	{"models/realesr-animevideov3-x4.bin", "SacrificeHD/esrgan/models/realesr-animevideov3-x4.bin", ""},
	{"models/realesr-animevideov3-x4.param", "SacrificeHD/esrgan/models/realesr-animevideov3-x4.param", ""},
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func fileSum(p string) string {
	b, err := os.ReadFile(p)
	must(err)
	return sum(b)
}

// findInZips returns the contents of name from the first zip in dir that has it
func findInZips(dir, name string) ([]byte, string) {
	zips, _ := filepath.Glob(filepath.Join(dir, "*.zip"))
	for _, z := range zips {
		r, err := zip.OpenReader(z)
		if err != nil {
			continue
		}
		for _, f := range r.File {
			if f.Name == name {
				rc, err := f.Open()
				must(err)
				b, err := io.ReadAll(rc)
				rc.Close()
				r.Close()
				must(err)
				return b, filepath.Base(z)
			}
		}
		r.Close()
	}
	return nil, ""
}

func write(p string, b []byte) {
	must(os.MkdirAll(filepath.Dir(p), 0755))
	must(os.WriteFile(p, b, 0644))
}

func copyFile(src, dst string) {
	b, err := os.ReadFile(src)
	must(err)
	write(dst, b)
}

func copyDir(src, dst string) {
	must(filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		copyFile(p, filepath.Join(dst, rel))
		return nil
	}))
}

func run(dir string, env []string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	fmt.Println(">", name, strings.Join(args, " "), "  (in", dir+")")
	must(cmd.Run())
}

func zipFolder(srcParent, folder, out string) {
	f, err := os.Create(out)
	must(err)
	w := zip.NewWriter(f)
	var files []string
	filepath.Walk(filepath.Join(srcParent, folder), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files = append(files, p)
		}
		return err
	})
	sort.Strings(files)
	for _, p := range files {
		rel, _ := filepath.Rel(srcParent, p)
		zw, err := w.Create(filepath.ToSlash(rel))
		must(err)
		b, err := os.ReadFile(p)
		must(err)
		_, err = zw.Write(b)
		must(err)
	}
	must(w.Close())
	must(f.Close())
}

func main() {
	tp := flag.String("thirdparty", "", "folder with the three official third-party zips")
	out := flag.String("out", "out", "output folder")
	flag.Parse()
	if *tp == "" {
		fmt.Println("usage: go run . -thirdparty <folder with the three zips> -out <output folder>")
		os.Exit(2)
	}
	here, _ := os.Getwd()
	root := filepath.Dir(here) // the source folder (this program is in its "release" folder)
	outDir, _ := filepath.Abs(*out)
	work := filepath.Join(outDir, "work")
	os.RemoveAll(work)
	files := filepath.Join(work, "files") // the mod as it goes into the Sacrifice folder

	fmt.Println("1. Third-party files")
	tpl, err := os.ReadFile(filepath.Join(root, "third-party", "dgVoodoo.template.conf"))
	must(err)
	for _, t := range thirdPartyFiles {
		b, from := findInZips(*tp, t.inZip)
		if b == nil {
			must(fmt.Errorf("%s was not found in the zips in %s (see BUILD.txt for the downloads)", t.inZip, *tp))
		}
		want := t.sha256
		if t.inZip == "dgVoodoo.conf" {
			want = sum(tpl)
		}
		got := sum(b)
		status := "ok"
		if want != "" && got != want {
			status = "DIFFERENT from the published 1.1.1 (another version of the download?)"
		}
		fmt.Printf("   %-40s from %-45s %s\n", t.inZip, from, status)
		write(filepath.Join(files, filepath.FromSlash(t.dest)), b)
	}

	fmt.Println("2. SacrificeHD.exe")
	exe := filepath.Join(files, "SacrificeHD", "SacrificeHD.exe")
	win := []string{"GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0"}
	run(filepath.Join(root, "launcher"), win, "go", "build", "-trimpath", "-ldflags", "-s -w", "-o", exe, ".")

	fmt.Println("3. Readme and licences")
	copyFile(filepath.Join(root, "README-mod.txt"), filepath.Join(files, "SacrificeHD", "README.txt"))
	copyDir(filepath.Join(root, "licenses"), filepath.Join(files, "SacrificeHD", "licenses"))

	fmt.Println("4. Installer")
	inst := filepath.Join(root, "installer")
	host := "verinfo"
	if runtime.GOOS == "windows" {
		host += ".exe"
	}
	run(filepath.Join(inst, "verinfo"), nil, "go", "build", "-o", filepath.Join(work, host), ".")
	py := "python3"
	if _, err := exec.LookPath(py); err != nil {
		py = "python"
	}
	run(inst, nil, py, "mkver.py", "Sacrifice HD", version, "Sacrifice HD setup", "Install Sacrifice HD.exe",
		filepath.Join(work, "vi.json"), "hydro.ico", "setup.manifest")
	run(inst, nil, filepath.Join(work, host), filepath.Join(work, "vi.json"), "rsrc_windows_amd64.syso")
	copyFile(filepath.Join(inst, "editions", "sachd.json"), filepath.Join(inst, "edition.json"))
	setupDir := filepath.Join(work, "Sacrifice HD "+version+" Setup")
	installer := filepath.Join(setupDir, "Install Sacrifice HD.exe")
	run(inst, win, "go", "build", "-trimpath", "-ldflags", "-H windowsgui", "-o", installer, ".")
	copyFile(installer, filepath.Join(files, "SacrificeHD", "Uninstall Sacrifice HD.exe"))

	fmt.Println("5. Downloads")
	copyDir(files, filepath.Join(setupDir, "files"))
	copyFile(filepath.Join(root, "README-mod.txt"), filepath.Join(setupDir, "README.txt"))
	manual := filepath.Join(work, "SacrificeHD-"+version+"-Manual")
	copyDir(files, manual)
	os.Remove(filepath.Join(manual, "SacrificeHD", "Uninstall Sacrifice HD.exe"))
	zipFolder(work, filepath.Base(setupDir), filepath.Join(outDir, "SacrificeHD-"+version+"-Setup.zip"))
	zipFolder(work, filepath.Base(manual), filepath.Join(outDir, "SacrificeHD-"+version+"-Manual.zip"))

	fmt.Println("6. Checksums (compare with CHECKSUMS.txt)")
	for _, p := range []string{exe, installer} {
		fmt.Printf("   %s  %s\n", fileSum(p), filepath.Base(p))
	}
	fmt.Println("done:", outDir)
}
