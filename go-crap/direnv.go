package crap

import (
	"context"
	"fmt"
	"io"
)

// FindDirenv searches $PATH for a "direnv" binary that is not the same file as selfExe.
func FindDirenv(selfExe string) (string, error) {
	return findBinary(selfExe, "direnv")
}

// direnvAwkScript returns the embedded awk script for a recognized direnv subcommand.
func direnvAwkScript(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("no subcommand")
	}
	switch args[0] {
	case "allow", "status", "reload", "hook":
		return lookupAwkScript("direnv", args[0])
	default:
		return "", fmt.Errorf("unrecognized: %s", args[0])
	}
}

// ConvertDirenv runs direnv with args and writes CRAP-2 output.
func ConvertDirenv(ctx context.Context, selfExe string, args []string, w io.Writer, stdin io.Reader, stderrW io.Writer, verbose bool, color bool) int {
	direnvPath, err := FindDirenv(selfExe)
	if err != nil {
		fmt.Fprintf(stderrW, "::direnv: %s\n", err)
		return 1
	}

	script, err := direnvAwkScript(args)
	if err != nil {
		return execPassthrough(ctx, direnvPath, args, w, stdin, stderrW)
	}

	return convertWithAwk(ctx, direnvPath, args, w, script, color, "direnv")
}
