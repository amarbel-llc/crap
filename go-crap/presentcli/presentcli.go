// Package presentcli is the shared command glue for the ndjson-crap
// presenter, used by both the `:: present` subcommand and the standalone
// `crap-present` binary. It reads ndjson-crap on stdin and renders it via
// the viewport on a terminal (or a plain fallback when not a terminal).
package presentcli

import (
	"fmt"
	"io"
	"os"

	"github.com/amarbel-llc/crap/go-crap/v2/viewport"
)

// Usage is the one-line help text shared by both entry points.
const Usage = "present [--title <s>] [--tail <n>]   Read ndjson-crap on stdin and render via the viewport"

// Run parses args, reads ndjson-crap from in, and renders to ttyOut (the
// terminal, conventionally stderr). It returns a process exit code.
func Run(args []string, in io.Reader, ttyOut *os.File) int {
	var (
		title string
		tail  int
	)
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--title":
			if i+1 < len(args) {
				i++
				title = args[i]
			}
		case "--tail":
			if i+1 < len(args) {
				i++
				if n, err := parseInt(args[i]); err == nil {
					tail = n
				}
			}
		case "-h", "--help":
			fmt.Fprintf(os.Stderr, "Usage: %s\n", Usage)
			return 0
		default:
			fmt.Fprintf(os.Stderr, "present: unknown argument %q\n", a)
			return 64 // EX_USAGE
		}
	}

	err := viewport.Present(in, viewport.Options{
		Title:     title,
		TailLines: tail,
		Out:       ttyOut,
		IsTTY:     isTerminal(ttyOut),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "present: %v\n", err)
		return 1
	}
	return 0
}

// isTerminal reports whether f is an interactive terminal. NO_COLOR forces
// the plain, non-interactive fallback (matching large-colon's convention).
func isTerminal(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func parseInt(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
