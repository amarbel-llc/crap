package crap

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

// GitPhase holds lines accumulated during one semantic phase of a git command.
type GitPhase struct {
	Name  string
	Lines []string
}

// GitPhaseParser classifies git output lines into named phases and accumulates
// them in order, skipping phases that receive no lines.
type GitPhaseParser struct {
	phaseOrder []string
	classify   func(string) string
	current    string
	phases     map[string]*GitPhase
	order      []string
}

// NewGitPullParser returns a parser for git pull output phases:
// fetch, unpack, merge, summary.
func NewGitPullParser() *GitPhaseParser {
	return &GitPhaseParser{
		phaseOrder: []string{"fetch", "unpack", "merge", "summary"},
		classify:   classifyPullLine,
		phases:     make(map[string]*GitPhase),
	}
}

// NewGitPushParser returns a parser for git push output phases:
// pack, transfer, summary.
func NewGitPushParser() *GitPhaseParser {
	return &GitPhaseParser{
		phaseOrder: []string{"pack", "transfer", "summary"},
		classify:   classifyPushLine,
		phases:     make(map[string]*GitPhase),
	}
}

// Classify returns the phase name for a line without accumulating it.
func (p *GitPhaseParser) Classify(line string) string {
	return p.classify(line)
}

// Feed classifies a line and appends it to the appropriate phase.
func (p *GitPhaseParser) Feed(line string) string {
	phase := p.classify(line)
	if phase == "" {
		return ""
	}
	if _, ok := p.phases[phase]; !ok {
		p.phases[phase] = &GitPhase{Name: phase}
		p.order = append(p.order, phase)
	}
	p.phases[phase].Lines = append(p.phases[phase].Lines, line)
	p.current = phase
	return phase
}

// Phases returns accumulated phases in the order defined by the parser,
// skipping any phase that received no lines.
func (p *GitPhaseParser) Phases() []GitPhase {
	var result []GitPhase
	for _, name := range p.phaseOrder {
		if ph, ok := p.phases[name]; ok {
			result = append(result, *ph)
		}
	}
	return result
}

// parserForSubcommand returns a phase parser for recognized git subcommands,
// or nil for generic fallback.
func parserForSubcommand(args []string) *GitPhaseParser {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "pull":
		return NewGitPullParser()
	case "push":
		return NewGitPushParser()
	default:
		return nil
	}
}

// ConvertGit runs git with args and writes CRAP-2 output. For recognized
// subcommands (pull, push) it emits semantic phase test points. For all
// others it emits a single test point based on exit code.
// Returns the git exit code.
func ConvertGit(ctx context.Context, args []string, w io.Writer, verbose bool, color bool) int {
	tw := NewColorWriter(w, color)
	if color {
		tw.EnableTTYBuildLastLine()
	}
	spinner := newStatusSpinner()

	cmd := exec.CommandContext(ctx, "git", args...)

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
		tw.BailOut(fmt.Sprintf("failed to start git: %v", err))
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

	parser := parserForSubcommand(args)
	if parser != nil {
		emitGitPhases(tw, parser, lines, exitCode)
	} else {
		emitGitGeneric(tw, args, lines, exitCode)
	}

	tw.Plan()
	return exitCode
}

// emitGitPhases feeds lines through a phase parser and emits one test point
// per non-empty phase. On non-zero exit, emits a single not ok instead.
func emitGitPhases(tw *Writer, parser *GitPhaseParser, lines []string, exitCode int) {
	if exitCode != 0 {
		desc := "git " + parser.phaseOrder[0]
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

// emitGitGeneric emits a single test point for an unrecognized git command.
func emitGitGeneric(tw *Writer, args []string, lines []string, exitCode int) {
	desc := "git " + strings.Join(args, " ")
	if exitCode == 0 {
		tw.Ok(desc)
	} else {
		combined := strings.Join(lines, "\n")
		diag := map[string]string{
			"exit-code": fmt.Sprintf("%d", exitCode),
		}
		if combined != "" {
			diag["output"] = combined
		}
		tw.NotOk(desc, diag)
	}
}

func classifyPullLine(line string) string {
	trimmed := strings.TrimSpace(line)

	// fetch phase
	if strings.HasPrefix(trimmed, "remote: ") ||
		strings.HasPrefix(trimmed, "Receiving objects:") ||
		strings.HasPrefix(trimmed, "Resolving deltas:") ||
		strings.HasPrefix(trimmed, "From ") ||
		(len(line) > 0 && line[0] == ' ' && strings.Contains(trimmed, "->") && strings.Contains(trimmed, "origin/")) {
		return "fetch"
	}

	// unpack phase
	if strings.HasPrefix(trimmed, "Unpacking objects:") {
		return "unpack"
	}

	// merge phase
	if strings.HasPrefix(trimmed, "Updating ") ||
		trimmed == "Fast-forward" ||
		trimmed == "Already up to date." ||
		strings.HasPrefix(trimmed, "Merge made by") {
		return "merge"
	}

	// summary phase — diffstat lines and file-change summary
	if (len(line) > 0 && line[0] == ' ' && strings.Contains(trimmed, "|")) ||
		strings.Contains(trimmed, "file changed") ||
		strings.Contains(trimmed, "files changed") ||
		strings.Contains(trimmed, "insertion") ||
		strings.Contains(trimmed, "deletion") {
		return "summary"
	}

	return ""
}

func classifyPushLine(line string) string {
	trimmed := strings.TrimSpace(line)

	// pack phase
	if strings.HasPrefix(trimmed, "Enumerating objects:") ||
		strings.HasPrefix(trimmed, "Counting objects:") ||
		strings.HasPrefix(trimmed, "Delta compression") ||
		strings.HasPrefix(trimmed, "Compressing objects:") ||
		strings.HasPrefix(trimmed, "Writing objects:") {
		return "pack"
	}

	// transfer phase
	if strings.HasPrefix(trimmed, "Total ") ||
		strings.HasPrefix(trimmed, "To ") {
		return "transfer"
	}

	// summary phase — ref update lines
	if (len(line) > 0 && line[0] == ' ' && strings.Contains(trimmed, "->")) ||
		strings.Contains(trimmed, "[new branch]") ||
		strings.Contains(trimmed, "[new tag]") {
		return "summary"
	}

	return ""
}
