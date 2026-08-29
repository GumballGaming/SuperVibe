// Nested module: build-time only, keeps winres out of the app's go.mod.
module supervibe/tools/syso

go 1.27

require github.com/tc-hib/winres v0.3.1

require (
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	golang.org/x/image v0.40.0 // indirect
)
