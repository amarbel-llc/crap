package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"

	crap "github.com/amarbel-llc/crap/go-crap"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if len(os.Args) < 2 {
		crap.ReformatTAP(os.Stdin, os.Stdout, stdoutIsTerminal())
		return
	}

	var err error
	switch os.Args[1] {
	case "validate":
		err = handleValidate()
	case "go-test":
		err = handleGoTest(ctx)
	case "cargo-test":
		err = handleCargoTest(ctx)
	case "reformat":
		err = handleReformat()
	case "exec":
		err = handleExec(ctx)
	case "exec-parallel":
		err = handleExecParallel(ctx)
	case "help", "-h", "--help":
		printUsage()
	default:
		command := os.Args[1]
		args := os.Args[2:]
		color := stdoutIsTerminal()
		exitCode := crap.RunWithPTYReformat(ctx, command, args, os.Stdout, color)
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, ":: — CRAP-2 toolkit\n\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  ::                        Read CRAP from stdin and emit CRAP-2\n")
	fmt.Fprintf(os.Stderr, "  :: <cmd> [args...]         Run cmd in a PTY and reformat as CRAP-2\n")
	fmt.Fprintf(os.Stderr, "  :: reformat               Read CRAP from stdin and emit CRAP-2\n")
	fmt.Fprintf(os.Stderr, "  :: validate [flags]       Validate CRAP-2 input\n")
	fmt.Fprintf(os.Stderr, "  :: go-test [args...]      Run go test and convert to CRAP-2\n")
	fmt.Fprintf(os.Stderr, "  :: cargo-test [args...]   Run cargo test and convert to CRAP-2\n")
	fmt.Fprintf(os.Stderr, "  :: exec <cmd> [args...]   Run cmd sequentially and emit CRAP-2\n")
	fmt.Fprintf(os.Stderr, "  :: exec-parallel          Run commands in parallel and emit CRAP-2\n")
}

func stdoutIsTerminal() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	stat, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func parseFlags(args []string, boolFlags, valueFlags map[string]*string) []string {
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if ptr, ok := boolFlags[a]; ok {
			*ptr = "true"
			continue
		}
		if ptr, ok := valueFlags[a]; ok && i+1 < len(args) {
			i++
			*ptr = args[i]
			continue
		}
		rest = append(rest, a)
	}
	return rest
}

func handleValidate() error {
	args := os.Args[2:]
	var format, strict, inputFlag string
	rest := parseFlags(args, map[string]*string{
		"--strict": &strict,
	}, map[string]*string{
		"--format": &format,
		"--input":  &inputFlag,
	})
	_ = rest

	if format == "" {
		format = "text"
	}
	switch format {
	case "text", "json", "tap":
	default:
		return fmt.Errorf("invalid format: %s (must be text, json, or tap)", format)
	}

	var input io.Reader
	if inputFlag != "" {
		input = strings.NewReader(inputFlag)
	} else {
		input = os.Stdin
	}

	reader := crap.NewReader(input)
	diags := reader.Diagnostics()
	summary := reader.Summary()

	switch format {
	case "json":
		result := map[string]any{
			"summary":     summary,
			"diagnostics": diags,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)

	case "tap":
		tw := crap.NewWriter(os.Stdout)
		for _, d := range diags {
			desc := fmt.Sprintf("[%s] %s", d.Rule, d.Message)
			if d.Severity == crap.SeverityError {
				tw.NotOk(desc, map[string]string{
					"line":     fmt.Sprintf("%d", d.Line),
					"severity": d.Severity.String(),
					"rule":     d.Rule,
				})
			} else {
				tw.Ok(desc)
			}
		}
		if summary.Valid {
			tw.Ok(fmt.Sprintf("CRAP stream valid: %d tests", summary.TotalTests))
		} else {
			tw.NotOk(fmt.Sprintf("CRAP stream invalid: %d tests", summary.TotalTests), map[string]string{
				"passed":  fmt.Sprintf("%d", summary.Passed),
				"failed":  fmt.Sprintf("%d", summary.Failed),
				"skipped": fmt.Sprintf("%d", summary.Skipped),
				"todo":    fmt.Sprintf("%d", summary.Todo),
			})
		}
		tw.Plan()
		if strict == "true" && !summary.Valid {
			os.Exit(1)
		}

	default:
		for _, d := range diags {
			fmt.Printf("line %d: %s: [%s] %s\n", d.Line, d.Severity, d.Rule, d.Message)
		}
		status := "valid"
		if !summary.Valid {
			status = "invalid"
		}
		fmt.Printf("\n%s: %d tests (%d passed, %d failed, %d skipped, %d todo)\n",
			status, summary.TotalTests, summary.Passed, summary.Failed, summary.Skipped, summary.Todo)
		if strict == "true" && !summary.Valid {
			os.Exit(1)
		}
	}

	return nil
}

func handleGoTest(ctx context.Context) error {
	args := os.Args[2:]
	var verbose, skipEmpty string
	rest := parseFlags(args, map[string]*string{
		"-v": &verbose, "--verbose": &verbose,
		"--skip-empty": &skipEmpty,
	}, nil)

	goTestArgs := []string{"test", "-json"}
	if verbose == "true" {
		goTestArgs = append(goTestArgs, "-v")
	}
	goTestArgs = append(goTestArgs, rest...)

	cmd := exec.CommandContext(ctx, "go", goTestArgs...)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	color := stdoutIsTerminal()

	if err := cmd.Start(); err != nil {
		tw := crap.NewColorWriter(os.Stdout, color)
		tw.BailOut(fmt.Sprintf("failed to start go test: %v", err))
		return err
	}

	exitCode := crap.ConvertGoTest(stdout, os.Stdout, verbose == "true", skipEmpty == "true", color)
	cmd.Wait()

	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

func handleCargoTest(ctx context.Context) error {
	args := os.Args[2:]
	var verbose, skipEmpty string
	rest := parseFlags(args, map[string]*string{
		"-v": &verbose, "--verbose": &verbose,
		"--skip-empty": &skipEmpty,
	}, nil)

	cargoArgs := []string{"test"}
	if verbose == "true" {
		cargoArgs = append(cargoArgs, "-v")
	}
	cargoArgs = append(cargoArgs, rest...)

	cmd := exec.CommandContext(ctx, "cargo", cargoArgs...)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	color := stdoutIsTerminal()

	if err := cmd.Start(); err != nil {
		tw := crap.NewColorWriter(os.Stdout, color)
		tw.BailOut(fmt.Sprintf("failed to start cargo test: %v", err))
		return err
	}

	exitCode := crap.ConvertCargoTest(stdout, os.Stdout, verbose == "true", skipEmpty == "true", color)
	cmdErr := cmd.Wait()

	if cmdErr != nil && exitCode == 0 {
		tw := crap.NewColorWriter(os.Stdout, color)
		msg := strings.TrimSpace(stderrBuf.String())
		if msg == "" {
			msg = cmdErr.Error()
		}
		tw.BailOut(fmt.Sprintf("cargo test failed: %s", msg))
		os.Exit(1)
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

func handleReformat() error {
	crap.ReformatTAP(os.Stdin, os.Stdout, stdoutIsTerminal())
	return nil
}

func handleExec(ctx context.Context) error {
	args := os.Args[2:]
	var verbose, noSpinner string
	rest := parseFlags(args, map[string]*string{
		"-v": &verbose, "--verbose": &verbose,
		"--no-spinner": &noSpinner,
	}, nil)

	if len(rest) == 0 {
		return fmt.Errorf("missing command\nusage: :: exec [--verbose] [--no-spinner] <cmd> [<arg1> <arg2> ...]")
	}

	utility := rest[0]
	execArgs := rest[1:]
	color := stdoutIsTerminal()
	exitCode := crap.ConvertExec(ctx, utility, execArgs, os.Stdout, verbose == "true", color, crap.WithSpinner(noSpinner != "true"))

	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

func handleExecParallel(ctx context.Context) error {
	args := os.Args[2:]
	var verbose, noSpinner, jobsStr string
	rest := parseFlags(args, map[string]*string{
		"-v": &verbose, "--verbose": &verbose,
		"--no-spinner": &noSpinner,
	}, map[string]*string{
		"-j": &jobsStr, "--jobs": &jobsStr,
	})

	maxJobs := 0
	if jobsStr != "" {
		if v, err := strconv.Atoi(jobsStr); err == nil {
			maxJobs = v
		}
	}

	sepIdx := -1
	for i, a := range rest {
		if a == ":::" {
			sepIdx = i
			break
		}
	}

	if sepIdx < 0 {
		return fmt.Errorf("missing ::: separator\nusage: :: exec-parallel [--verbose] <template> ::: <arg1> <arg2> ...")
	}
	if sepIdx == 0 {
		return fmt.Errorf("missing command template before :::\nusage: :: exec-parallel [--verbose] <template> ::: <arg1> <arg2> ...")
	}

	template := strings.Join(rest[:sepIdx], " ")
	execArgs := rest[sepIdx+1:]

	if len(execArgs) == 0 {
		return fmt.Errorf("no arguments after :::\nusage: :: exec-parallel [--verbose] <template> ::: <arg1> <arg2> ...")
	}

	color := stdoutIsTerminal()
	executor := &crap.GoroutineExecutor{MaxJobs: maxJobs}

	var exitCode int
	if color {
		exitCode = crap.ConvertExecParallelWithStatus(ctx, executor, template, execArgs, os.Stdout, verbose == "true", color, crap.WithSpinner(noSpinner != "true"))
	} else {
		results := executor.Run(ctx, template, execArgs)
		exitCode = crap.ConvertExecParallel(results, os.Stdout, verbose == "true", color)
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}
