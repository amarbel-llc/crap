package crap

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

func convertWithPhases(ctx context.Context, binPath string, args []string, w io.Writer, parser *PhaseParser, color bool, toolName string) int {
	tw := NewColorWriter(w, color)
	if color {
		tw.EnableTTYBuildLastLine()
	}
	spinner := newStatusSpinner()

	cmd := exec.CommandContext(ctx, binPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		tw.BailOut(fmt.Sprintf("failed to create stdout pipe: %v", err))
		return 1
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		tw.BailOut(fmt.Sprintf("failed to create stderr pipe: %v", err))
		return 1
	}

	if err := cmd.Start(); err != nil {
		tw.BailOut(fmt.Sprintf("failed to start %s: %v", toolName, err))
		return 1
	}

	var mu sync.Mutex
	var lastContent string
	stopTicker := startStatusTicker(tw, spinner, &mu, &lastContent)

	var lines []string
	var linesMu sync.Mutex

	onLine := func(line string) {
		linesMu.Lock()
		lines = append(lines, line)
		linesMu.Unlock()
		mu.Lock()
		lastContent = line
		spinner.Touch()
		tw.UpdateLastLine(spinner.prefix() + line)
		mu.Unlock()
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			onLine(scanner.Text())
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			onLine(scanner.Text())
		}
	}()
	wg.Wait()

	cmdErr := cmd.Wait()
	stopTicker()
	tw.FinishLastLine()

	exitCode := 0
	if cmdErr != nil {
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
			} else {
				exitCode = 1
			}
		} else {
			exitCode = 1
		}
	}

	emitPhases(tw, parser, lines, exitCode, toolName)
	tw.Plan()
	return exitCode
}

func emitPhases(tw *Writer, parser *PhaseParser, lines []string, exitCode int, toolName string) {
	if exitCode != 0 {
		desc := toolName + " " + parser.phaseOrder[0]
		combined := strings.Join(lines, "\n")
		diag := map[string]string{
			"exit-code": fmt.Sprintf("%d", exitCode),
		}
		if combined != "" {
			diag["output"] = combined
		}
		tw.NotOk(desc, diag)
		return
	}

	for _, line := range lines {
		parser.Feed(line)
	}

	for _, phase := range parser.Phases() {
		tw.Ok(phase.Name)
	}
}
