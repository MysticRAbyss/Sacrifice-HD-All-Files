package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const (
	regHKLM  = `HKLM\SOFTWARE\WOW6432Node\Shiny Entertainment\Sacrifice`
	regVStor = `HKCU\Software\Classes\VirtualStore\MACHINE\SOFTWARE\WOW6432Node\Shiny Entertainment\Sacrifice`
)

func reg(args ...string) (string, error) {
	c := exec.Command("reg", args...)
	hideWindow(c)
	out, err := c.CombinedOutput()
	return string(out), err
}

// applyGameOptions makes the unelevated game (which reads the per-user VirtualStore copy of its
// HKLM key) start from the machine settings plus the overhaul's render rows.
func applyGameOptions() {
	// First run only: start the per-user copy from the machine settings. Afterwards the player's
	// own in-game option changes (saved in the per-user copy) are kept.
	if _, err := reg("query", regVStor+`\Options`, "/v", "SacrificeHD"); err != nil {
		if out, err := reg("query", regHKLM); err == nil && strings.Contains(out, "Sacrifice") {
			if o, err := reg("copy", regHKLM, regVStor, "/s", "/f"); err != nil {
				logf("reg copy failed: %v %s", err, o)
			}
		}
		reg("add", regVStor+`\Options`, "/v", "SacrificeHD", "/t", "REG_DWORD", "/d", "1", "/f")
	}
	w, h := gameMode()
	res := make([]byte, 24)
	for i, v := range []uint32{uint32(w), uint32(h), 1, 32, 1, 0} {
		binary.LittleEndian.PutUint32(res[i*4:], v)
	}
	opts := regVStor + `\Options`
	set := func(name, typ, val string) {
		if o, err := reg("add", opts, "/v", name, "/t", typ, "/d", val, "/f"); err != nil {
			logf("reg add %s failed: %v %s", name, err, o)
		}
	}
	set("Resolution", "REG_BINARY", hex.EncodeToString(res))
	n := 0
	for _, r := range RenderRows {
		if r.Target != "game_option" {
			continue
		}
		if v, err := strconv.ParseUint(r.Value, 0, 32); err == nil {
			set(r.Key, "REG_DWORD", fmt.Sprint(v))
			n++
		}
	}
	reg("add", regVStor+`\Direct3D options`, "/v", "Direct3D Driver", "/t", "REG_SZ", "/d", "dgVoodoo HAL", "/f")
	reg("add", regVStor+`\Direct3D options`, "/v", "DirectX Device", "/t", "REG_SZ", "/d", "display", "/f")
	logf("game options set: %dx%dx32 + %d rows", w, h, n)
}

// gameMode is the resolution the game runs at: the largest 4:3 mode at the desktop's height.
// Sacrifice culls objects with a 4:3 view volume, so at widescreen modes units, souls and
// buildings near the top and bottom of the screen vanish while the camera moves.
// dgVoodoo scales the 4:3 image to the screen with side bars (ScalingMode stretched_ar).
func gameMode() (int, int) {
	_, h := desktopSize()
	w := h * 4 / 3
	w -= w % 8
	return w, h
}
