//go:build windows

package main

// Windows parts: finding Sacrifice (GOG / Steam / registry), shortcuts, the Apps & features entry,
// administrator rights and removing the uninstaller after it has run.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func regString(root registry.Key, path, name string) string {
	for _, view := range []uint32{registry.WOW64_32KEY, registry.WOW64_64KEY} {
		k, err := registry.OpenKey(root, path, registry.QUERY_VALUE|view)
		if err != nil {
			continue
		}
		s, _, err := k.GetStringValue(name)
		k.Close()
		if err == nil && s != "" {
			return s
		}
	}
	return ""
}

func subKeys(root registry.Key, path string) []string {
	var r []string
	for _, view := range []uint32{registry.WOW64_32KEY, registry.WOW64_64KEY} {
		k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS|view)
		if err != nil {
			continue
		}
		names, _ := k.ReadSubKeyNames(-1)
		k.Close()
		r = append(r, names...)
	}
	return r
}

// registryGameFolders: the GOG entry of Sacrifice, the uninstall entries and Steam's libraries
func registryGameFolders() []string {
	var l []string
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		l = append(l, regString(root, `SOFTWARE\GOG.com\Games\1207658651`, "path"))
		for _, id := range subKeys(root, `SOFTWARE\GOG.com\Games`) {
			p := `SOFTWARE\GOG.com\Games\` + id
			if strings.Contains(strings.ToLower(regString(root, p, "gameName")), "sacrifice") {
				l = append(l, regString(root, p, "path"))
			}
		}
		un := `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
		for _, id := range subKeys(root, un) {
			name := strings.ToLower(regString(root, un+`\`+id, "DisplayName"))
			if strings.Contains(name, "sacrifice") && !strings.Contains(name, "hydro") && !strings.Contains(name, " hd") {
				l = append(l, regString(root, un+`\`+id, "InstallLocation"))
			}
		}
	}
	l = append(l, steamLibraryFolders(regString(registry.CURRENT_USER, `Software\Valve\Steam`, "SteamPath"))...)
	l = append(l, steamLibraryFolders(regString(registry.LOCAL_MACHINE, `SOFTWARE\Valve\Steam`, "InstallPath"))...)
	return l
}

func fixedDrives() []string {
	var d []string
	for c := 'C'; c <= 'Z'; c++ {
		if _, err := os.Stat(string(c) + `:\`); err == nil {
			d = append(d, string(c))
		}
	}
	return d
}

// guessGameFolder: where the setup is (unzipped into the Sacrifice folder), the registry, the usual folders
func guessGameFolder(exeDir string) string {
	c := []string{exeDir, filepath.Dir(exeDir)}
	c = append(c, registryGameFolders()...)
	c = append(c, commonGameFolders(fixedDrives())...)
	return findGameFolder(c)
}

func hasVCRuntime() bool {
	sys := os.Getenv("SystemRoot")
	if sys == "" {
		sys = `C:\Windows`
	}
	_, err := os.Stat(filepath.Join(sys, "System32", "vcruntime140.dll"))
	return err == nil
}

const vcRedistURL = "https://aka.ms/vs/17/release/vc_redist.x64.exe"

func openURL(u string) {
	win.ShellExecute(0, syscall.StringToUTF16Ptr("open"), syscall.StringToUTF16Ptr(u), nil, nil, win.SW_SHOWNORMAL)
}

// runElevated starts this program again with administrator rights (Windows asks the player)
func runElevated(args string) bool {
	exe, _ := os.Executable()
	return win.ShellExecute(0, syscall.StringToUTF16Ptr("runas"), syscall.StringToUTF16Ptr(exe), syscall.StringToUTF16Ptr(args), nil, win.SW_SHOWNORMAL)
}

func canWrite(folder string) bool {
	f, err := os.CreateTemp(folder, ".setup-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// underProgramFiles: normal users cannot write there, so the game needs administrator rights
func underProgramFiles(dir string) bool {
	l := strings.ToLower(dir)
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"} {
		if v := strings.ToLower(os.Getenv(env)); v != "" && strings.HasPrefix(l, v) {
			return true
		}
	}
	return false
}

func desktopFolder() string {
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0); err == nil {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Desktop")
}

func startMenuFolder() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, edition.Title)
}

func shortcutPaths() []string {
	return []string{filepath.Join(desktopFolder(), edition.Title+".lnk"), filepath.Join(startMenuFolder(), edition.Title+".lnk"),
		filepath.Join(startMenuFolder(), "Uninstall "+edition.Title+".lnk")}
}

// makeShortcut writes a .lnk file through the Windows Script Host shell object
func makeShortcut(lnk, target, dir, icon string, admin bool) error {
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		if oe, ok := err.(*ole.OleError); !ok || (oe.Code() != 1 && oe.Code() != 0x80010106) { // S_FALSE / already initialised
			return err
		}
	}
	defer ole.CoUninitialize()
	unknown, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return err
	}
	defer unknown.Release()
	shell, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer shell.Release()
	v, err := oleutil.CallMethod(shell, "CreateShortcut", lnk)
	if err != nil {
		return err
	}
	sc := v.ToIDispatch()
	defer sc.Release()
	oleutil.PutProperty(sc, "TargetPath", target)
	oleutil.PutProperty(sc, "WorkingDirectory", dir)
	oleutil.PutProperty(sc, "IconLocation", icon+",0")
	oleutil.PutProperty(sc, "Description", edition.Title)
	if _, err := oleutil.CallMethod(sc, "Save"); err != nil {
		return err
	}
	if admin { // the shortcut's "Run as administrator" flag (byte 0x15, bit 0x20)
		if b, err := os.ReadFile(lnk); err == nil && len(b) > 0x15 {
			b[0x15] |= 0x20
			os.WriteFile(lnk, b, 0644)
		}
	}
	return nil
}

func makeShortcuts(game string, desktop, startMenu bool) error {
	target := filepath.Join(game, filepath.FromSlash(edition.Launcher))
	dir := filepath.Dir(target)
	admin := edition.AdminLink && underProgramFiles(game)
	links := shortcutPaths()
	var firstErr error
	try := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if desktop {
		try(makeShortcut(links[0], target, dir, target, admin))
	}
	if startMenu {
		os.MkdirAll(startMenuFolder(), 0755)
		try(makeShortcut(links[1], target, dir, target, admin))
		un := filepath.Join(game, filepath.FromSlash(edition.Uninstaller))
		try(makeShortcut(links[2], un, filepath.Dir(un), un, false))
	}
	return firstErr
}

func removeShortcuts() {
	for _, l := range shortcutPaths() {
		os.Remove(l)
	}
	os.Remove(startMenuFolder())
}

const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\`

func register(game string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey+edition.ID, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	k.SetStringValue("DisplayName", edition.Title)
	k.SetStringValue("DisplayVersion", edition.Version)
	k.SetStringValue("Publisher", edition.Title+" mod")
	k.SetStringValue("InstallLocation", filepath.Join(game, edition.ModFolder))
	k.SetStringValue("DisplayIcon", filepath.Join(game, filepath.FromSlash(edition.Launcher)))
	k.SetStringValue("UninstallString", `"`+filepath.Join(game, filepath.FromSlash(edition.Uninstaller))+`"`)
	k.SetDWordValue("NoModify", 1)
	k.SetDWordValue("NoRepair", 1)
	if edition.SizeKB > 0 {
		k.SetDWordValue("EstimatedSize", uint32(edition.SizeKB))
	}
	return nil
}

func unregister() { registry.DeleteKey(registry.CURRENT_USER, uninstallKey+edition.ID) }

// removeAfterRestart asks Windows to delete a file or an empty folder at the next restart (the
// running uninstaller cannot delete itself). Needs administrator rights; false if not possible.
func removeAfterRestart(p string) bool {
	q, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return false
	}
	return windows.MoveFileEx(q, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT) == nil
}

// runLauncher runs the mod's own launcher (to put the original game files back); false if it failed
func runLauncher(game string, args []string) (bool, bool) {
	l := filepath.Join(game, filepath.FromSlash(edition.Launcher))
	if _, err := os.Stat(l); err != nil {
		return true, false
	}
	cmd := exec.Command(l, args...)
	cmd.Dir = filepath.Dir(l)
	return cmd.Run() == nil, true
}

// running tells whether the game or the mod's launcher is open (a running program cannot be renamed)
func running(game string) bool {
	for _, p := range []string{filepath.Join(game, filepath.FromSlash(edition.Launcher)), filepath.Join(game, edition.ModFolder, "Hydro.exe")} {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := os.Rename(p, p+".check"); err != nil {
			return true
		}
		os.Rename(p+".check", p)
	}
	return false
}

func cacheInUse(game string) bool {
	for _, other := range []string{"SacrificeHD", "SacrificeHydroHD"} {
		if other == edition.ModFolder {
			continue
		}
		if _, err := os.Stat(filepath.Join(game, other)); err == nil {
			return true
		}
	}
	return false
}
