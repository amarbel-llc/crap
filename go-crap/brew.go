package crap

import (
	"context"
	"fmt"
	"io"
)

// FindBrew searches $PATH for a "brew" binary that is not the same file as selfExe.
func FindBrew(selfExe string) (string, error) {
	return findBinary(selfExe, "brew")
}

// brewAwkScript returns the embedded awk script for a recognized brew subcommand.
func brewAwkScript(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("no subcommand")
	}
	switch args[0] {
	case "install", "upgrade", "update", "tap":
		return lookupAwkScript("brew", args[0])
	default:
		return "", fmt.Errorf("unrecognized: %s", args[0])
	}
}

// ConvertBrew runs brew with args and writes CRAP-2 output.
func ConvertBrew(ctx context.Context, selfExe string, args []string, w io.Writer, stdin io.Reader, stderrW io.Writer, verbose bool, color bool) int {
	brewPath, err := FindBrew(selfExe)
	if err != nil {
		fmt.Fprintf(stderrW, "::brew: %s\n", err)
		return 1
	}

	script, err := brewAwkScript(args)
	if err != nil {
		return execPassthrough(ctx, brewPath, args, w, stdin, stderrW)
	}

	return convertWithAwk(ctx, brewPath, args, w, script, color, "brew")
}
