Sacrifice HD 1.1.1 - source code
================================

Sacrifice HD is a visual overhaul for Sacrifice (Shiny Entertainment, 2000): it upscales the
game's textures from the player's own copy with Real-ESRGAN, gives them richer colours, and runs
the game through dgVoodoo2 with modern rendering settings. None of Sacrifice's files are part of
the mod or of this source.

BUILD.txt explains step by step how to build both downloads from this folder, and how to check
that your build is identical to the published 1.1.1 (CHECKSUMS.txt).

What is where
-------------
launcher\           SacrificeHD.exe (Go, standard library only)
  main.go             start-up, options (-regen, -generate-only, -restore, -game), dgVoodoo settings,
                      starting Sacrifice and putting the original files back afterwards; also the
                      testing mode (dev.txt, see below)
  pipeline.go         the texture pipeline: which textures to upscale, upscaling, grading, writing
  wad.go, tex.go      reading and writing Sacrifice's .wad archives and its texture formats
  upscale.go          running realesrgan-ncnn-vulkan (or the built-in Lanczos upscaler without Vulkan)
  imgops.go, quant.go colour grading, sharpening, alpha handling and palette reduction
  swap.go             putting the HD wads in place while the game runs, and restoring the originals
  settings.go         the game's own render settings (its registry entry), set from the sheets
  sys_*.go            Windows-specific helpers (one launcher at a time, hidden helper windows)
  input_*.go, shot_*.go   testing aids, only active when a dev.txt file sits next to the launcher
                      (it is never shipped): scripted mouse/keyboard input and game screenshots,
                      used to test the mod automatically
  gen_sheets.go       generated from sheets\*.json by tools\sheets.py - do not edit by hand
sheets\             the design rules as data: asset classes (which textures, how much bigger),
                    colour grades, renderer settings, the hooks into the game, the upscalers
tools\              Python helpers used while developing
  sheets.py           checks the sheets and generates launcher\gen_sheets.go
  wad.py              reader/writer for the game's .wad archives
  proto.py, check_keys.py   prototypes/checks from the development of the pipeline
installer\          the setup / uninstaller (Go; lxn/walk, go-ole, golang.org/x/sys)
  editions\sachd.json, edition.json   the Sacrifice HD settings of the installer
  verinfo\            writes the icon, manifest and version information (uses goversioninfo, MIT)
  core_test.go        tests of the install / update / uninstall logic ("go test .")
release\            the tool that builds the two downloads (go run . -thirdparty ... -out ...)
third-party\        dgVoodoo.template.conf (dgVoodoo2's own default configuration, unchanged; the
                    launcher writes the game's dgVoodoo.conf from it)
licenses\           the licences shipped with the mod
README-mod.txt      the README that goes into the mod
CHECKSUMS.txt       SHA-256 of every file of the published 1.1.1

Third-party parts (not in this folder; BUILD.txt says where to download them)
-----------------------------------------------------------------------------
- dgVoodoo2 2.87.5 by Dege (freeware; its readme allows shipping its files with a mod)
- dinputto8 by Elisha Riedlinger (zlib licence)
- realesrgan-ncnn-vulkan (MIT) and the realesr-animevideov3 models (BSD-3-Clause), Xintao Wang et al.
All of them are shipped exactly as their authors released them.
