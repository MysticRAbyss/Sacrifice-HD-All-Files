package main

import (
	"image"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	gdi32            = syscall.NewLazyDLL("gdi32.dll")
	pEnumWindows     = user32.NewProc("EnumWindows")
	pGetWinPid       = user32.NewProc("GetWindowThreadProcessId")
	pIsVisible       = user32.NewProc("IsWindowVisible")
	pGetClientRect   = user32.NewProc("GetClientRect")
	pGetDC           = user32.NewProc("GetDC")
	pReleaseDC       = user32.NewProc("ReleaseDC")
	pPrintWindow     = user32.NewProc("PrintWindow")
	pCreateCompatDC  = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatBmp = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject    = gdi32.NewProc("SelectObject")
	pGetDIBits       = gdi32.NewProc("GetDIBits")
	pDeleteObject    = gdi32.NewProc("DeleteObject")
	pDeleteDC        = gdi32.NewProc("DeleteDC")
	pBitBlt          = gdi32.NewProc("BitBlt")
	pClientToScreen  = user32.NewProc("ClientToScreen")
)

func windowOfPid(pid uint32) uintptr {
	var found uintptr
	var bestArea int32
	cb := syscall.NewCallback(func(h uintptr, _ uintptr) uintptr {
		var p uint32
		pGetWinPid.Call(h, uintptr(unsafe.Pointer(&p)))
		if p == pid {
			if v, _, _ := pIsVisible.Call(h); v != 0 {
				var r [4]int32
				pGetClientRect.Call(h, uintptr(unsafe.Pointer(&r)))
				if a := r[2] * r[3]; a > bestArea {
					bestArea, found = a, h
				}
			}
		}
		return 1
	})
	pEnumWindows.Call(cb, 0)
	return found
}

// captureGame saves the game window's own content (no overlays) as a PNG.
func captureGame(dir string) (string, error) {
	curMu.Lock()
	var pid uint32
	if cur != nil && cur.Process != nil {
		pid = uint32(cur.Process.Pid)
	}
	curMu.Unlock()
	h := windowOfPid(pid)
	if h == 0 {
		return "", os.ErrNotExist
	}
	var r [4]int32
	pGetClientRect.Call(h, uintptr(unsafe.Pointer(&r)))
	w, ht := r[2], r[3]
	sdc, _, _ := pGetDC.Call(h)
	defer pReleaseDC.Call(h, sdc)
	mdc, _, _ := pCreateCompatDC.Call(sdc)
	defer pDeleteDC.Call(mdc)
	bmp, _, _ := pCreateCompatBmp.Call(sdc, uintptr(w), uintptr(ht))
	defer pDeleteObject.Call(bmp)
	pSelectObject.Call(mdc, bmp)
	type bih struct {
		Size                          uint32
		Width, Height                 int32
		Planes, BitCount              uint16
		Compression, SizeImage        uint32
		XPels, YPels, ClrUsed, ClrImp int32
	}
	hdr := bih{Size: 40, Width: w, Height: -ht, Planes: 1, BitCount: 32}
	buf := make([]byte, int(w)*int(ht)*4)
	for try := 0; try < 25; try++ {
		pPrintWindow.Call(h, mdc, 3) // PW_CLIENTONLY | PW_RENDERFULLCONTENT
		pGetDIBits.Call(mdc, bmp, 0, uintptr(ht), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&hdr)), 0)
		lit := 0
		for i := 0; i < len(buf); i += 4 * 997 {
			if buf[i]|buf[i+1]|buf[i+2] != 0 {
				lit++
			}
		}
		if lit > 50 {
			break
		}
		if try >= 3 {
			// flip-model swap chains often print black: copy the window's area of the screen instead
			var pt [2]int32
			pClientToScreen.Call(h, uintptr(unsafe.Pointer(&pt)))
			scr, _, _ := pGetDC.Call(0)
			pBitBlt.Call(mdc, 0, 0, uintptr(w), uintptr(ht), scr, uintptr(pt[0]), uintptr(pt[1]), 0x00CC0020|0x40000000)
			pReleaseDC.Call(0, scr)
			pGetDIBits.Call(mdc, bmp, 0, uintptr(ht), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&hdr)), 0)
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	im := image.NewNRGBA(image.Rect(0, 0, int(w), int(ht)))
	for i := 0; i < int(w)*int(ht); i++ {
		im.Pix[i*4], im.Pix[i*4+1], im.Pix[i*4+2], im.Pix[i*4+3] = buf[i*4+2], buf[i*4+1], buf[i*4], 255
	}
	os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, "shot_"+time.Now().Format("150405")+".png")
	return p, SavePNG(p, im)
}
