# Crappy Wrappers Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add clone/fetch phase parsers to crappy-git, change unrecognized subcommands to pure passthrough, extract shared infrastructure for wrapping CLI tools, and create a new crappy-brew wrapper with phase parsers for install/upgrade/update/tap.

**Architecture:** Each "crappy" wrapper is a Go binary that intercepts recognized subcommands to emit CRAP-2 phase output with spinner/status-line, and passes through unrecognized subcommands with no modification (stdin/stdout/stderr connected directly). Shared infrastructure (`findBinary`, `convertWithPhases`, `emitPhases`, `execPassthrough`) lives in `go-crap/wrap.go`. Tool-specific phase parsers and classifier functions live in `go-crap/git.go` and `go-crap/brew.go`. Each CLI is a thin entry point in `go-crap/cmd/`.

**Tech Stack:** Go 1.24, go-crap library (CRAP-2 Writer, statusSpinner, startStatusTicker), Nix flake (buildGoModule)

---

## Chunk 1: crappy-git upgrades

### Task 1: Add git clone phase parser

**Files:**
- Modify: `go-crap/git.go`
- Modify: `go-crap/git_test.go`

- [ ] **Step 1: Write failing test for clone line classification**

Add to `go-crap/git_test.go`:

```go
func TestGitClonePhases(t *testing.T) {
	parser := NewGitCloneParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"Cloning into 'repo'...", "init"},
		{"remote: Enumerating objects: 100, done.", "receive"},
		{"remote: Counting objects: 100% (100/100), done.", "receive"},
		{"remote: Compressing objects: 100% (80/80), done.", "receive"},
		{"remote: Total 100 (delta 20), reused 80 (delta 10)", "receive"},
		{"Receiving objects: 100% (100/100), 1.23 MiB | 5.00 MiB/s, done.", "receive"},
		{"Resolving deltas: 100% (20/20), done.", "resolve"},
		{"Updating files: 100% (50/50), done.", "checkout"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-crap && go test -run TestGitClonePhases -v`
Expected: FAIL — `NewGitCloneParser` undefined

- [ ] **Step 3: Implement clone parser**

Add to `go-crap/git.go`:

```go
func NewGitCloneParser() *GitPhaseParser {
	return &GitPhaseParser{
		phaseOrder: []string{"init", "receive", "resolve", "checkout"},
		classify:   classifyCloneLine,
		phases:     make(map[string]*GitPhase),
	}
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-crap && go test -run TestGitClonePhases -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go-crap/git.go go-crap/git_test.go
git commit -m "feat: add git clone phase parser"
```

### Task 2: Add git fetch phase parser

**Files:**
- Modify: `go-crap/git.go`
- Modify: `go-crap/git_test.go`

- [ ] **Step 1: Write failing test for fetch line classification**

Add to `go-crap/git_test.go`:

```go
func TestGitFetchPhases(t *testing.T) {
	parser := NewGitFetchParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"remote: Enumerating objects: 5, done.", "negotiate"},
		{"remote: Counting objects: 100% (5/5), done.", "negotiate"},
		{"remote: Compressing objects: 100% (3/3), done.", "negotiate"},
		{"remote: Total 3 (delta 2), reused 0 (delta 0)", "negotiate"},
		{"Receiving objects: 100% (3/3), done.", "receive"},
		{"Resolving deltas: 100% (2/2), done.", "resolve"},
		{"From github.com:org/repo", "negotiate"},
		{"   abc1234..def5678  main       -> origin/main", "negotiate"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-crap && go test -run TestGitFetchPhases -v`
Expected: FAIL — `NewGitFetchParser` undefined

- [ ] **Step 3: Implement fetch parser**

Add to `go-crap/git.go`:

```go
func NewGitFetchParser() *GitPhaseParser {
	return &GitPhaseParser{
		phaseOrder: []string{"negotiate", "receive", "resolve"},
		classify:   classifyFetchLine,
		phases:     make(map[string]*GitPhase),
	}
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-crap && go test -run TestGitFetchPhases -v`
Expected: PASS

- [ ] **Step 5: Register clone and fetch in parserForSubcommand**

Update `parserForSubcommand` in `go-crap/git.go`:

```go
func parserForSubcommand(args []string) *GitPhaseParser {
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
```

- [ ] **Step 6: Run all git classifier tests**

Run: `cd go-crap && go test -run 'TestGit(Clone|Fetch|Pull|Push)Phases' -v`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add go-crap/git.go go-crap/git_test.go
git commit -m "feat: add git fetch phase parser and register clone/fetch"
```

### Task 3: Extract shared wrapper infrastructure and add passthrough

**Files:**
- Create: `go-crap/wrap.go`
- Modify: `go-crap/git.go`
- Modify: `go-crap/git_test.go`
- Modify: `go-crap/cmd/crappy-git/main.go`

This task extracts the shared pieces (`findBinary`, `execPassthrough`, `convertWithPhases`, `emitPhases`) into `wrap.go`, renames `GitPhaseParser`→`PhaseParser` and `GitPhase`→`Phase` in `git.go`, refactors `ConvertGit` to use the shared code and add passthrough, and updates all callers/tests atomically.

- [ ] **Step 1: Create `go-crap/wrap.go` with shared infrastructure**

Create `go-crap/wrap.go`:

```go
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

// findBinary searches $PATH for a binary with the given name that is not the
// same file as selfExe. This prevents infinite recursion when a user symlinks
// or renames a crappy wrapper to the original binary name.
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

// execPassthrough runs a command with stdin/stdout/stderr connected directly.
// No CRAP-2 framing is applied.
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

// convertWithPhases runs a command, streams its output through the status
// spinner, then emits CRAP-2 phase test points based on the parser.
// toolName is used for error descriptions (e.g. "git", "brew").
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

// emitPhases feeds lines through a phase parser and emits one test point
// per non-empty phase. On non-zero exit, emits a single not ok instead.
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
```

- [ ] **Step 2: Refactor ConvertGit to use shared infrastructure**

Replace `ConvertGit`, `emitPhases`, and `emitGitGeneric` in `go-crap/git.go` with:

```go
// FindGit searches $PATH for a "git" binary that is not the same file as
// selfExe. Wrapper around findBinary for backward compatibility.
func FindGit(selfExe string) (string, error) {
	return findBinary(selfExe, "git")
}

// ConvertGit runs git with args. For recognized subcommands (pull, push,
// clone, fetch) it emits CRAP-2 phase output. For all others it passes
// through with no modification.
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
```

Remove `ConvertGit` (old version), `emitGitPhases`, `emitGitGeneric`, and `FindGit` (old version with inline logic) from `go-crap/git.go`. The new `ConvertGit` and `FindGit` shown above replace them.

Rename `GitPhaseParser` to `PhaseParser` and `GitPhase` to `Phase` throughout `go-crap/git.go` (type definitions, constructor return types, function signatures). The type definitions stay in `git.go` since they're still used by both git and brew parsers.

Remove now-unused imports from `git.go`. The file will only need `"context"`, `"fmt"`, `"io"`, `"strings"`.

- [ ] **Step 3: Update crappy-git main.go for new signature**

Update `go-crap/cmd/crappy-git/main.go` to pass stdin/stderr:

```go
exitCode := crap.ConvertGit(ctx, selfExe, os.Args[1:], os.Stdout, os.Stdin, os.Stderr, false, color)
```

- [ ] **Step 4: Update all git tests for new signature and shared functions**

In `go-crap/git_test.go`:

1. Rename `emitGitPhases` → `emitPhases` calls in `TestEmitGitPhasesSuccess`, `TestEmitGitPhasesAlreadyUpToDate`, `TestEmitGitPhasesFailure`, `TestEmitGitPushPhases`, adding `"git"` as the toolName parameter:

```go
// Example — apply same pattern to all four:
emitPhases(tw, NewGitPullParser(), lines, 0, "git")
```

2. Delete `TestConvertGitGenericSuccess` and `TestConvertGitGenericFailure` (old integration tests using 6-arg signature). Replace with these passthrough tests:

```go
func TestConvertGitPassthroughSuccess(t *testing.T) {
	var buf bytes.Buffer
	exitCode := ConvertGit(
		context.Background(), os.Args[0], []string{"version"},
		&buf, os.Stdin, os.Stderr, false, false,
	)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	out := buf.String()
	if strings.Contains(out, "CRAP-2") {
		t.Errorf("passthrough should not contain CRAP-2 header, got:\n%s", out)
	}
	if !strings.Contains(out, "git version") {
		t.Errorf("expected raw git version output, got:\n%s", out)
	}
}
```

3. Add passthrough failure test using an unrecognized subcommand:

```go
func TestConvertGitPassthroughFailure(t *testing.T) {
	var buf bytes.Buffer
	var stderrBuf bytes.Buffer
	exitCode := ConvertGit(
		context.Background(), os.Args[0], []string{"status", "--bad-flag-that-does-not-exist"},
		&buf, os.Stdin, &stderrBuf, false, false,
	)

	if exitCode == 0 {
		t.Error("expected non-zero exit code")
	}

	out := buf.String()
	if strings.Contains(out, "CRAP-2") {
		t.Errorf("passthrough should not contain CRAP-2 header, got:\n%s", out)
	}
}
```

4. Delete `TestEmitGitGenericSuccess` and `TestEmitGitGenericFailure` (emit-level unit tests for removed `emitGitGeneric`).

5. Add clone emit test (was deferred from Task 1 because `emitPhases` didn't exist yet):

```go
func TestEmitGitClonePhases(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"Cloning into 'repo'...",
		"remote: Enumerating objects: 100, done.",
		"remote: Counting objects: 100% (100/100), done.",
		"Receiving objects: 100% (100/100), 1.23 MiB | 5.00 MiB/s, done.",
		"Resolving deltas: 100% (20/20), done.",
	}

	emitPhases(tw, NewGitCloneParser(), lines, 0, "git")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - init") {
		t.Errorf("expected init test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - receive") {
		t.Errorf("expected receive test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 3 - resolve") {
		t.Errorf("expected resolve test point, got:\n%s", out)
	}
}
```

- [ ] **Step 5: Run all tests**

Run: `cd go-crap && go test ./... -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add go-crap/wrap.go go-crap/git.go go-crap/git_test.go go-crap/cmd/crappy-git/main.go
git commit -m "refactor: extract shared wrapper infrastructure, add passthrough

Extract findBinary, execPassthrough, convertWithPhases, and emitPhases
into wrap.go for reuse by crappy-git and crappy-brew.

Unrecognized git subcommands now exec git directly with stdin/stdout/stderr
connected. Only recognized subcommands (pull, push, clone, fetch) get
CRAP-2 phase output."
```

## Chunk 2: crappy-brew

### Task 4: Add brew phase parsers and ConvertBrew

**Files:**
- Create: `go-crap/brew.go`
- Create: `go-crap/brew_test.go`

- [ ] **Step 1: Write failing test for brew install line classification**

Create `go-crap/brew_test.go`:

```go
package crap

import (
	"bytes"
	"strings"
	"testing"
)

func TestBrewInstallPhases(t *testing.T) {
	parser := NewBrewInstallParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"==> Downloading https://ghcr.io/v2/homebrew/core/jq/manifests/1.7.1", "download"},
		{"==> Fetching jq", "download"},
		{"==> Fetching dependencies for jq: oniguruma", "download"},
		{"Already downloaded: /Users/user/Library/Caches/Homebrew/downloads/abc--jq-1.7.1.tar.gz", "download"},
		{"==> Installing jq", "install"},
		{"==> Pouring jq--1.7.1.arm64_sonoma.bottle.tar.gz", "install"},
		{"==> Linking jq", "link"},
		{"==> Caveats", "caveats"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestBrewUpdatePhases(t *testing.T) {
	parser := NewBrewUpdateParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"==> Fetching newest version of Homebrew...", "fetch"},
		{"remote: Enumerating objects: 100, done.", "fetch"},
		{"remote: Counting objects: 100% (100/100), done.", "fetch"},
		{"==> Updated 2 taps (homebrew/core and homebrew/cask).", "update"},
		{"==> New Formulae", "update"},
		{"==> Updated Formulae", "update"},
		{"==> Deleted Formulae", "update"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestBrewTapPhases(t *testing.T) {
	parser := NewBrewTapParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"==> Tapping homebrew/cask", "clone"},
		{"Cloning into '/opt/homebrew/Library/Taps/homebrew/homebrew-cask'...", "clone"},
		{"remote: Enumerating objects: 100, done.", "clone"},
		{"==> Tapped 1 command and 4000 casks (4,123 files, 300MB).", "install"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd go-crap && go test -run TestBrew -v`
Expected: FAIL — types undefined

- [ ] **Step 3: Implement brew.go**

Create `go-crap/brew.go`:

```go
package crap

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// FindBrew searches $PATH for a "brew" binary that is not the same file as
// selfExe.
func FindBrew(selfExe string) (string, error) {
	return findBinary(selfExe, "brew")
}

func brewParserForSubcommand(args []string) *PhaseParser {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "install":
		return NewBrewInstallParser()
	case "upgrade":
		return NewBrewUpgradeParser()
	case "update":
		return NewBrewUpdateParser()
	case "tap":
		return NewBrewTapParser()
	default:
		return nil
	}
}

// ConvertBrew runs brew with args. For recognized subcommands (install,
// upgrade, update, tap) it emits CRAP-2 phase output. For all others it
// passes through with no modification.
func ConvertBrew(ctx context.Context, selfExe string, args []string, w io.Writer, stdin io.Reader, stderrW io.Writer, verbose bool, color bool) int {
	brewPath, err := FindBrew(selfExe)
	if err != nil {
		fmt.Fprintf(stderrW, "::brew: %s\n", err)
		return 1
	}

	parser := brewParserForSubcommand(args)
	if parser == nil {
		return execPassthrough(ctx, brewPath, args, w, stdin, stderrW)
	}

	return convertWithPhases(ctx, brewPath, args, w, parser, color, "brew")
}

func NewBrewInstallParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"download", "install", "link", "caveats"},
		classify:   classifyBrewInstallLine,
		phases:     make(map[string]*Phase),
	}
}

func NewBrewUpgradeParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"download", "install", "link", "caveats"},
		classify:   classifyBrewInstallLine,
		phases:     make(map[string]*Phase),
	}
}

func NewBrewUpdateParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"fetch", "update"},
		classify:   classifyBrewUpdateLine,
		phases:     make(map[string]*Phase),
	}
}

func NewBrewTapParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"clone", "install"},
		classify:   classifyBrewTapLine,
		phases:     make(map[string]*Phase),
	}
}

func classifyBrewInstallLine(line string) string {
	trimmed := strings.TrimSpace(line)

	if strings.HasPrefix(trimmed, "==> Downloading") ||
		strings.HasPrefix(trimmed, "==> Fetching") ||
		strings.HasPrefix(trimmed, "Already downloaded:") {
		return "download"
	}

	if strings.HasPrefix(trimmed, "==> Installing") ||
		strings.HasPrefix(trimmed, "==> Pouring") {
		return "install"
	}

	if strings.HasPrefix(trimmed, "==> Linking") {
		return "link"
	}

	if strings.HasPrefix(trimmed, "==> Caveats") {
		return "caveats"
	}

	return ""
}

func classifyBrewUpdateLine(line string) string {
	trimmed := strings.TrimSpace(line)

	if strings.HasPrefix(trimmed, "==> Fetching") ||
		strings.HasPrefix(trimmed, "remote: ") ||
		strings.HasPrefix(trimmed, "Receiving objects:") ||
		strings.HasPrefix(trimmed, "Resolving deltas:") {
		return "fetch"
	}

	if strings.HasPrefix(trimmed, "==> Updated") ||
		strings.HasPrefix(trimmed, "==> New") ||
		strings.HasPrefix(trimmed, "==> Deleted") ||
		strings.HasPrefix(trimmed, "==> Renamed") {
		return "update"
	}

	return ""
}

func classifyBrewTapLine(line string) string {
	trimmed := strings.TrimSpace(line)

	if strings.HasPrefix(trimmed, "==> Tapping") ||
		strings.HasPrefix(trimmed, "Cloning into") ||
		strings.HasPrefix(trimmed, "remote: ") ||
		strings.HasPrefix(trimmed, "Receiving objects:") ||
		strings.HasPrefix(trimmed, "Resolving deltas:") {
		return "clone"
	}

	if strings.HasPrefix(trimmed, "==> Tapped") {
		return "install"
	}

	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd go-crap && go test -run TestBrew -v`
Expected: All PASS

- [ ] **Step 5: Write emit tests for brew phases**

Add to `go-crap/brew_test.go`:

```go
func TestEmitBrewInstallPhases(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"==> Fetching jq",
		"==> Downloading https://ghcr.io/v2/homebrew/core/jq/manifests/1.7.1",
		"==> Installing jq",
		"==> Pouring jq--1.7.1.arm64_sonoma.bottle.tar.gz",
		"==> Linking jq",
	}

	emitPhases(tw, NewBrewInstallParser(), lines, 0, "brew")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - download") {
		t.Errorf("expected download test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - install") {
		t.Errorf("expected install test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 3 - link") {
		t.Errorf("expected link test point, got:\n%s", out)
	}
}

func TestEmitBrewInstallFailure(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	emitPhases(tw, NewBrewInstallParser(), []string{"Error: No formula found"}, 1, "brew")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "not ok 1 - brew download") {
		t.Errorf("expected not ok for failed install, got:\n%s", out)
	}
}

func TestEmitBrewUpdatePhases(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"==> Fetching newest version of Homebrew...",
		"remote: Enumerating objects: 100, done.",
		"==> Updated 2 taps (homebrew/core and homebrew/cask).",
		"==> New Formulae",
	}

	emitPhases(tw, NewBrewUpdateParser(), lines, 0, "brew")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - fetch") {
		t.Errorf("expected fetch test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - update") {
		t.Errorf("expected update test point, got:\n%s", out)
	}
}

func TestEmitBrewTapPhases(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"==> Tapping homebrew/cask",
		"Cloning into '/opt/homebrew/Library/Taps/homebrew/homebrew-cask'...",
		"==> Tapped 1 command and 4000 casks (4,123 files, 300MB).",
	}

	emitPhases(tw, NewBrewTapParser(), lines, 0, "brew")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - clone") {
		t.Errorf("expected clone test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - install") {
		t.Errorf("expected install test point, got:\n%s", out)
	}
}
```

- [ ] **Step 6: Run all brew tests**

Run: `cd go-crap && go test -run '(TestBrew|TestEmitBrew)' -v`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add go-crap/brew.go go-crap/brew_test.go
git commit -m "feat: add brew phase parsers and ConvertBrew

Phase parsers for install/upgrade (download, install, link, caveats),
update (fetch, update), and tap (clone, install). Unrecognized brew
subcommands pass through with no CRAP-2 framing."
```

### Task 5: Add crappy-brew CLI entry point

**Files:**
- Create: `go-crap/cmd/crappy-brew/main.go`

- [ ] **Step 1: Create CLI entry point**

Create `go-crap/cmd/crappy-brew/main.go`:

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
```

- [ ] **Step 2: Verify it compiles**

Run: `cd go-crap && go build ./cmd/crappy-brew/`
Expected: Compiles successfully

- [ ] **Step 3: Commit**

```bash
git add go-crap/cmd/crappy-brew/main.go
git commit -m "feat: add crappy-brew CLI entry point (::brew binary)"
```

### Task 6: Add crappy-brew to Nix flake

**Files:**
- Modify: `flake.nix`

- [ ] **Step 1: Add crappy-brew derivation**

Add after the `crappy-git` derivation in `flake.nix`:

```nix
crappy-brew = pkgs.buildGoModule {
  pname = "crappy-brew";
  version = "0.1.0";
  src = ./go-crap;
  subPackages = [ "cmd/crappy-brew" ];
  vendorHash = "sha256-5Pb0w+3v+R9ciPQ4H0HyFZlIJPOGjFFURDwLl2JvLjs=";

  postInstall = ''
    mv $out/bin/crappy-brew "$out/bin/::brew"
  '';

  meta = {
    description = "Brew wrapper that emits CRAP-2 output";
    homepage = "https://github.com/amarbel-llc/crap";
    license = pkgs.lib.licenses.mit;
  };
};
```

- [ ] **Step 2: Add crappy-brew to default package and exports**

Update the `packages` attrset — add `crappy-brew` to the `symlinkJoin` paths and to `inherit`:

```nix
packages = {
  default = pkgs.symlinkJoin {
    name = "crap";
    paths = [
      large-colon
      crappy-git
      crappy-brew
    ];
  };
  inherit large-colon crappy-git crappy-brew rust-crap;
};
```

- [ ] **Step 3: Build to verify**

Run: `nix build --show-trace`
Expected: Builds successfully. If vendorHash changed, update it with the hash from the error message.

- [ ] **Step 4: Verify binaries exist**

Run: `ls -la result/bin/`
Expected: `::`, `::brew`, `::git`, `large-colon` all present

- [ ] **Step 5: Commit**

```bash
git add flake.nix
git commit -m "feat: add crappy-brew Nix derivation (::brew binary)"
```

### Task 7: Run full test suite

- [ ] **Step 1: Run all Go tests**

Run: `cd go-crap && go test ./... -v`
Expected: All PASS

- [ ] **Step 2: Run nix build**

Run: `just build`
Expected: Builds successfully

- [ ] **Step 3: Run full test suite**

Run: `just test`
Expected: All PASS
