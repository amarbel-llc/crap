package crap

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func findBinary(selfExe, name string) (string, error) {
	selfResolved, err := filepath.EvalSymlinks(selfExe)
	if err != nil {
		selfResolved = selfExe
	}

	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return "", fmt.Errorf("%s not found: PATH is empty", name)
	}

	for _, dir := range filepath.SplitList(pathEnv) {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		candidateResolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			candidateResolved = candidate
		}
		if candidateResolved == selfResolved {
			continue
		}
		return candidate, nil
	}

	return "", fmt.Errorf("%s not found in PATH (all candidates resolve to %s)", name, selfResolved)
}

func execPassthrough(ctx context.Context, binPath string, args []string, stdout io.Writer, stdin io.Reader, stderr io.Writer) int {
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus()
			}
		}
		return 1
	}
	return 0
}
