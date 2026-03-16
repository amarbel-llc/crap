package crap

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Phase holds lines accumulated during one semantic phase of a command.
type Phase struct {
	Name  string
	Lines []string
}

// PhaseParser classifies output lines into named phases and accumulates
// them in order, skipping phases that receive no lines.
type PhaseParser struct {
	phaseOrder []string
	classify   func(string) string
	current    string
	phases     map[string]*Phase
	order      []string
}

// NewGitPullParser returns a parser for git pull output phases:
// fetch, unpack, merge, summary.
func NewGitPullParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"fetch", "unpack", "merge", "summary"},
		classify:   classifyPullLine,
		phases:     make(map[string]*Phase),
	}
}

// NewGitPushParser returns a parser for git push output phases:
// pack, transfer, summary.
func NewGitPushParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"pack", "transfer", "summary"},
		classify:   classifyPushLine,
		phases:     make(map[string]*Phase),
	}
}

// NewGitCloneParser returns a parser for git clone output phases:
// init, receive, resolve, checkout.
func NewGitCloneParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"init", "receive", "resolve", "checkout"},
		classify:   classifyCloneLine,
		phases:     make(map[string]*Phase),
	}
}

// NewGitFetchParser returns a parser for git fetch output phases:
// negotiate, receive, resolve.
func NewGitFetchParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"negotiate", "receive", "resolve"},
		classify:   classifyFetchLine,
		phases:     make(map[string]*Phase),
	}
}

// Classify returns the phase name for a line without accumulating it.
func (p *PhaseParser) Classify(line string) string {
	return p.classify(line)
}

// Feed classifies a line and appends it to the appropriate phase.
func (p *PhaseParser) Feed(line string) string {
	phase := p.classify(line)
	if phase == "" {
		return ""
	}
	if _, ok := p.phases[phase]; !ok {
		p.phases[phase] = &Phase{Name: phase}
		p.order = append(p.order, phase)
	}
	p.phases[phase].Lines = append(p.phases[phase].Lines, line)
	p.current = phase
	return phase
}

// Phases returns accumulated phases in the order defined by the parser,
// skipping any phase that received no lines.
func (p *PhaseParser) Phases() []Phase {
	var result []Phase
	for _, name := range p.phaseOrder {
		if ph, ok := p.phases[name]; ok {
			result = append(result, *ph)
		}
	}
	return result
}

// FindGit searches $PATH for a "git" binary that is not the same file as
// selfExe. This prevents infinite recursion when a user symlinks or renames
// ::git to "git". Returns the absolute path to git, or an error if no
// suitable git is found.
func FindGit(selfExe string) (string, error) {
	return findBinary(selfExe, "git")
}

// parserForSubcommand returns a phase parser for recognized git subcommands,
// or nil for generic fallback.
func parserForSubcommand(args []string) *PhaseParser {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "pull":
		return NewGitPullParser()
	case "push":
		return NewGitPushParser()
	case "clone":
		return NewGitCloneParser()
	case "fetch":
		return NewGitFetchParser()
	default:
		return nil
	}
}

// ConvertGit runs git with args and writes CRAP-2 output. For recognized
// subcommands (pull, push, clone, fetch) it emits semantic phase test points.
// For all others it passes through stdin/stdout/stderr directly to git.
// selfExe is the path to the running binary, used to avoid exec recursion
// when the user renames ::git to "git".
// Returns the git exit code.
func ConvertGit(ctx context.Context, selfExe string, args []string, w io.Writer, stdin io.Reader, stderrW io.Writer, verbose bool, color bool) int {
	gitPath, err := FindGit(selfExe)
	if err != nil {
		fmt.Fprintf(stderrW, "::git: %s\n", err)
		return 1
	}

	parser := parserForSubcommand(args)
	if parser == nil {
		return execPassthrough(ctx, gitPath, args, w, stdin, stderrW)
	}

	return convertWithPhases(ctx, gitPath, args, w, parser, color, "git")
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

func classifyCloneLine(line string) string {
	trimmed := strings.TrimSpace(line)

	if strings.HasPrefix(trimmed, "Cloning into ") {
		return "init"
	}

	if strings.HasPrefix(trimmed, "remote: ") ||
		strings.HasPrefix(trimmed, "Receiving objects:") {
		return "receive"
	}

	if strings.HasPrefix(trimmed, "Resolving deltas:") {
		return "resolve"
	}

	if strings.HasPrefix(trimmed, "Updating files:") ||
		strings.HasPrefix(trimmed, "Checking out files:") {
		return "checkout"
	}

	return ""
}

func classifyFetchLine(line string) string {
	trimmed := strings.TrimSpace(line)

	if strings.HasPrefix(trimmed, "Receiving objects:") {
		return "receive"
	}

	if strings.HasPrefix(trimmed, "Resolving deltas:") {
		return "resolve"
	}

	if strings.HasPrefix(trimmed, "remote: ") ||
		strings.HasPrefix(trimmed, "From ") ||
		(len(line) > 0 && line[0] == ' ' && strings.Contains(trimmed, "->")) {
		return "negotiate"
	}

	return ""
}
