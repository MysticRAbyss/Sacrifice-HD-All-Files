# writes versioninfo.json (goversioninfo format) for one program
import json,sys
title,version,desc,orig,out=sys.argv[1:6]
icon=sys.argv[6] if len(sys.argv)>6 else ""
manifest=sys.argv[7] if len(sys.argv)>7 else ""
v=[int(x) for x in (version.split(".")+["0","0","0"])[:4]]
fv={"Major":v[0],"Minor":v[1],"Patch":v[2],"Build":v[3]}
j={"FixedFileInfo":{"FileVersion":fv,"ProductVersion":fv,"FileFlagsMask":"3f","FileFlags":"00","FileOS":"040004","FileType":"01","FileSubType":"00"},
 "StringFileInfo":{"Comments":"Fan-made mod for Sacrifice (Shiny Entertainment, 2000). Needs the original game; includes none of its files.",
  "CompanyName":title+" mod","FileDescription":desc,"FileVersion":version,"InternalName":orig.rsplit(".",1)[0],
  "LegalCopyright":"Open-source fan mod","OriginalFilename":orig,"ProductName":title,"ProductVersion":version},
 "VarFileInfo":{"Translation":{"LangID":"0409","CharsetID":"04B0"}},
 "IconPath":icon,"ManifestPath":manifest}
json.dump(j,open(out,"w"),indent=1)
