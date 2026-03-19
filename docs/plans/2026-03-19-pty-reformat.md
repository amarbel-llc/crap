# PTY-Wrapping Reformatter Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Make `:: <util>` run `<util>` in a PTY and pipe its output through the CRAP reformatter, with proper signal forwarding and exit code propagation.

**Architecture:** New `RunWithPTYReformat` function in `go-crap/ptyrun.go` uses `creack/pty` to allocate a PTY for the child process, reads its output through `ReformatTAP`, forwards signals, and propagates the child's exit code. The `large-colon` CLI's `default:` case calls this function, and `:: ` with no args becomes `:: reformat`.

**Tech Stack:** Go, `creack/pty` v1.1.24, `os/signal`, `syscall`

**Rollback:** Revert the commits. Purely additive — existing subcommands are matched first in the switch.

---

### Task 1: Add creack/pty dependency

**Promotion criteria:** N/A

**Files:**
- Modify: `go-crap/go.mod`
- Modify: `go-crap/go.sum`
- Create: `go-crap/vendor/github.com/creack/pty/` (vendored)

**Step 1: Add the dependency**

```bash
cd /Users/sfriedenberg/eng/repos/crap/.worktrees/rapid-beech/go-crap
go get github.com/creack/pty/v2@latest
go mod vendor
```

**Step 2: Verify it builds**

Run: `go build ./...`
Expected: success

**Step 3: Commit**

```
git add go-crap/go.mod go-crap/go.sum go-crap/vendor/
git commit -m "deps: add creack/pty v2 for PTY allocation"
```

---

### Task 2: Implement RunWithPTYReformat

**Promotion criteria:** N/A

**Files:**
- Create: `go-crap/ptyrun.go`
- Create: `go-crap/ptyrun_test.go`

**Step 1: Write the failing test for command-not-found**

```go
package crap

import (
	"bytes"
	"context"
	"testing"
)

func TestRunWithPTYReformatCommandNotFound(t *testing.T) {
	var buf bytes.Buffer
	code := RunWithPTYReformat(context.Background(), "nonexistent-command-xyz", nil, &buf, false)
	if code != 127 {
		t.Errorf("expected exit code 127 for missing command, got %d", code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/sfriedenberg/eng/repos/crap/.worktrees/rapid-beech/go-crap && go test -run TestRunWithPTYReformatCommandNotFound ./...`
Expected: FAIL — `RunWithPTYReformat` not defined

**Step 3: Write the failing test for successful command**

```go
func TestRunWithPTYReformatSuccess(t *testing.T) {
	var buf bytes.Buffer
	code := RunWithPTYReformat(context.Background(), "echo", []string{"ok 1 - hello"}, &buf, false)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	out := buf.String()
	if !strings.Contains(out, "CRAP-2") {
		t.Errorf("expected CRAP-2 header in output, got:\n%s", out)
	}
	if !strings.Contains(out, "ok") && !strings.Contains(out, "hello") {
		t.Errorf("expected reformatted output containing 'ok' and 'hello', got:\n%s", out)
	}
}
```

**Step 4: Write the failing test for non-zero exit code**

```go
func TestRunWithPTYReformatNonZeroExit(t *testing.T) {
	var buf bytes.Buffer
	code := RunWithPTYReformat(context.Background(), "sh", []string{"-c", "exit 42"}, &buf, false)
	if code != 42 {
		t.Errorf("expected exit code 42, got %d", code)
	}
}
```

**Step 5: Implement RunWithPTYReformat**

Create `go-crap/ptyrun.go`:

```go
package crap

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty/v2"
)

// RunWithPTYReformat runs command in a PTY, pipes its output through
// ReformatTAP, forwards signals to the child, and returns the child's
// exit code.
func RunWithPTYReformat(ctx context.Context, command string, args []string, w io.Writer, color bool) int {
	path, err := exec.LookPath(command)
	if err != nil {
		fmt.Fprintf(os.Stderr, ":: %s: command not found\n", command)
		return 127
	}

	cmd := exec.CommandContext(ctx, path, args...)
	// Don't let CommandContext send SIGKILL — we handle signals ourselves.
	cmd.Cancel = func() error { return nil }

	ptmx, err := pty.Start(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, ":: failed to start %s: %v\n", command, err)
		return 126
	}
	defer ptmx.Close()

	// Forward signals to the child.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigCh {
			if cmd.Process != nil {
				cmd.Process.Signal(sig)
			}
		}
	}()
	defer signal.Stop(sigCh)

	// Pipe PTY output through reformatter.
	ReformatTAP(ptmx, w, color)

	// Wait for child to exit.
	waitErr := cmd.Wait()
	if waitErr != nil {
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
	return 0
}
```

Note: The `pty` import path depends on whether we use v1 (`github.com/creack/pty`)
or v2 (`github.com/creack/pty/v2`). Check which version `go get` installs and
adjust the import accordingly. The API is the same: `pty.Start(cmd)` returns
`(*os.File, error)`.

**Step 6: Run tests**

Run: `cd /Users/sfriedenberg/eng/repos/crap/.worktrees/rapid-beech/go-crap && go test -run TestRunWithPTYReformat ./...`
Expected: all 3 tests PASS

**Step 7: Commit**

```
git add go-crap/ptyrun.go go-crap/ptyrun_test.go
git commit -m "feat: add RunWithPTYReformat for PTY-wrapped command execution"
```

---

### Task 3: Update large-colon CLI

**Promotion criteria:** N/A

**Files:**
- Modify: `go-crap/cmd/large-colon/main.go:18-53` (main function)
- Modify: `go-crap/cmd/large-colon/main.go:55-66` (printUsage)

**Step 1: Change no-args behavior to reformat stdin**

In `main()`, change the `len(os.Args) < 2` block from printing usage to
running reformat:

```go
if len(os.Args) < 2 {
    crap.ReformatTAP(os.Stdin, os.Stdout, stdoutIsTerminal())
    return
}
```

**Step 2: Change default case to PTY reformat**

Replace the `default:` case in the switch:

```go
default:
    command := os.Args[1]
    args := os.Args[2:]
    color := stdoutIsTerminal()
    exitCode := crap.RunWithPTYReformat(ctx, command, args, os.Stdout, color)
    if exitCode != 0 {
        os.Exit(exitCode)
    }
```

**Step 3: Update printUsage**

Add a line for the implicit PTY mode:

```go
fmt.Fprintf(os.Stderr, ":: — CRAP-2 validator and writer toolkit\n\n")
fmt.Fprintf(os.Stderr, "Usage:\n")
fmt.Fprintf(os.Stderr, "  ::                    Read CRAP from stdin and emit CRAP-2 with ANSI colors\n")
fmt.Fprintf(os.Stderr, "  :: <cmd> [args...]     Run cmd in a PTY and reformat output as CRAP-2\n")
fmt.Fprintf(os.Stderr, "  :: reformat            Read CRAP from stdin and emit CRAP-2 with ANSI colors\n")
fmt.Fprintf(os.Stderr, "  :: validate [flags]    Validate CRAP-2 input\n")
fmt.Fprintf(os.Stderr, "  :: go-test [args...]   Run go test and convert output to CRAP-2\n")
fmt.Fprintf(os.Stderr, "  :: cargo-test [args...]  Run cargo test and convert output to CRAP-2\n")
fmt.Fprintf(os.Stderr, "  :: exec <cmd> [args...]  Run cmd for each arg sequentially and emit CRAP-2\n")
fmt.Fprintf(os.Stderr, "  :: exec-parallel       Run commands in parallel and emit CRAP-2\n")
```

**Step 4: Run build and tests**

Run: `cd /Users/sfriedenberg/eng/repos/crap/.worktrees/rapid-beech/go-crap && go build ./... && go test ./...`
Expected: PASS

**Step 5: Commit**

```
git add go-crap/cmd/large-colon/main.go
git commit -m "feat: :: with no args reformats stdin, :: <cmd> runs in PTY"
```

---

### Task 4: Vendor and nix build

**Promotion criteria:** N/A

**Files:**
- Modify: `go-crap/vendor/` (vendored pty package)

**Step 1: Ensure vendor is up to date**

```bash
cd /Users/sfriedenberg/eng/repos/crap/.worktrees/rapid-beech/go-crap
go mod vendor
```

**Step 2: Nix build**

Use the `mcp__plugin_chix_chix__build` tool with
`flake_dir: /Users/sfriedenberg/eng/repos/crap/.worktrees/rapid-beech`
Expected: success

**Step 3: Commit vendor changes if any**

```
git add go-crap/vendor/
git commit -m "deps: vendor creack/pty"
```

---

### Task 5: End-to-end verification

**Files:** None (testing only)

**Step 1: Run all tests**

Run: `cd /Users/sfriedenberg/eng/repos/crap/.worktrees/rapid-beech/go-crap && go test ./...`
Expected: all PASS

**Step 2: Test PTY reformat manually**

Test command-not-found:
```bash
go run ./cmd/large-colon/ nonexistent-xyz 2>&1; echo "exit: $?"
```
Expected: `:: nonexistent-xyz: command not found` on stderr, exit 127

Test PTY reformat with echo:
```bash
echo 'ok 1 - hello' | go run ./cmd/large-colon/
```
Expected: CRAP-2 header followed by colorized `ok 1 - hello`

Test PTY reformat with a real command:
```bash
go run ./cmd/large-colon/ echo 'ok 1 - test'
```
Expected: CRAP-2 header followed by reformatted output

Test exit code propagation:
```bash
go run ./cmd/large-colon/ sh -c 'exit 42'; echo "exit: $?"
```
Expected: exit 42
