//go:build windows

package main

// Setup and uninstaller (the same program: named "Uninstall ..." it uninstalls)

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

func argValue(name string) (string, bool) {
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, name+"=") {
			return strings.Trim(a[len(name)+1:], `"`), true
		}
		if a == name {
			return "", true
		}
	}
	return "", false
}

func main() {
	exe, _ := os.Executable()
	_, un := argValue("--uninstall")
	if un || strings.Contains(strings.ToLower(filepath.Base(exe)), "uninstall") {
		runUninstaller(exe)
		return
	}
	runSetup(exe)
}

func runSetup(exe string) {
	title := edition.Title + " " + edition.Version + " Setup"
	exeDir := filepath.Dir(exe)
	filesDir := filepath.Join(exeDir, "files")
	if st, err := os.Stat(filesDir); err != nil || !st.IsDir() {
		walk.MsgBox(nil, title, "The \"files\" folder is not next to the setup.\r\n\r\n"+
			"Unzip the whole download first (right-click the zip, \"Extract All...\"), then run the setup from the unzipped folder.", walk.MsgBoxIconInformation)
		return
	}
	folder, given := argValue("--dir")
	if !given {
		folder = guessGameFolder(exeDir)
	}
	_, autoInstall := argValue("--install")

	var mw *walk.MainWindow
	var folderEdit *walk.LineEdit
	var check, target *walk.Label
	var desktop, startMenu *walk.CheckBox
	var progress *walk.ProgressBar
	var installBtn, cancelBtn, browseBtn *walk.PushButton
	installedIn := ""

	validate := func() bool {
		t := strings.TrimSpace(folderEdit.Text())
		f := normalizeGameFolder(t)
		if f == "" {
			if t == "" {
				check.SetText("Choose the folder where Sacrifice is installed (it has the \"data\" and \"maps\" folders).")
			} else {
				check.SetText("This is not a Sacrifice folder. Missing: " + strings.Join(missingGameFiles(t), ", "))
			}
			target.SetText("")
			installBtn.SetEnabled(false)
			return false
		}
		check.SetText("Sacrifice found.")
		target.SetText("The mod goes into: " + filepath.Join(f, edition.ModFolder))
		installBtn.SetEnabled(true)
		return true
	}

	install := func() {
		if installedIn != "" { // "Play now"
			l := filepath.Join(installedIn, filepath.FromSlash(edition.Launcher))
			cmd := exec.Command(l)
			cmd.Dir = filepath.Dir(l)
			cmd.Start()
			mw.Close()
			return
		}
		if !validate() {
			return
		}
		game := normalizeGameFolder(folderEdit.Text())
		if running(game) {
			walk.MsgBox(mw, title, "Please close the game and the mod's launcher first.", walk.MsgBoxIconInformation)
			return
		}
		if !canWrite(game) {
			if walk.MsgBox(mw, title, "Windows needs administrator rights to put files into\r\n"+game+".\r\n\r\nContinue as administrator?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes {
				if runElevated(fmt.Sprintf(`--dir="%s" --install`, game)) {
					mw.Close()
				}
			}
			return
		}
		wantDesktop, wantStart := desktop.Checked(), startMenu.Checked()
		installBtn.SetEnabled(false)
		cancelBtn.SetEnabled(false)
		browseBtn.SetEnabled(false)
		folderEdit.SetReadOnly(true)
		check.SetText("Installing...")
		go func() {
			var err error
			if len(edition.Restore) > 0 { // an update: the mod's launcher first puts the original game files back
				if ok, existed := runLauncher(game, restoreArgs(game)); existed && !ok {
					err = fmt.Errorf("the original game files could not be put back - is the game still running?")
				}
			}
			var files []string
			if err == nil {
				files, err = installFiles(filesDir, game, func(done, total int) {
					mw.Synchronize(func() { progress.SetRange(0, total); progress.SetValue(done) })
				})
			}
			if err == nil {
				err = writeManifest(game, files)
			}
			var warn []string
			if err == nil {
				if e := register(game); e != nil {
					warn = append(warn, "It could not be added to Apps & features ("+e.Error()+"); to remove it, run "+filepath.Base(edition.Uninstaller)+" in its folder.")
				}
				if e := makeShortcuts(game, wantDesktop, wantStart); e != nil {
					warn = append(warn, "The shortcuts could not be made ("+e.Error()+"); start "+filepath.Base(edition.Launcher)+" in "+edition.ModFolder+".")
				}
			}
			mw.Synchronize(func() {
				if err != nil {
					walk.MsgBox(mw, title, "The installation did not finish:\r\n"+err.Error(), walk.MsgBoxIconError)
					check.SetText("The installation did not finish.")
					installBtn.SetEnabled(true)
					cancelBtn.SetEnabled(true)
					browseBtn.SetEnabled(true)
					folderEdit.SetReadOnly(false)
					return
				}
				if len(warn) > 0 {
					walk.MsgBox(mw, title, strings.Join(warn, "\r\n\r\n"), walk.MsgBoxIconWarning)
				}
				installedIn = game
				msg := edition.Title + " is installed. Start it with \"" + edition.Title + "\" on the Desktop or in the Start menu."
				for _, n := range edition.Notes {
					msg += "\r\n" + n
				}
				check.SetText(msg)
				installBtn.SetText("Play now")
				installBtn.SetEnabled(true)
				cancelBtn.SetText("Close")
				cancelBtn.SetEnabled(true)
			})
		}()
	}

	intro := "This puts " + edition.Title + " into your Sacrifice folder.\r\n\r\n" +
		"It needs the original Sacrifice (GOG or Steam version). None of the game's files come with the mod; " +
		"it uses the ones of your copy."
	vc := hasVCRuntime() || edition.ModFolder == "SacrificeHD"

	err := MainWindow{
		AssignTo: &mw,
		Title:    title,
		MinSize:  Size{Width: 560, Height: 430},
		Size:     Size{Width: 600, Height: 460},
		Layout:   VBox{Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14}},
		Children: []Widget{
			Label{Text: edition.Title + " " + edition.Version, Font: Font{PointSize: 15, Bold: true}},
			Label{Text: intro},
			GroupBox{Title: "Sacrifice folder", Layout: HBox{}, Children: []Widget{
				LineEdit{AssignTo: &folderEdit, Text: folder, OnTextChanged: func() { validate() }},
				PushButton{AssignTo: &browseBtn, Text: "Browse...", OnClicked: func() {
					dlg := walk.FileDialog{Title: "Choose the Sacrifice folder", InitialDirPath: folderEdit.Text()}
					if ok, _ := dlg.ShowBrowseFolder(mw); ok {
						if f := normalizeGameFolder(dlg.FilePath); f != "" {
							folderEdit.SetText(f)
						} else {
							folderEdit.SetText(dlg.FilePath)
						}
					}
				}},
			}},
			Label{AssignTo: &check, Text: ""},
			Label{AssignTo: &target, Text: ""},
			CheckBox{AssignTo: &desktop, Text: "Shortcut on the Desktop", Checked: true},
			CheckBox{AssignTo: &startMenu, Text: "Shortcuts in the Start menu", Checked: true},
			Composite{Visible: !vc, Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{Text: "The game needs the Microsoft Visual C++ Redistributable (x64), which this PC does not have yet."},
				PushButton{Text: "Get it", OnClicked: func() { openURL(vcRedistURL) }},
			}},
			VSpacer{},
			ProgressBar{AssignTo: &progress},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				HSpacer{},
				PushButton{AssignTo: &installBtn, Text: "Install", MinSize: Size{Width: 110}, OnClicked: install},
				PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { mw.Close() }},
			}},
		},
	}.Create()
	if err != nil {
		walk.MsgBox(nil, title, err.Error(), walk.MsgBoxIconError)
		return
	}
	validate()
	if autoInstall {
		install()
	}
	mw.Run()
}

func runUninstaller(exe string) {
	title := "Uninstall " + edition.Title
	game, given := argValue("--dir")
	if !given {
		game = normalizeGameFolder(filepath.Dir(exe))
	}
	if game == "" {
		walk.MsgBox(nil, title, "The Sacrifice folder was not found next to the uninstaller.", walk.MsgBoxIconError)
		return
	}
	if !canWrite(game) {
		if runElevated(fmt.Sprintf(`--uninstall --dir="%s"`, game)) {
			return
		}
		walk.MsgBox(nil, title, "Administrator rights are needed to remove the mod from\r\n"+game, walk.MsgBoxIconError)
		return
	}
	var dlg *walk.Dialog
	var all *walk.CheckBox
	remove, everything := false, false
	_, err := Dialog{
		AssignTo: &dlg,
		Title:    title,
		MinSize:  Size{Width: 460, Height: 200},
		Layout:   VBox{Margins: Margins{Left: 14, Top: 12, Right: 14, Bottom: 12}},
		Children: []Widget{
			Label{Text: "Remove " + edition.Title + " from\r\n" + game + "?\r\n\r\nYour Sacrifice game itself is not touched."},
			CheckBox{AssignTo: &all, Text: "Also delete my settings and saved games", Visible: len(edition.UserFiles) > 0},
			VSpacer{},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				HSpacer{},
				PushButton{Text: "Remove", MinSize: Size{Width: 90}, OnClicked: func() { remove, everything = true, all.Checked(); dlg.Accept() }},
				PushButton{Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Run(nil)
	if err != nil || !remove {
		return
	}
	if running(game) {
		walk.MsgBox(nil, title, "Please close the game and the mod's launcher first, then run the uninstaller again.", walk.MsgBoxIconInformation)
		return
	}
	if len(edition.Restore) > 0 {
		if ok, existed := runLauncher(game, restoreArgs(game)); existed && !ok {
			walk.MsgBox(nil, title, "Your original game files could not be put back (is Sacrifice still running?).\r\nClose the game and run the uninstaller again.", walk.MsgBoxIconError)
			return
		}
	}
	failed := removeFiles(game, exe, everything)
	if edition.HDCache && !cacheInUse(game) {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			os.RemoveAll(filepath.Join(la, "SacrificeHD"))
		}
	}
	removeShortcuts()
	unregister()
	msg := edition.Title + " has been removed."
	if len(failed) > 0 {
		msg += "\r\n\r\nSome files could not be deleted (are they open?):\r\n" + strings.Join(failed, "\r\n")
	}
	mod := filepath.Join(game, edition.ModFolder)
	// this uninstaller is still running, so Windows removes it (and the folder, if empty) after a restart
	if removeAfterRestart(exe) {
		removeAfterRestart(mod)
	} else {
		msg += "\r\n\r\nThe folder " + mod + " now only holds the uninstaller (and your settings, if kept); you can delete it."
	}
	walk.MsgBox(nil, title, msg, walk.MsgBoxIconInformation)
}
