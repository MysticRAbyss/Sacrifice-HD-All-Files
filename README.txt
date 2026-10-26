Sacrifice HD 1.1.1
==================

A visual overhaul for Sacrifice (Shiny Entertainment, 2000). Works with the GOG version and other
installs of the final patch (build 0678).

Install
- Easiest: unzip "SacrificeHD-1.1.1-Setup.zip" (right-click, "Extract All...") and run
  "Install Sacrifice HD.exe" in the unzipped folder. It finds your Sacrifice folder (or lets you
  pick it), installs the mod and adds a "Sacrifice HD" shortcut to your desktop and Start menu.
- Manual: unzip "SacrificeHD-1.1.1-Manual.zip" and copy everything in it into your Sacrifice folder
  (the one with Sacrifice.exe), so that DDraw.dll, D3DImm.dll and dinput.dll sit next to
  Sacrifice.exe and the SacrificeHD folder sits inside it. Start the game with
  SacrificeHD\SacrificeHD.exe.
- Always start the game through Sacrifice HD; it puts the HD files in place while you play and puts
  your original files back when the game closes.
- The first start builds the HD textures from YOUR copy of the game (a few minutes, once). None of
  Sacrifice's own files are included in this mod.

Updating
- Run the new setup over your existing install (from 1.0.0 the first start rebuilds the HD
  textures once; from 1.1.0 nothing changes but the installer).

Uninstall
- Setup version: Windows' Apps & features, the Start menu, or "Uninstall Sacrifice HD.exe" in the
  SacrificeHD folder. Manual version: delete DDraw.dll, D3DImm.dll, dinput.dll, dgVoodoo.conf and
  the SacrificeHD folder. Your game files are never permanently changed.

Changes in 1.1.1
- A new installer: it no longer carries the mod's files inside itself (they sit next to it in the
  download), it uses only standard Windows functions, and the uninstaller is an ordinary part of
  the mod. The mod itself is unchanged.

What it does
- Upscaled, sharper and richer textures: terrain detail, sky and clouds, buildings and world models,
  spell effects (4x, more vivid), souls, HUD frames, spell icons and menus.
- Sharper, more vivid creature and wizard skins, kept at their original size so every creature
  renders correctly. A few skins stay original because the engine shares them between creatures
  (the Netherfiend and the races in saxs_r9/r10/r11).
- Rendering through dgVoodoo2: full screen at your desktop height stretched to fill the screen,
  2x supersampling, 8x anti-aliasing, 16x anisotropic filtering, per-pixel lighting, sharp scaling.
- 60 fps cap: above 60 fps the computer players stop moving.
- Stability fixes for modern Windows (DirectInput wrapper and compatibility settings); the game no
  longer needs to run as administrator (unless it is installed under Program Files).
- Upscaling uses Real-ESRGAN on your graphics card (Vulkan); without one it uses a slower built-in
  upscaler.

Multiplayer
- Visual only. You can play with people who don't have the mod.

Options (SacrificeHD.exe)
  -regen            rebuild the HD textures
  -generate-only    build the textures and exit
  -restore          put the original game files back and exit

Credits
- Real-ESRGAN / realesrgan-ncnn-vulkan by Xintao Wang et al. (BSD-3-Clause / MIT)
- dgVoodoo2 by Dege (freeware; redistributed as its readme allows)
- dinputto8 by Elisha Riedlinger (zlib license)
- Sacrifice HD launcher, installer and texture pipeline: built with Claude
- The installer uses walk and win (The Walk Authors, BSD-3-Clause), go-ole (MIT) and
  golang.org/x/sys (BSD-3-Clause); licences in the licenses folder.
