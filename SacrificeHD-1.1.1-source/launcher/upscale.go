package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// runUpscale fills job.out for every job, using bundled Real-ESRGAN when it works and the built-in
// Lanczos+sharpen fallback otherwise. Returns the back-end used.
func runUpscale(modDir, work string, js []*job, progress func(string)) string {
	byScale := map[int][]*job{}
	for _, j := range js {
		byScale[j.scale] = append(byScale[j.scale], j)
	}
	used := "lanczos"
	for s, list := range byScale {
		var up *Upscaler
		for i := range Upscalers {
			if Upscalers[i].Scale == s && Upscalers[i].Exe != "" {
				up = &Upscalers[i]
			}
		}
		ok := false
		if up != nil && os.Getenv("SACRIFICEHD_NO_GPU") == "" {
			ok = esrgan(modDir, work, up, list, progress)
			if ok {
				used = up.ID
			}
		}
		if !ok {
			progress(fmt.Sprintf("Upscaling %d images x%d on the CPU", len(list), s))
		}
	}
	return used
}

// result returns the upscaled image: Real-ESRGAN's file when there is one, else Lanczos + sharpen.
func (j *job) result() *FImg {
	if j.out != nil {
		return j.out
	}
	if j.outPath != "" {
		if im, err := LoadPNG(j.outPath); err == nil {
			f := FromImage(im)
			if f.W != j.src.W*j.scale || f.H != j.src.H*j.scale {
				f = Resize(f, j.src.W*j.scale, j.src.H*j.scale, false)
			}
			os.Remove(j.outPath)
			j.out = f
			return f
		}
	}
	f := Resize(j.src, j.src.W*j.scale, j.src.H*j.scale, false)
	ApplyGrade(f, Grade{Saturation: 1, Contrast: 1, SharpenRadius: 1.0, SharpenAmount: 0.6})
	j.out = f
	return f
}

func esrgan(modDir, work string, up *Upscaler, list []*job, progress func(string)) bool {
	exe := filepath.Join(modDir, filepath.FromSlash(up.Exe))
	if _, err := os.Stat(exe); err != nil {
		logf("upscaler missing: %s", exe)
		return false
	}
	in := filepath.Join(work, fmt.Sprintf("in%d", up.Scale))
	out := filepath.Join(work, fmt.Sprintf("out%d", up.Scale))
	os.RemoveAll(in)
	os.RemoveAll(out)
	os.MkdirAll(in, 0o755)
	os.MkdirAll(out, 0o755)
	progress(fmt.Sprintf("Preparing %d images for x%d upscaling", len(list), up.Scale))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, j := range list {
		j := j
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// pad tiny frames: Real-ESRGAN needs a little context
			SavePNG(filepath.Join(in, j.id+".png"), j.src.ToNRGBA(nil))
		}()
	}
	wg.Wait()
	progress(fmt.Sprintf("Upscaling %d images x%d on the GPU (Real-ESRGAN)", len(list), up.Scale))
	args := []string{"-i", in, "-o", out, "-n", up.Model + fmt.Sprintf("-x%d", up.Scale), "-m", filepath.Join(filepath.Dir(exe), "models")}
	args = append(args, strings.Fields(up.Args)...)
	c := exec.Command(exe, args...)
	c.Dir = filepath.Dir(exe)
	hideWindow(c)
	b, err := c.CombinedOutput()
	if err != nil {
		logf("Real-ESRGAN failed (%v): %s", err, tail(string(b), 800))
		return false
	}
	bad := 0
	for _, j := range list {
		p := filepath.Join(out, j.id+".png")
		if _, err := os.Stat(p); err != nil {
			bad++
			continue
		}
		j.outPath = p
	}
	os.RemoveAll(in)
	if bad > 0 {
		logf("Real-ESRGAN produced no output for %d images; those use the CPU path", bad)
	}
	return bad < len(list)
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
