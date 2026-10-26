//go:build !windows

package main

import "os/exec"

func hideWindow(c *exec.Cmd)  {}
func freeConsole()            {}
func desktopSize() (int, int) { return 1920, 1080 }
func firstInstance() bool     { return true }
