package crap

import (
	"context"
	"fmt"
	"io"
)

// FindGit searches $PATH for a "git" binary that is not the same file as
// selfExe. This prevents infinite recursion when a user symlinks or renames
// ::git to "git". Returns the absolute path to git, or an error if no
// suitable git is found.
func FindGit(selfExe string) (string, error) {
	return findBinary(selfExe, "git")
}

// gitAwkScript returns the embedded awk script for a recognized git subcommand.
func gitAwkScript(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("no subcommand")
	}
	switch args[0] {
	case "pull", "push", "clone", "fetch", "rebase":
		return lookupAwkScript("git", args[0])
	default:
		return "", fmt.Errorf("unrecognized: %s", args[0])
	}
}

// ConvertGit runs git with args and writes CRAP-2 output. For recognized
// subcommands (pull, push, clone, fetch, rebase) it pipes output through
// an embedded awk script and emits CRAP-2. For all others it passes through
// stdin/stdout/stderr directly to git.
func ConvertGit(ctx context.Context, selfExe string, args []string, w io.Writer, stdin io.Reader, stderrW io.Writer, verbose bool, color bool) int {
	gitPath, err := FindGit(selfExe)
	if err != nil {
		fmt.Fprintf(stderrW, "::git: %s\n", err)
		return 1
	}

	script, err := gitAwkScript(args)
	if err != nil {
		return execPassthrough(ctx, gitPath, args, w, stdin, stderrW)
	}

	return convertWithAwk(ctx, gitPath, args, w, script, color, "git")
}
