package crap

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	pty "github.com/creack/pty/v2"
)

// RunWithPTYReformat runs command in a PTY, pipes its output through
// ReformatTAP, forwards signals to the child, and returns the child's
// exit code.
func RunWithPTYReformat(ctx context.Context, command string, args []string, w io.Writer, color bool) int {
	path, err := exec.LookPath(command)
	if err != nil {
		fmt.Fprintf(os.Stderr, ":: %s: command not found\n", command)
		return 127
	}

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Cancel = func() error { return nil }

	ptmx, err := pty.Start(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, ":: failed to start %s: %v\n", command, err)
		return 126
	}
	defer ptmx.Close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigCh {
			if cmd.Process != nil {
				cmd.Process.Signal(sig)
			}
		}
	}()
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
	}()

	ReformatTAP(ptmx, w, color)

	waitErr := cmd.Wait()
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() {
					return 128 + int(status.Signal())
				}
				return status.ExitStatus()
			}
		}
		return 1
	}
	return 0
}
