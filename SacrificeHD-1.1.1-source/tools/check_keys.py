"""Check that colour-keyed pixels (index 0 or pure black) keep their index after upscaling."""
import sys,struct,glob,os; sys.path.insert(0,os.path.dirname(__file__))
import wad
C=sys.argv[1]
def load(fn):
  r=wad.read(fn); root=r[0] if isinstance(r,tuple) else r
  return {p+'|'+e.type:e.data for p,e in root.walk() if not e.flag&1}
def palfor(D,p,tag):
  parts=p.split('/')
  for k in range(len(parts)-1,0,-1):
    q='/'.join(parts[:k])+'/'+tag+'|PALT'
    if q in D: return D[q][8:776]
tot=bad=0
for fn in sorted(glob.glob(C+'*.wad')+glob.glob(C+'local/*.wad')):
  rel=os.path.relpath(fn,C); O=load('orig/'+rel); N=load(fn)
  for k,d in N.items():
    if not k.endswith(('|TXTR','|ICON')): continue
    od=O[k]; w,h=struct.unpack('<II',od[:8]); W,H=struct.unpack('<II',d[:8]); fl=struct.unpack('<I',od[12:16])[0]
    if not fl&4 or W==w: continue
    p=k.split('|')[0]; tag=od[8:12][::-1].decode('latin1'); op=palfor(O,p,tag)
    if op is None: continue
    s=W//w; oi=od[36:36+w*h]; ni=d[36:36+W*H]
    cand=lambda i: i==0 or op[i*3:i*3+3]==b'\0\0\0'
    miss=0
    for y in range(1,h-1):
      for x in range(1,w-1):
        ix=oi[y*w+x]
        if cand(ix) and all(oi[(y+dy)*w+x+dx]==ix for dy in(-1,0,1) for dx in(-1,0,1)):
          if ni[(y*s+s//2)*W+x*s+s//2]!=ix: miss+=1
    tot+=1
    if miss: bad+=1; print(rel,p,miss)
print('keyed upscaled',tot,'with misses',bad)
