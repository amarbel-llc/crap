// Command large-colon (invoked as `::`) is the CRAP-2 toolkit. Every
// subcommand is an ndjson-crap producer that writes the canonical wire
// format (see go-crap/ndjsoncrap) to stdout; presentation is the viewport's
// job via `:: present` / the crap-present binary:
//
//	:: go-test ./... | crap-present
//	:: go-test ./... | ::  present     # equivalent
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"

	crap "github.com/amarbel-llc/crap/go-crap"
	"github.com/amarbel-llc/crap/go-crap/ndjsoncrap"
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// No subcommand: read ndjson-crap on stdin and present it.
	if len(os.Args) < 2 {
		os.Exit(handlePresent(ctx, nil))
	}

	switch os.Args[1] {
	case "present", "reformat":
		os.Exit(handlePresent(ctx, os.Args[2:]))
	case "go-test":
		os.Exit(handleGoTest(ctx, os.Args[2:]))
	case "cargo-test":
		os.Exit(handleCargoTest(ctx, os.Args[2:]))
	case "exec":
		os.Exit(handleExec(ctx, os.Args[2:]))
	case "validate":
		os.Exit(handleValidate())
	case "version", "--version", "-v":
		fmt.Printf(":: %s+%s\n", version, commit)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q (try `:: help`)\n", os.Args[1])
		os.Exit(64)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, ":: — CRAP-2 toolkit (ndjson-crap producers + viewport)\n\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  ::                        Read ndjson-crap on stdin and render via the viewport\n")
	fmt.Fprintf(os.Stderr, "  :: present [flags]        Same as above (also: reformat)\n")
	fmt.Fprintf(os.Stderr, "  :: go-test [args...]      Run `go test -json` and emit ndjson-crap\n")
	fmt.Fprintf(os.Stderr, "  :: cargo-test [args...]   Run `cargo test` and emit ndjson-crap\n")
	fmt.Fprintf(os.Stderr, "  :: exec <cmd> [args...]   Run a command and emit ndjson-crap (execution records)\n")
	fmt.Fprintf(os.Stderr, "  :: validate               Validate an ndjson-crap stream on stdin\n")
	fmt.Fprintf(os.Stderr, "  :: version                Print version and commit\n")
}

// handlePresent delegates to the standalone crap-present binary. The
// presenter pulls in bubbletea, whose init() probes the terminal (OSC 11)
// for any process that imports it; keeping that out of the general-purpose
// `::` binary means delegating rather than importing. crap-present is
// resolved from PATH or alongside this executable (they ship together).
func handlePresent(ctx context.Context, args []string) int {
	bin, err := resolvePresentBin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "present: %v\n", err)
		return 1
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "present: %v\n", err)
		return 1
	}
	return 0
}

func resolvePresentBin() (string, error) {
	if p, err := exec.LookPath("crap-present"); err == nil {
		return p, nil
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "crap-present")
		if st, statErr := os.Stat(cand); statErr == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("crap-present not found on PATH or alongside %s", filepath.Base(os.Args[0]))
}

func handleGoTest(ctx context.Context, args []string) int {
	goArgs := append([]string{"test", "-json"}, args...)
	cmd := exec.CommandContext(ctx, "go", goArgs...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return bailout(os.Stdout, fmt.Sprintf("creating stdout pipe: %v", err))
	}
	if err := cmd.Start(); err != nil {
		return bailout(os.Stdout, fmt.Sprintf("failed to start go test: %v", err))
	}
	code := crap.ConvertGoTest(stdout, os.Stdout)
	_ = cmd.Wait()
	return code
}

func handleCargoTest(ctx context.Context, args []string) int {
	cargoArgs := append([]string{"test"}, args...)
	cmd := exec.CommandContext(ctx, "cargo", cargoArgs...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return bailout(os.Stdout, fmt.Sprintf("creating stdout pipe: %v", err))
	}
	if err := cmd.Start(); err != nil {
		return bailout(os.Stdout, fmt.Sprintf("failed to start cargo test: %v", err))
	}
	code := crap.ConvertCargoTest(stdout, os.Stdout)
	_ = cmd.Wait()
	return code
}

func handleExec(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "exec: missing command\n")
		return 64
	}
	return crap.ConvertExec(ctx, args[0], args[1:], os.Stdout)
}

// handleValidate reads an ndjson-crap stream on stdin and reports any
// undecodable records. Returns 0 when every record decodes, 1 otherwise.
func handleValidate() int {
	r := ndjsoncrap.NewReader(os.Stdin)
	records, bad := 0, 0
	for {
		_, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			bad++
			fmt.Fprintf(os.Stderr, "invalid: %v\n", err)
			continue
		}
		records++
	}
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	if bad == 0 {
		fmt.Fprintf(out, "valid: %d record(s)\n", records)
		return 0
	}
	fmt.Fprintf(out, "invalid: %d record(s) ok, %d undecodable\n", records, bad)
	return 1
}

func bailout(w io.Writer, msg string) int {
	_ = ndjsoncrap.NewWriter(w).Write(ndjsoncrap.Bailout{Message: msg})
	return 2
}
