package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var logw io.Writer = os.Stdout
var logMu sync.Mutex

func logf(f string, a ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	fmt.Fprintf(logw, time.Now().Format("15:04:05 ")+f+"\n", a...)
}

func say(msg string) { // shown to the player in the console during generation
	logf("%s", msg)
}

func compatLayer() string {
	for _, r := range RenderRows {
		if r.Target == "process_env" && r.Key == "__COMPAT_LAYER" {
			return r.Value
		}
	}
	return "RunAsInvoker"
}

func main() {
	gameFlag := flag.String("game", "", "Sacrifice install folder (Melty passes {game})")
	regen := flag.Bool("regen", false, "rebuild the HD textures even if the cache is current")
	genOnly := flag.Bool("generate-only", false, "build the HD textures and exit")
	restoreOnly := flag.Bool("restore", false, "put the original game files back and exit (used by the uninstaller)")
	flag.Parse()
	exe, _ := os.Executable()
	modDir := filepath.Dir(exe)
	game := *gameFlag
	if game == "" {
		game = os.Getenv("SACRIFICE_DIR")
	}
	if game == "" {
		game = filepath.Dir(modDir)
	}
	var logFile *os.File
	if lf, err := os.Create(filepath.Join(modDir, "launcher.log")); err == nil {
		logFile = lf
		logw = io.MultiWriter(lf, os.Stdout) // file first: it must survive the console going away
		defer lf.Close()
	}
	logf("Sacrifice HD %s  game=%s", cacheVersion, game)
	if _, err := os.Stat(filepath.Join(game, "Sacrifice.exe")); err != nil {
		logf("Sacrifice.exe not found in %s", game)
		time.Sleep(8 * time.Second)
		os.Exit(2)
	}
	if !firstInstance() {
		logf("Sacrifice HD is already running - not starting a second copy")
		os.Exit(0)
	}
	l := newLayout(game, modDir)
	l.restore() // in case a previous run did not finish
	if *restoreOnly {
		if left, _ := filepath.Glob(filepath.Join(l.backup, "*.wad")); len(left) > 0 {
			os.Exit(3)
		}
		return
	}
	if left, _ := filepath.Glob(filepath.Join(l.backup, "*.wad")); len(left) > 0 {
		logf("original game files are still in %s and could not be put back; close Sacrifice and start again", l.backup)
		time.Sleep(8 * time.Second)
		os.Exit(3)
	}
	// one-time clean-up: earlier builds could leave character caches built from mismatched wads
	if marker := filepath.Join(l.mod, "gamecache.v2"); func() bool { _, err := os.Stat(marker); return err != nil }() {
		clearGameCache(filepath.Join(game, "cache"))
		clearGameCache(l.gameCacheHD)
		clearGameCache(filepath.Join(l.mod, "gamecache_vanilla"))
		os.WriteFile(marker, []byte("character caches cleared once\n"), 0o644)
		logf("cleared old character caches (the game rebuilds them)")
	}
	os.MkdirAll(l.cache, 0o755)

	if err := ensureHD(l, *regen); err != nil {
		logf("HD build failed: %v (starting the original game)", err)
	}
	if *genOnly {
		return
	}
	writeDgVoodooConf(l)
	applyGameOptions()
	if db, _ := os.ReadFile(filepath.Join(modDir, "dev.txt")); strings.Contains(string(db), "SACHD_DEV") {
		reg("add", regVStor+`\Options`, "/v", "Demo mode", "/t", "REG_DWORD", "/d", "0", "/f") // testing: no attract demo
	}
	rels := filterSwap(l.mod, sourceWads(l.data))
	if err := l.swapIn(rels); err != nil {
		logf("swap failed: %v", err)
		l.restore()
	}
	defer l.restore()

	freeConsole()
	if logFile != nil {
		logMu.Lock()
		logw = logFile
		logMu.Unlock()
	}
	devb, _ := os.ReadFile(filepath.Join(modDir, "dev.txt"))
	if !strings.Contains(string(devb), "SACHD_DEV") {
		runGame(game, compatLayer(), flag.Args())
		return
	}
	devLoop(l, game, flag.Args())
}

var curMu sync.Mutex
var cur *exec.Cmd

func runGame(game, layer string, args []string) {
	cmd := exec.Command(filepath.Join(game, "Sacrifice.exe"), args...)
	cmd.Dir = game
	cmd.Env = append(os.Environ(), "__COMPAT_LAYER="+layer)
	t := time.Now()
	logf("starting Sacrifice (__COMPAT_LAYER=%q)", layer)
	if err := cmd.Start(); err != nil {
		logf("start failed: %v", err)
		return
	}
	curMu.Lock()
	cur = cmd
	curMu.Unlock()
	err := cmd.Wait()
	curMu.Lock()
	cur = nil
	curMu.Unlock()
	logf("Sacrifice exited after %s: %v", time.Since(t).Round(time.Second), err)
}

func ensureHD(l layout, force bool) error {
	mp := filepath.Join(l.cache, "manifest.json")
	m := readManifest(mp)
	if m.Version != cacheVersion {
		m = manifest{Version: cacheVersion, Sources: map[string]srcInfo{}}
	}
	rels := sourceWads(l.data)
	var stale []string
	for _, rel := range rels {
		si, err := statInfo(filepath.Join(l.data, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		si.Rules = rulesHash(rel)
		_, herr := os.Stat(filepath.Join(l.cache, filepath.FromSlash(rel)))
		if force || herr != nil || m.Sources[rel] != si {
			stale = append(stale, rel)
		}
	}
	if len(stale) == 0 {
		logf("HD textures up to date (%d wads)", len(rels))
		return nil
	}
	say("Sacrifice HD: building high-resolution textures from your copy of Sacrifice.")
	say("This happens once and takes a few minutes; the game starts by itself afterwards.")
	work := filepath.Join(l.cache, "work")
	start := time.Now()
	for i, rel := range stale {
		t0 := time.Now()
		say(fmt.Sprintf("[%d/%d] %s", i+1, len(stale), rel))
		p, err := planWad(l.data, rel)
		if err != nil {
			return err
		}
		js := p.jobs()
		be := "none"
		if len(js) > 0 {
			be = runUpscale(l.mod, work, js, func(s string) { logf("   %s", s) })
		}
		if err := p.finish(filepath.Join(l.cache, filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		si, _ := statInfo(filepath.Join(l.data, filepath.FromSlash(rel)))
		si.Rules = rulesHash(rel)
		m.Sources[rel] = si
		m.Backend = be
		writeJSON(mp, m)
		logf("   done in %s (%d images, %s)", time.Since(t0).Round(time.Second), len(js), be)
	}
	os.RemoveAll(work)
	say(fmt.Sprintf("HD textures ready in %s.", time.Since(start).Round(time.Second)))
	return nil
}

func writeDgVoodooConf(l layout) {
	tpl := filepath.Join(l.mod, "dgVoodoo.template.conf")
	b, err := os.ReadFile(tpl)
	if err != nil {
		logf("no dgVoodoo template: %v", err)
		return
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	sec := ""
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			sec = t[1 : len(t)-1]
			continue
		}
		k, _, ok := strings.Cut(t, "=")
		if !ok || strings.HasPrefix(t, ";") {
			continue
		}
		k = strings.TrimSpace(k)
		for _, r := range RenderRows {
			if r.Target == "dgVoodoo.conf" && r.Section == sec && r.Key == k {
				v := r.Value
				if v == "{gamemode}" {
					gw, gh := gameMode()
					v = fmt.Sprintf("%dx%d", gw, gh)
				}
				lines[i] = fmt.Sprintf("%-37s= %s", k, v)
			}
		}
	}
	devb, _ := os.ReadFile(filepath.Join(l.mod, "dev.txt"))
	if os.Getenv("SACRIFICEHD_WINDOWED") != "" || strings.Contains(string(devb), "SACHD_DEV") && strings.Contains(string(devb), "windowed") {
		for i, ln := range lines {
			if strings.HasPrefix(strings.TrimSpace(ln), "FullScreenMode") {
				lines[i] = fmt.Sprintf("%-37s= %s", "FullScreenMode", "false")
			}
		}
	}
	if err := os.WriteFile(filepath.Join(l.game, "dgVoodoo.conf"), []byte(strings.Join(lines, "\r\n")), 0o644); err != nil {
		logf("writing dgVoodoo.conf: %v", err)
	}
}

// devLoop is the testing mode (only when dev.txt sits next to the launcher): it obeys go.txt.
// go.txt: "kill" | "quit" | "run <exe> [args]" | "<compat layer>" (relaunch the game)
func devLoop(l layout, game string, args []string) {
	trig := filepath.Join(l.mod, "go.txt")
	var last time.Time
	if st, err := os.Stat(trig); err == nil {
		last = st.ModTime()
	}
	go runGame(game, compatLayer(), args)
	logf("dev mode: watching %s", trig)
	killGame := func() {
		curMu.Lock()
		if cur != nil && cur.Process != nil {
			cur.Process.Kill()
		}
		curMu.Unlock()
		c := exec.Command("taskkill", "/IM", "Sacrifice.exe", "/F")
		hideWindow(c)
		c.Run()
		time.Sleep(time.Second)
	}
	for {
		time.Sleep(2 * time.Second)
		st, err := os.Stat(trig)
		if err != nil || !st.ModTime().After(last) {
			continue
		}
		last = st.ModTime()
		b, _ := os.ReadFile(trig)
		line := strings.TrimSpace(string(b))
		logf("dev command %q", line)
		switch {
		case line == "quit":
			killGame()
			return
		case line == "kill":
			killGame()
		case strings.HasPrefix(line, "shots "):
			var n, every int
			fmt.Sscanf(line, "shots %d %d", &n, &every)
			go func() {
				for i := 0; i < n; i++ {
					time.Sleep(time.Duration(every) * time.Second)
					p, err := captureGame(filepath.Join(l.mod, "shots"))
					logf("shot %s %v", p, err)
				}
			}()
		case strings.HasPrefix(line, "input "):
			go runInput(line[6:])
		case line == "shot":
			p, err := captureGame(filepath.Join(l.mod, "shots"))
			logf("shot %s %v", p, err)
		case strings.HasPrefix(line, "run "):
			killGame()
			l.restore()
			f := strings.Fields(line[4:])
			p := f[0]
			if !filepath.IsAbs(p) {
				p = filepath.Join(l.mod, p)
			}
			c := exec.Command(p, f[1:]...)
			c.Dir = l.mod
			if err := c.Start(); err != nil {
				logf("dev run failed: %v", err)
				continue
			}
			logf("dev run started pid %d; handing over", c.Process.Pid)
			os.Exit(0)
		default:
			killGame()
			if line == "" {
				line = compatLayer()
			}
			go runGame(game, line, args)
		}
	}
}

// filterSwap (testing aid): swap.txt next to the launcher may list wads to leave out ("-main.wad")
// or the only wads to use ("saxs_wiz.wad").
func filterSwap(mod string, rels []string) []string {
	b, err := os.ReadFile(filepath.Join(mod, "swap.txt"))
	if err != nil {
		return rels
	}
	only, skip := map[string]bool{}, map[string]bool{}
	for _, f := range strings.Fields(string(b)) {
		if strings.HasPrefix(f, "-") {
			skip[strings.ToLower(f[1:])] = true
		} else {
			only[strings.ToLower(f)] = true
		}
	}
	var out []string
	for _, r := range rels {
		lr := strings.ToLower(r)
		if skip[lr] || (len(only) > 0 && !only[lr]) {
			continue
		}
		out = append(out, r)
	}
	logf("swap.txt applied: %d of %d wads", len(out), len(rels))
	return out
}
