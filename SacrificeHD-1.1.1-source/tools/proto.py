"""Prototype HD repack (Lanczos only) used to test what the engine accepts.
usage: proto.py in.wad out.wad scale [tint]"""
import os, sys, struct
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import wad
import numpy as np
from PIL import Image

def palettes(root):
    pal = {}
    for p, e in root.walk():
        if e.type == "PALT": pal.setdefault("/".join(p.split("/")[:-1]), {})[e.name] = e
    return pal

def find_pal(pal, p, tag):
    parts = p.split("/")[:-1]
    while parts:
        f = "/".join(parts)
        if f in pal and tag in pal[f]: return pal[f][tag]
        parts = parts[:-1]
    return None

def main(inp, outp, scale, tint=False):
    r, h = wad.read(inp)
    pal = palettes(r)
    groups = {}
    for p, e in r.walk():
        if e.flag & 1 or e.type not in ("TXTR", "ICON"): continue
        tag = e.data[8:12][::-1].decode('latin1')
        pe = find_pal(pal, p, tag)
        if pe is None: continue  # GrAy etc: handled as gray below
        groups.setdefault(id(pe), (pe, []))[1].append(e)
    for pe, texs in groups.values():
        P = np.frombuffer(pe.data[8:8+768], np.uint8).reshape(256, 3)
        imgs = []
        for e in texs:
            w, hh = struct.unpack("<II", e.data[:8])
            idx = np.frombuffer(e.data[36:36+w*hh], np.uint8).reshape(hh, w)
            rgb = P[idx]
            im = Image.fromarray(rgb).resize((w*scale, hh*scale), Image.LANCZOS)
            key = Image.fromarray((idx == 0).astype(np.uint8)*255).resize((w*scale, hh*scale), Image.NEAREST)
            a = np.asarray(im).astype(np.int16)
            if tint: a[..., 1] = np.clip(a[..., 1] + 90, 0, 255)
            imgs.append((e, w, hh, a.astype(np.uint8), np.asarray(key) > 127))
        # joint quantization to 255 colours + index 0 reserved
        allpx = np.concatenate([a[~k] for _, _, _, a, k in imgs]) if imgs else np.zeros((1,3),np.uint8)
        if len(allpx) == 0: allpx = np.zeros((1, 3), np.uint8)
        side = int(np.ceil(np.sqrt(len(allpx))))
        canvas = np.zeros((side*side, 3), np.uint8); canvas[:len(allpx)] = allpx; canvas[len(allpx):] = allpx[0]
        q = Image.fromarray(canvas.reshape(side, side, 3)).quantize(255, method=Image.Quantize.MEDIANCUT, dither=Image.Dither.NONE)
        qp = np.array(q.getpalette()[:255*3], np.uint8).reshape(-1, 3)
        newP = np.vstack([P[0:1], qp, np.zeros((256-1-len(qp), 3), np.uint8)])[:256]
        palimg = Image.new("P", (1, 1)); palimg.putpalette(newP[1:].flatten().tolist() + [0]*3)
        for e, w, hh, a, k in imgs:
            qi = np.asarray(Image.fromarray(a).quantize(palette=palimg, dither=Image.Dither.NONE)).astype(np.uint8) + 1
            qi[k] = 0
            d = bytearray(e.data[:36]); W, H = w*scale, hh*scale
            struct.pack_into("<II", d, 0, W, H)
            fw, fh = struct.unpack("<II", d[24:32])
            if fw and fh: struct.pack_into("<II", d, 24, fw*scale, fh*scale)
            e.data = bytes(d) + qi.tobytes() + e.data[36+w*hh:]; e.raw = None
        pe.data = pe.data[:8] + newP.tobytes() + pe.data[8+768:]; pe.raw = None
    wad.write(outp, r, h)

if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2], int(sys.argv[3]), len(sys.argv) > 4)
