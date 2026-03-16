# ::git Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Create a `::git` binary that wraps git output in CRAP-2, with semantic phase parsing for `pull` and `push`.

**Architecture:** A `GitPhaseParser` interface in `go-crap/git.go` classifies git output lines into named phases. The CLI at `go-crap/cmd/crap-git/main.go` runs git, streams lines through the parser, updates the status line, and emits test points at phase boundaries. Unrecognized commands use a single-phase fallback.

**Tech Stack:** Go, go-crap library (Writer, status line, spinner), Nix flake

**Rollback:** N/A — purely additive, no existing code modified.

---

### Task 1: Git line classifier and phase parser

**Files:**
- Create: `go-crap/git.go`
- Create: `go-crap/git_test.go`

**Step 1: Write the failing tests for phase classification**

In `go-crap/git_test.go`:

```go
package crap

import (
	"testing"
)

func TestGitPullPhases(t *testing.T) {
	parser := NewGitPullParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"remote: Enumerating objects: 5, done.", "fetch"},
		{"remote: Counting objects: 100% (5/5), done.", "fetch"},
		{"remote: Compressing objects: 100% (3/3), done.", "fetch"},
		{"remote: Total 3 (delta 2), reused 0 (delta 0)", "fetch"},
		{"Receiving objects: 100% (3/3), done.", "fetch"},
		{"Resolving deltas: 100% (2/2), done.", "fetch"},
		{"From github.com:org/repo", "fetch"},
		{"   abc1234..def5678  main -> origin/main", "fetch"},
		{"Unpacking objects: 100% (3/3), done.", "unpack"},
		{"Updating abc1234..def5678", "merge"},
		{"Fast-forward", "merge"},
		{"Already up to date.", "merge"},
		{" file.go | 3 +++", "summary"},
		{" 1 file changed, 3 insertions(+)", "summary"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestGitPushPhases(t *testing.T) {
	parser := NewGitPushParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"Enumerating objects: 5, done.", "pack"},
		{"Counting objects: 100% (5/5), done.", "pack"},
		{"Delta compression using up to 10 threads", "pack"},
		{"Compressing objects: 100% (3/3), done.", "pack"},
		{"Writing objects: 100% (3/3), 1.23 KiB | 1.23 MiB/s, done.", "pack"},
		{"Total 3 (delta 2), reused 0 (delta 0), pack-reused 0", "transfer"},
		{"To github.com:org/repo.git", "transfer"},
		{"   abc1234..def5678  main -> main", "summary"},
		{" * [new branch]      feature -> feature", "summary"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestGitPhaseParserCollectsPhases(t *testing.T) {
	parser := NewGitPullParser()

	lines := []string{
		"remote: Enumerating objects: 5, done.",
		"remote: Counting objects: 100% (5/5), done.",
		"Unpacking objects: 100% (3/3), done.",
		"Updating abc1234..def5678",
		"Fast-forward",
		" file.go | 3 +++",
		" 1 file changed, 3 insertions(+)",
	}

	for _, line := range lines {
		parser.Feed(line)
	}

	phases := parser.Phases()

	if len(phases) != 4 {
		t.Fatalf("expected 4 phases, got %d", len(phases))
	}

	expected := []struct {
		name  string
		lines int
	}{
		{"fetch", 2},
		{"unpack", 1},
		{"merge", 2},
		{"summary", 2},
	}

	for i, exp := range expected {
		if phases[i].Name != exp.name {
			t.Errorf("phase %d: name = %q, want %q", i, phases[i].Name, exp.name)
		}
		if len(phases[i].Lines) != exp.lines {
			t.Errorf("phase %d (%s): %d lines, want %d", i, exp.name, len(phases[i].Lines), exp.lines)
		}
	}
}

func TestGitPhaseParserSkipsEmptyPhases(t *testing.T) {
	parser := NewGitPullParser()

	lines := []string{
		"Already up to date.",
	}

	for _, line := range lines {
		parser.Feed(line)
	}

	phases := parser.Phases()

	if len(phases) != 1 {
		t.Fatalf("expected 1 phase (no fetch/unpack), got %d: %+v", len(phases), phases)
	}

	if phases[0].Name != "merge" {
		t.Errorf("phase name = %q, want %q", phases[0].Name, "merge")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd go-crap && go test -run 'TestGitPull|TestGitPush|TestGitPhaseParser' -v`
Expected: FAIL — types don't exist yet

**Step 3: Implement the phase parser**

In `go-crap/git.go`:

```go
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
		(len(trimmed) > 0 && trimmed[0] == ' ' && strings.Contains(trimmed, "->") && strings.Contains(trimmed, "origin/")) {
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
	if (len(trimmed) > 0 && trimmed[0] == ' ' && strings.Contains(trimmed, "|")) ||
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
	if (len(trimmed) > 0 && trimmed[0] == ' ' && strings.Contains(trimmed, "->")) ||
		strings.Contains(trimmed, "[new branch]") ||
		strings.Contains(trimmed, "[new tag]") {
		return "summary"
	}

	return ""
}
```

**Step 4: Run tests to verify they pass**

Run: `cd go-crap && go test -run 'TestGitPull|TestGitPush|TestGitPhaseParser' -v`
Expected: PASS

**Step 5: Commit**

```
git add go-crap/git.go go-crap/git_test.go
git commit -m "feat: add git output phase parser for pull and push"
```

---

### Task 2: ConvertGit — generic and phase-aware git-to-CRAP-2 converter

**Files:**
- Modify: `go-crap/git.go`
- Modify: `go-crap/git_test.go`

**Step 1: Write the failing tests**

Append to `go-crap/git_test.go`:

```go
func TestConvertGitGenericSuccess(t *testing.T) {
	var buf bytes.Buffer
	exitCode := ConvertGit(
		context.Background(), []string{"version"},
		&buf, false, false,
	)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	out := buf.String()
	if !strings.Contains(out, "CRAP version 2") {
		t.Errorf("expected CRAP version header, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 1 - git version") {
		t.Errorf("expected ok test point, got:\n%s", out)
	}
	if !strings.Contains(out, "1..1") {
		t.Errorf("expected plan 1..1, got:\n%s", out)
	}
}

func TestConvertGitGenericFailure(t *testing.T) {
	var buf bytes.Buffer
	exitCode := ConvertGit(
		context.Background(), []string{"clone", "--bad-flag-that-does-not-exist"},
		&buf, false, false,
	)

	if exitCode == 0 {
		t.Error("expected non-zero exit code")
	}

	out := buf.String()
	if !strings.Contains(out, "not ok 1") {
		t.Errorf("expected not ok test point, got:\n%s", out)
	}
}

func TestConvertGitPullAlreadyUpToDate(t *testing.T) {
	// Feed simulated pull output through the phase emitter directly
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"Already up to date.",
	}

	emitGitPhases(tw, NewGitPullParser(), lines, 0)
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - merge") {
		t.Errorf("expected merge test point, got:\n%s", out)
	}
	if !strings.Contains(out, "1..1") {
		t.Errorf("expected plan 1..1, got:\n%s", out)
	}
}

func TestConvertGitPullWithFetch(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"remote: Enumerating objects: 5, done.",
		"remote: Counting objects: 100% (5/5), done.",
		"Unpacking objects: 100% (3/3), done.",
		"Updating abc1234..def5678",
		"Fast-forward",
		" file.go | 3 +++",
		" 1 file changed, 3 insertions(+)",
	}

	emitGitPhases(tw, NewGitPullParser(), lines, 0)
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - fetch") {
		t.Errorf("expected fetch test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - unpack") {
		t.Errorf("expected unpack test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 3 - merge") {
		t.Errorf("expected merge test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 4 - summary") {
		t.Errorf("expected summary test point, got:\n%s", out)
	}
}

func TestConvertGitPullFailure(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"fatal: not a git repository",
	}

	emitGitPhases(tw, NewGitPullParser(), lines, 128)
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "not ok 1 - git pull") {
		t.Errorf("expected not ok for failed pull, got:\n%s", out)
	}
}

func TestConvertGitPushPhases(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"Enumerating objects: 5, done.",
		"Counting objects: 100% (5/5), done.",
		"Delta compression using up to 10 threads",
		"Compressing objects: 100% (3/3), done.",
		"Writing objects: 100% (3/3), 1.23 KiB | 1.23 MiB/s, done.",
		"Total 3 (delta 2), reused 0 (delta 0), pack-reused 0",
		"To github.com:org/repo.git",
		"   abc1234..def5678  main -> main",
	}

	emitGitPhases(tw, NewGitPushParser(), lines, 0)
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - pack") {
		t.Errorf("expected pack test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - transfer") {
		t.Errorf("expected transfer test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 3 - summary") {
		t.Errorf("expected summary test point, got:\n%s", out)
	}
}
```

Add `"bytes"`, `"context"`, `"strings"` to imports.

**Step 2: Run tests to verify they fail**

Run: `cd go-crap && go test -run 'TestConvertGit' -v`
Expected: FAIL — `ConvertGit` and `emitGitPhases` don't exist

**Step 3: Implement ConvertGit and emitGitPhases**

Append to `go-crap/git.go`:

```go
import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

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
	var stderrBuf, stdoutBuf bytes.Buffer

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		tw.BailOut(fmt.Sprintf("failed to create stdout pipe: %v", err))
		return 1
	}
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		tw.BailOut(fmt.Sprintf("failed to start git: %v", err))
		return 1
	}

	var mu sync.Mutex
	var lastContent string
	stopTicker := startStatusTicker(tw, spinner, &mu, &lastContent)

	var lines []string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
		stdoutBuf.WriteString(line)
		stdoutBuf.WriteByte('\n')
		mu.Lock()
		lastContent = line
		spinner.Touch()
		tw.UpdateLastLine(spinner.prefix() + line)
		mu.Unlock()
	}

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

	// Add stderr lines
	if stderr := stderrBuf.String(); stderr != "" {
		for _, line := range strings.Split(strings.TrimRight(stderr, "\n"), "\n") {
			lines = append(lines, line)
			mu.Lock()
			lastContent = line
			spinner.Touch()
			tw.UpdateLastLine(spinner.prefix() + line)
			mu.Unlock()
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
		tw.NotOk(desc, map[string]string{
			"exit-code": fmt.Sprintf("%d", exitCode),
			"output":    combined,
		})
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
```

Note: the import block at the top of `git.go` needs to be merged — the file will have a single `import` block with all needed packages.

**Step 4: Run tests to verify they pass**

Run: `cd go-crap && go test -run 'TestConvertGit|TestGitPull|TestGitPush|TestGitPhaseParser' -v`
Expected: PASS

**Step 5: Commit**

```
git add go-crap/git.go go-crap/git_test.go
git commit -m "feat: add ConvertGit with phase-aware pull/push and generic fallback"
```

---

### Task 3: CLI binary

**Files:**
- Create: `go-crap/cmd/crap-git/main.go`

**Step 1: Write the binary**

```go
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
		fmt.Fprintf(os.Stderr, "::git — wrap git output in CRAP-2\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  ::git <git-command> [args...]\n")
		os.Exit(1)
	}

	color := stdoutIsTerminal()
	exitCode := crap.ConvertGit(ctx, os.Args[1:], os.Stdout, false, color)
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
```

**Step 2: Verify it compiles**

Run: `cd go-crap && go build ./cmd/crap-git/`
Expected: success, produces `crap-git` binary

**Step 3: Smoke test**

Run: `cd go-crap && ./crap-git version`
Expected: CRAP-2 output with `ok 1 - git version` and `1..1`

**Step 4: Commit**

```
git add go-crap/cmd/crap-git/main.go
git commit -m "feat: add ::git CLI binary"
```

---

### Task 4: Nix packaging

**Files:**
- Modify: `flake.nix`

**Step 1: Add crap-git package to flake.nix**

After the `large-colon` definition, add:

```nix
crap-git = pkgs.buildGoModule {
  pname = "crap-git";
  version = "0.1.0";
  src = ./go-crap;
  subPackages = [ "cmd/crap-git" ];
  vendorHash = "sha256-5Pb0w+3v+R9ciPQ4H0HyFZlIJPOGjFFURDwLl2JvLjs=";

  postInstall = ''
    mv $out/bin/crap-git "$out/bin/::git"
  '';

  meta = {
    description = "Git wrapper that emits CRAP-2 output";
    homepage = "https://github.com/amarbel-llc/crap";
    license = pkgs.lib.licenses.mit;
  };
};
```

Add to packages output:

```nix
packages = {
  default = large-colon;
  inherit large-colon rust-crap crap-git;
};
```

**Step 2: Build**

Run: `nix build .#crap-git --show-trace`
Expected: success. If vendorHash is wrong, update it from the error message.

**Step 3: Verify binary name**

Run: `ls result/bin/`
Expected: `::git`

**Step 4: Smoke test via nix**

Run: `./result/bin/'::git' version`
Expected: CRAP-2 output wrapping git version

**Step 5: Verify existing build still works**

Run: `just build`
Expected: success (default package unchanged)

**Step 6: Commit**

```
git add flake.nix
git commit -m "feat: add crap-git nix package (::git binary)"
```

---

### Task 5: Handle stderr streaming for git progress

Git writes progress output (counting, compressing, etc.) to stderr, not stdout. The `ConvertGit` implementation in Task 2 buffers stderr and appends it after stdout completes. This task fixes it to stream stderr lines as status line updates in real time alongside stdout.

**Files:**
- Modify: `go-crap/git.go`
- Modify: `go-crap/git_test.go`

**Step 1: Write failing test**

Append to `go-crap/git_test.go`:

```go
func TestConvertGitStreamsStderr(t *testing.T) {
	var buf bytes.Buffer
	// git status on a non-repo dir writes to stderr
	exitCode := ConvertGit(
		context.Background(), []string{"status"},
		&buf, false, false,
	)

	out := buf.String()
	// Should produce valid CRAP-2 regardless
	if !strings.Contains(out, "CRAP version 2") {
		t.Errorf("expected CRAP version header, got:\n%s", out)
	}
	_ = exitCode // may be 0 or 128 depending on cwd
}
```

**Step 2: Refactor ConvertGit to stream stderr**

Modify `ConvertGit` in `go-crap/git.go`: instead of `cmd.Stderr = &stderrBuf`, create a stderr pipe and merge both stdout and stderr lines into the status line and line collector using goroutines.

```go
// In ConvertGit, replace the stderr buffering with:
stderr, err := cmd.StderrPipe()
if err != nil {
    tw.BailOut(fmt.Sprintf("failed to create stderr pipe: %v", err))
    return 1
}

// ... after cmd.Start() ...

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
```

**Step 3: Run all git tests**

Run: `cd go-crap && go test -run 'TestConvertGit|TestGitPull|TestGitPush|TestGitPhaseParser' -v`
Expected: PASS

**Step 4: Commit**

```
git add go-crap/git.go go-crap/git_test.go
git commit -m "feat: stream stderr in real time for ::git status line"
```

---

### Task 6: Run full test suite and final build

**Step 1: Run all Go tests**

Run: `just test-go`
Expected: PASS

**Step 2: Full nix build**

Run: `just build`
Expected: success

**Step 3: Build crap-git specifically**

Run: `nix build .#crap-git --show-trace`
Expected: success

**Step 4: No commit needed — this is verification only**
