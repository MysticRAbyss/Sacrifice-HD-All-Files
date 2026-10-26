package main

import (
	"strconv"
	"strings"
	"time"
	"unsafe"
)

var pSendInput = user32.NewProc("SendInput")

type mouseInput struct {
	typ       uint32
	_         uint32
	dx, dy    int32
	mouseData uint32
	flags     uint32
	time      uint32
	_         uint32
	extra     uintptr
}

type keyInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	_     uint32
	extra uintptr
	_     [8]byte
}

func sendMouse(dx, dy int32, flags uint32) {
	in := mouseInput{typ: 0, dx: dx, dy: dy, flags: flags}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

func sendKey(vk uint16, up bool) {
	in := keyInput{typ: 1, vk: vk}
	if up {
		in.flags = 2
	}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

// runInput plays a testing script of relative mouse moves and clicks, which DirectInput games see
// the same way as a real mouse: "m:dx,dy" (split into small steps), "c" left click, "r" right click,
// "md"/"mu" middle down/up, "w:ms" wait, "k:VK" key press (decimal virtual-key code).
func runInput(script string) {
	for _, tok := range strings.Fields(script) {
		switch {
		case strings.HasPrefix(tok, "m:"):
			p := strings.Split(tok[2:], ",")
			dx, _ := strconv.Atoi(p[0])
			dy, _ := strconv.Atoi(p[1])
			for dx != 0 || dy != 0 {
				sx, sy := clampStep(dx), clampStep(dy)
				sendMouse(int32(sx), int32(sy), 0x0001)
				dx -= sx
				dy -= sy
				time.Sleep(8 * time.Millisecond)
			}
		case tok == "c":
			sendMouse(0, 0, 0x0002)
			time.Sleep(80 * time.Millisecond)
			sendMouse(0, 0, 0x0004)
		case tok == "r":
			sendMouse(0, 0, 0x0008)
			time.Sleep(80 * time.Millisecond)
			sendMouse(0, 0, 0x0010)
		case tok == "md":
			sendMouse(0, 0, 0x0020)
		case tok == "mu":
			sendMouse(0, 0, 0x0040)
		case strings.HasPrefix(tok, "w:"):
			ms, _ := strconv.Atoi(tok[2:])
			time.Sleep(time.Duration(ms) * time.Millisecond)
		case strings.HasPrefix(tok, "k:"):
			vk, _ := strconv.Atoi(tok[2:])
			sendKey(uint16(vk), false)
			time.Sleep(60 * time.Millisecond)
			sendKey(uint16(vk), true)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func clampStep(v int) int {
	if v > 4 {
		return 4
	}
	if v < -4 {
		return -4
	}
	return v
}
