// Command crap-present reads ndjson-crap on stdin and renders it with the
// CRAP-2 viewport on the terminal (stderr). It is the standalone presenter
// for subprocess-pipe consumers (e.g. just-us --events-fd, piggy) that
// cannot import the Go library directly:
//
//	just --events-fd 1 build | crap-present --title build
//
// On a terminal it shows a live spinner + rolling tail + progress and
// persists a verdict line per test/node; off a terminal it prints a plain
// verdict-per-line fallback.
package main

import (
	"fmt"
	"os"

	"github.com/amarbel-llc/crap/go-crap/v2/presentcli"
)

// version and commit are injected at release build time by the
// amarbel-llc/nixpkgs fork's buildGoApplication (-X main.version from
// version.env, -X main.commit from the flake rev). A plain `go build`
// reports "dev"/"unknown" — the intended dev behavior.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("crap-present %s+%s\n", version, commit)
		os.Exit(0)
	}
	os.Exit(presentcli.Run(os.Args[1:], os.Stdin, os.Stderr))
}
