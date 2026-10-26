// verinfo writes a Windows resource file (.syso) with an icon, a manifest and version information
// usage: verinfo <versioninfo.json> <out.syso>   (JSON in goversioninfo's format)
package main

import (
	"fmt"
	"os"

	"verinfo/goversioninfo"
)

func main() {
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	vi := &goversioninfo.VersionInfo{}
	if err := vi.ParseJSON(b); err != nil {
		panic(err)
	}
	vi.Build()
	vi.Walk()
	if err := vi.WriteSyso(os.Args[2], "amd64"); err != nil {
		panic(err)
	}
	fmt.Println("wrote", os.Args[2])
}
