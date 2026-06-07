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
	"os"

	"github.com/amarbel-llc/crap/go-crap/presentcli"
)

func main() {
	os.Exit(presentcli.Run(os.Args[1:], os.Stdin, os.Stderr))
}
