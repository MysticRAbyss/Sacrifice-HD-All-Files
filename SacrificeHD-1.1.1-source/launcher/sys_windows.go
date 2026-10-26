package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

func hideWindow(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

func freeConsole() { syscall.NewLazyDLL("kernel32.dll").NewProc("FreeConsole").Call() }

func desktopSize() (int, int) {
	u := syscall.NewLazyDLL("user32.dll")
	if sp := u.NewProc("SetProcessDPIAware"); sp.Find() == nil {
		sp.Call()
	}
	gsm := u.NewProc("GetSystemMetrics")
	w, _, _ := gsm.Call(0)
	h, _, _ := gsm.Call(1)
	if w == 0 || h == 0 {
		return 1024, 768
	}
	return int(w), int(h)
}

// firstInstance holds a named mutex for the launcher's lifetime; false if another launcher is running.
func firstInstance() bool {
	name, _ := syscall.UTF16PtrFromString("Local\\SacrificeHD-launcher")
	h, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return true
	}
	return err != syscall.Errno(183) // ERROR_ALREADY_EXISTS
}
