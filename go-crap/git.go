package crap

import "strings"

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
