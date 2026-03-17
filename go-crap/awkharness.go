package crap

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/benhoyt/goawk/interp"
	"github.com/benhoyt/goawk/parser"
)

//go:embed awk/git/*.awk
var gitAwkScripts embed.FS

//go:embed awk/brew/*.awk
var brewAwkScripts embed.FS

// lookupAwkScript reads an embedded awk script for a tool and subcommand.
func lookupAwkScript(tool, subcommand string) (string, error) {
	var fs embed.FS
	switch tool {
	case "git":
		fs = gitAwkScripts
	case "brew":
		fs = brewAwkScripts
	default:
		return "", fmt.Errorf("unknown tool: %s", tool)
	}
	path := fmt.Sprintf("awk/%s/%s.awk", tool, subcommand)
	data, err := fs.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("no awk script for %s %s: %w", tool, subcommand, err)
	}
	return string(data), nil
}

// convertTAPToCRAP is a simple non-streaming TAP to CRAP-2 converter for testing.
// It skips TAP version lines, bare comments, and indented subtest content.
func convertTAPToCRAP(r io.Reader, w io.Writer, color bool) {
	tw := NewColorWriter(w, color)
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip TAP version lines
		if strings.HasPrefix(trimmed, "TAP version") {
			continue
		}

		// Determine depth by counting leading spaces
		indent := len(line) - len(strings.TrimLeft(line, " "))
		depth := indent / 4

		// Skip indented lines (subtests)
		if depth > 0 {
			continue
		}

		// Skip bare comment lines (status lines in streaming mode)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Handle plan lines
		if strings.HasPrefix(trimmed, "1..") {
			tw.Plan()
			continue
		}

		// Handle test points
		if strings.HasPrefix(trimmed, "ok ") || strings.HasPrefix(trimmed, "not ok ") {
			tp, _ := parseTestPoint(trimmed)
			if tp.OK {
				tw.Ok(tp.Description)
			} else {
				tw.NotOk(tp.Description, nil)
			}
		}
	}
}

// convertWithAwk runs a command, pipes its output through an embedded awk
// script using goawk, reads the resulting TAP, and emits CRAP-2 with
// spinner and status line support.
func convertWithAwk(ctx context.Context, binPath string, args []string, w io.Writer, awkScript string, color bool, toolName string) int {
	tw := NewColorWriter(w, color)
	if color {
		tw.EnableTTYBuildLastLine()
	}
	spinner := newStatusSpinner()

	// Parse the awk script
	prog, err := parser.ParseProgram([]byte(awkScript), nil)
	if err != nil {
		tw.BailOut(fmt.Sprintf("failed to parse awk script: %v", err))
		return 1
	}

	// Start the real command
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmdStdout, err := cmd.StdoutPipe()
	if err != nil {
		tw.BailOut(fmt.Sprintf("failed to create stdout pipe: %v", err))
		return 1
	}
	cmdStderr, err := cmd.StderrPipe()
	if err != nil {
		tw.BailOut(fmt.Sprintf("failed to create stderr pipe: %v", err))
		return 1
	}

	if err := cmd.Start(); err != nil {
		tw.BailOut(fmt.Sprintf("failed to start %s: %v", toolName, err))
		return 1
	}

	// Pipe: command stdout+stderr → goawk stdin → goawk stdout (TAP) → scanner
	awkIn, awkInWriter := io.Pipe()
	awkOut, awkOutWriter := io.Pipe()

	// Merge command stdout+stderr into awk input pipe
	var mergeWg sync.WaitGroup
	mergeWg.Add(2)
	go func() {
		defer mergeWg.Done()
		io.Copy(awkInWriter, cmdStdout)
	}()
	go func() {
		defer mergeWg.Done()
		io.Copy(awkInWriter, cmdStderr)
	}()
	go func() {
		mergeWg.Wait()
		awkInWriter.Close()
	}()

	// Run goawk in a goroutine
	go func() {
		interp.ExecProgram(prog, &interp.Config{
			Stdin:  awkIn,
			Output: awkOutWriter,
		})
		awkOutWriter.Close()
	}()

	// Start status ticker
	var mu sync.Mutex
	var lastContent string
	stopTicker := startStatusTicker(tw, spinner, &mu, &lastContent)

	// Read TAP lines from goawk's output
	scanner := bufio.NewScanner(awkOut)
	hasTestPoints := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip TAP version lines
		if strings.HasPrefix(trimmed, "TAP version") {
			continue
		}

		// Determine depth by counting leading spaces
		indent := len(line) - len(strings.TrimLeft(line, " "))
		depth := indent / 4

		// Handle bare comments (depth 0) as status line updates
		if depth == 0 && strings.HasPrefix(trimmed, "#") {
			comment := strings.TrimPrefix(trimmed, "#")
			comment = strings.TrimSpace(comment)
			mu.Lock()
			lastContent = comment
			spinner.Touch()
			tw.UpdateLastLine(spinner.prefix() + comment)
			mu.Unlock()
			continue
		}

		// Skip indented comments (subtest status lines)
		if depth > 0 && strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Handle test points at depth 0
		if depth == 0 && (strings.HasPrefix(trimmed, "ok ") || strings.HasPrefix(trimmed, "not ok ")) {
			tp, _ := parseTestPoint(trimmed)
			mu.Lock()
			tw.FinishLastLine()
			if tp.OK {
				tw.Ok(tp.Description)
			} else {
				tw.NotOk(tp.Description, nil)
			}
			hasTestPoints = true
			mu.Unlock()
			continue
		}

		// Handle plan at depth 0
		if depth == 0 && strings.HasPrefix(trimmed, "1..") {
			mu.Lock()
			tw.FinishLastLine()
			tw.Plan()
			mu.Unlock()
		}
	}

	// Stop ticker and finish status line
	stopTicker()
	tw.FinishLastLine()

	// Wait for command to finish
	cmdErr := cmd.Wait()

	// Extract exit code from cmd
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

	// If command failed and we haven't shown failures, emit a test point
	if exitCode != 0 && !tw.HasFailures() {
		diag := map[string]string{
			"exit-code": fmt.Sprintf("%d", exitCode),
		}
		if hasTestPoints {
			tw.NotOk(toolName+" failed", diag)
		} else {
			tw.NotOk(toolName+" "+strings.Join(args, " "), diag)
		}
		tw.Plan()
	}

	return exitCode
}
