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
func RunWithPTYReformat(ctx context.Context, command string, args []string, w io.Writer, color bool, opts ...ExecOption) int {
	cfg := applyExecOptions(opts)
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

	scanner := bufio.NewScanner(ptmx)

	// Read the first line to detect TAP output.
	var firstLine string
	if scanner.Scan() {
		firstLine = scanner.Text()
	}

	isTAP := strings.TrimSpace(firstLine) == "TAP version 14"

	if isTAP {
		return runTAPReformat(scanner, command, w, color, cfg, cmd)
	}

	return runOpaque(scanner, firstLine, command, args, w, color, cfg, cmd)
}

func runTAPReformat(scanner *bufio.Scanner, command string, w io.Writer, color bool, cfg execConfig, cmd *exec.Cmd) int {
	tw := NewColorWriter(w, color)
	if color {
		tw.EnableTTYBuildLastLine()
	}
	spinner := newStatusSpinner()
	spinner.disabled = !cfg.spinner

	var mu sync.Mutex
	var lastContent string
	stopTicker := startStatusTicker(tw, spinner, &mu, &lastContent)

	for scanner.Scan() {
		line := scanner.Text()

		mu.Lock()
		lastContent = line
		tw.clearStatusIfActive()
		reformatLine(tw.w, line, color)
		mu.Unlock()
	}

	stopTicker()
	tw.clearStatusIfActive()

	return waitExitCode(cmd)
}

// reformatLine colorizes a single TAP/CRAP line and writes it, handling
// indented subtest lines.
func reformatLine(w io.Writer, line string, color bool) {
	// Strip leading whitespace to match TAP tokens, but preserve it for output.
	trimmed := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(trimmed)]

	if strings.HasPrefix(trimmed, "TAP version ") || strings.HasPrefix(trimmed, "CRAP version ") || strings.HasPrefix(trimmed, "CRAP-2") {
		return
	}

	if m := notOkLine.FindStringSubmatchIndex(trimmed); m != nil {
		rest := trimmed[m[4]:m[5]]
		rest = colorizeDirective(rest, todoDir, "# TODO", color, ansiYellow)
		rest = colorizeDirective(rest, warnDir, "# WARN", color, ansiYellow)
		fmt.Fprintf(w, "%s%s%s\n", indent, colorToken("not ok", color, ansiRed), rest)
	} else if m := okLine.FindStringSubmatchIndex(trimmed); m != nil {
		rest := trimmed[m[4]:m[5]]
		rest = colorizeDirective(rest, skipDir, "# SKIP", color, ansiYellow)
		rest = colorizeDirective(rest, warnDir, "# WARN", color, ansiYellow)
		fmt.Fprintf(w, "%s%s%s\n", indent, colorToken("ok", color, ansiGreen), rest)
	} else if m := bailOutLine.FindStringSubmatchIndex(trimmed); m != nil {
		rest := trimmed[m[4]:m[5]]
		fmt.Fprintf(w, "%s%s%s\n", indent, colorToken("Bail out!", color, ansiRed), rest)
	} else if color && strings.HasPrefix(trimmed, "#") {
		fmt.Fprintf(w, "%s%s%s%s\n", ansiDim, indent, trimmed, ansiReset)
	} else {
		fmt.Fprintln(w, line)
	}
}

func waitExitCode(cmd *exec.Cmd) int {
	waitErr := cmd.Wait()
	if waitErr == nil {
		return 0
	}
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

func runOpaque(scanner *bufio.Scanner, firstLine, command string, args []string, w io.Writer, color bool, cfg execConfig, cmd *exec.Cmd) int {
	tw := NewColorWriter(w, color)
	if color {
		tw.EnableTTYBuildLastLine()
	}
	spinner := newStatusSpinner()
	spinner.disabled = !cfg.spinner

	desc := command
	if len(args) > 0 {
		desc = command + " " + strings.Join(args, " ")
	}

	var mu sync.Mutex
	var lastContent string
	stopTicker := startStatusTicker(tw, spinner, &mu, &lastContent)

	tw.StartTestPoint(desc)

	if firstLine != "" {
		mu.Lock()
		lastContent = firstLine
		tw.UpdateLastLine(firstLine)
		mu.Unlock()
	}

	for scanner.Scan() {
		line := scanner.Text()
		mu.Lock()
		lastContent = line
		tw.UpdateLastLine(line)
		mu.Unlock()
	}

	stopTicker()
	tw.FinishLastLine()

	exitCode := waitExitCode(cmd)

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
