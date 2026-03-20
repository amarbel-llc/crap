package crap

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	pty "github.com/creack/pty/v2"
)

// RunWithPTYReformat runs command in a PTY, streams its output through a
// CRAP-2 writer with status line and spinner, and returns the child's exit
// code.
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

	tw := NewColorWriter(w, color)
	if color {
		tw.EnableTTYBuildLastLine()
	}
	spinner := newStatusSpinner()

	desc := command
	if len(args) > 0 {
		desc = command + " " + strings.Join(args, " ")
	}

	var mu sync.Mutex
	var lastContent string
	stopTicker := startStatusTicker(tw, spinner, &mu, &lastContent)

	tw.StartTestPoint(desc)

	scanner := bufio.NewScanner(ptmx)
	for scanner.Scan() {
		line := scanner.Text()
		mu.Lock()
		lastContent = line
		tw.UpdateLastLine(line)
		mu.Unlock()
	}

	stopTicker()
	tw.FinishLastLine()

	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() {
					exitCode = 128 + int(status.Signal())
				} else {
					exitCode = status.ExitStatus()
				}
			} else {
				exitCode = 1
			}
		} else {
			exitCode = 1
		}
	}

	if color {
		tw.FinishInProgress(exitCode == 0)
	} else if exitCode == 0 {
		tw.Ok(desc)
	} else {
		tw.NotOk(desc, nil)
	}
	tw.Plan()

	return exitCode
}
