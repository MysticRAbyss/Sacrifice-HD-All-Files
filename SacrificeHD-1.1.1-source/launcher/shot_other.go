//go:build !windows

package main

import "os"

func captureGame(dir string) (string, error) { return "", os.ErrNotExist }
