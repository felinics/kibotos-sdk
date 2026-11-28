module github.com/felinics/kibotos-sdk/cli

go 1.24

require (
	github.com/coder/websocket v1.8.15
	github.com/fatih/color v1.18.0
	github.com/felinics/kibotos-sdk/sdks/go v0.0.0
	github.com/mattn/go-isatty v0.0.20
	github.com/spf13/cobra v1.9.1
	golang.org/x/sys v0.30.0
	golang.org/x/term v0.29.0
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
)

replace github.com/felinics/kibotos-sdk/sdks/go => ../sdks/go
