"""Reader/writer for Sacrifice (Shiny, 2000) .wad archives.
Layout: 'WAD>' u32 dirOffset u32 dirSize u32 0 u32 1, then one zlib stream per file in
directory order, then a zlib-compressed directory of 28-byte entries:
name(4, reversed) type(4, reversed) compSize uncompSize count flag(bit0=folder, bit1=zlib-compressed; other bits kept) time.
Folders own the next `count` entries (depth first)."""
import struct, zlib, sys

def rev(b): return b[::-1].decode('latin1')

class Entry:
    def __init__(s, name, typ, csz, usz, count, flag, time):
        s.name, s.type, s.csz, s.usz, s.count, s.flag, s.time = name, typ, csz, usz, count, flag, time
        s.children = []; s.data = None; s.raw = None
    def walk(s, path=""):
        p = path + "/" + s.name
        yield p, s
        for c in s.children: yield from c.walk(p)

def read(fn, load=True):
    d = open(fn, "rb").read()
    assert d[:4] == b"WAD>", fn
    off, dsz, z0, z1 = struct.unpack("<4I", d[4:20])
    dd = zlib.decompress(d[off:])
    ents = []
    for i in range(0, len(dd), 28):
        n, t = dd[i:i+4], dd[i+4:i+8]
        csz, usz, cnt, flag, tm = struct.unpack("<5I", dd[i+8:i+28])
        ents.append(Entry(rev(n), rev(t), csz, usz, cnt, flag, tm))
    it = iter(ents)
    def build():
        e = next(it)
        if e.flag & 1:
            for _ in range(e.count): e.children.append(build())
        return e
    root = build()
    pos = 20
    for p, e in root.walk():
        if not e.flag & 1:
            e.raw = d[pos:pos+e.csz]; pos += e.csz
            if load: e.data = e.raw if not e.flag & 2 else zlib.decompress(e.raw)
    return root, (z0, z1)

def write(fn, root, hdr=(0, 1)):
    body = bytearray(); dirb = bytearray()
    for p, e in root.walk():
        if not e.flag & 1:
            if e.data is not None and e.raw is None:
                e.raw = zlib.compress(e.data, 6); e.usz = len(e.data); e.flag |= 2
            e.csz = len(e.raw); body += e.raw
    def sizes(e):
        if not e.flag & 1: return e.csz, e.usz
        c = u = 0
        for ch in e.children:
            a, b = sizes(ch); c += a; u += b
        e.csz, e.usz, e.count = c, u, len(e.children)
        return c, u
    sizes(root)
    for p, e in root.walk():
        dirb += e.name.encode('latin1')[::-1] + e.type.encode('latin1')[::-1]
        dirb += struct.pack("<5I", e.csz, e.usz, e.count, e.flag, e.time)
    zd = zlib.compress(bytes(dirb), 9)
    out = b"WAD>" + struct.pack("<4I", 20 + len(body), len(dirb), *hdr) + body + zd
    open(fn, "wb").write(out)

if __name__ == "__main__":
    for fn in sys.argv[1:]:
        root, _ = read(fn, load=False)
        for p, e in root.walk():
            print(f"{e.type:4} {e.usz:9} {p}")
