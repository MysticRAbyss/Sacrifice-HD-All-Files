module sacsetup

go 1.24

require (
	github.com/go-ole/go-ole v1.3.0
	github.com/lxn/walk v0.0.0-20210112085537-c389da54e794
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e
	golang.org/x/sys v0.1.0
)

require gopkg.in/Knetic/govaluate.v3 v3.0.0-00010101000000-000000000000 // indirect

replace golang.org/x/sys => github.com/golang/sys v0.0.0-20201018230417-eeed37f84f13

replace gopkg.in/Knetic/govaluate.v3 => github.com/Knetic/govaluate v3.0.0+incompatible
