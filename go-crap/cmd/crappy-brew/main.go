package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	crap "github.com/amarbel-llc/crap/go-crap"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "::brew — wrap brew output in CRAP-2\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  ::brew <brew-command> [args...]\n")
		os.Exit(1)
	}

	selfExe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "::brew: failed to resolve own executable: %v\n", err)
		os.Exit(1)
	}

	color := stdoutIsTerminal()
	exitCode := crap.ConvertBrew(ctx, selfExe, os.Args[1:], os.Stdout, os.Stdin, os.Stderr, false, color)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
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
