# Awk-Based Phase Parsing Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Go phase parsers with embedded awk scripts that emit TAP, consumed by a Go harness that converts to CRAP-2.

**Architecture:** Awk scripts classify command output lines into phases and emit TAP (test points for phases, comments for status line). A Go harness (`convertWithAwk`) pipes the real command through the awk script, reads the TAP stream, and emits CRAP-2 using the existing `Writer`. The existing `ReformatTAP` handles version/plan/test-point conversion; the harness adds spinner and status line support on top.

**Tech Stack:** Go (go:embed, bufio, os/exec), awk, bats (testing)

---

## File Structure

```
go-crap/
  awk/
    git/
      pull.awk           # NEW: git pull phase classifier
      push.awk           # NEW: git push phase classifier
      clone.awk          # NEW: git clone phase classifier
      fetch.awk          # NEW: git fetch phase classifier
      rebase.awk         # NEW: git rebase phase classifier
      testdata/
        pull.input       # NEW: sample git pull output
        pull.expected     # NEW: expected TAP output
        rebase.input     # NEW: sample git rebase output
        rebase.expected   # NEW: expected TAP output
        clone.input      # NEW: sample git clone output
        clone.expected    # NEW: expected TAP output
        push.input       # NEW: sample git push output
        push.expected     # NEW: expected TAP output
        fetch.input      # NEW: sample git fetch output
        fetch.expected    # NEW: expected TAP output
    brew/
      install.awk        # NEW: brew install phase classifier
      upgrade.awk        # NEW: brew upgrade phase classifier (multi-package)
      update.awk         # NEW: brew update phase classifier
      tap.awk            # NEW: brew tap phase classifier
      testdata/
        install.input    # NEW: sample brew install output
        install.expected  # NEW: expected TAP output
        upgrade.input    # NEW: sample brew upgrade output (multi-package)
        upgrade.expected  # NEW: expected TAP output
        update.input     # NEW: sample brew update output
        update.expected   # NEW: expected TAP output
        tap.input        # NEW: sample brew tap output
        tap.expected      # NEW: expected TAP output
  awkharness.go          # NEW: convertWithAwk, awk embedding, TAP→CRAP-2 streaming
  awkharness_test.go     # NEW: Go tests for harness with mock TAP input
  git.go                 # MODIFY: delete PhaseParser/Phase types, classify* funcs,
                         #   replace convertWithPhases calls with convertWithAwk
  git_test.go            # MODIFY: delete classifier/emitPhases tests,
                         #   add convertWithAwk integration tests
  brew.go                # MODIFY: delete classify* funcs, replace convertWithPhases
  brew_test.go           # MODIFY: delete classifier/emit tests
  wrap.go                # MODIFY: delete convertWithPhases, emitPhases
tests/
  awk-scripts.bats       # NEW: bats tests for awk scripts
```

---

## Chunk 1: Foundation

### Task 1: Write the first awk script (git rebase)

Start with the simplest case to establish the awk script pattern.

**Files:**
- Create: `go-crap/awk/git/rebase.awk`
- Create: `go-crap/awk/git/testdata/rebase.input`
- Create: `go-crap/awk/git/testdata/rebase.expected`

- [ ] **Step 1: Create testdata for rebase**

Create `go-crap/awk/git/testdata/rebase.input`:
```
Created autostash: e55899e
Current branch lucid-aspen is up to date.
Applied autostash.
```

Create `go-crap/awk/git/testdata/rebase.expected`:
```
TAP version 14
# Created autostash: e55899e
# Current branch lucid-aspen is up to date.
ok 1 - stash
# Applied autostash.
ok 2 - rebase
1..2
```

- [ ] **Step 2: Verify test fails (awk script doesn't exist yet)**

Run: `awk -f go-crap/awk/git/rebase.awk < go-crap/awk/git/testdata/rebase.input`
Expected: FAIL — file not found

- [ ] **Step 3: Write rebase.awk**

Create `go-crap/awk/git/rebase.awk`:
```awk
BEGIN {
    print "TAP version 14"
    n = 0
    current = ""
    split("", closed)
}

function classify(line) {
    if (line ~ /^Created autostash:/ || line == "Applied autostash." || line ~ /^Dropped refs\/stash/) {
        return "stash"
    }
    if (line ~ /^Current branch / || line ~ /^Updating / || line == "Fast-forward" || line ~ /^Successfully rebased/ || line ~ /^Applying: / || line ~ /^Rebasing \(/ || line ~ /^CONFLICT / || line ~ /^Auto-merging / || line == "Already up to date.") {
        return "rebase"
    }
    if ((line ~ /^ .+\|/) || line ~ /files? changed/ || line ~ /insertion/ || line ~ /deletion/) {
        return "summary"
    }
    return ""
}

function emit_phase() {
    if (current != "") {
        n++
        printf "ok %d - %s\n", n, current
        closed[current] = 1
    }
}

{
    line = $0
    sub(/^[[:space:]]+/, "", line)
    phase = classify(line)

    printf "# %s\n", $0

    if (phase != "" && phase != current && !(phase in closed)) {
        emit_phase()
        current = phase
    }
}

END {
    emit_phase()
    printf "1..%d\n", n
}
```

- [ ] **Step 4: Run awk script and diff against expected**

Run: `awk -f go-crap/awk/git/rebase.awk < go-crap/awk/git/testdata/rebase.input | diff - go-crap/awk/git/testdata/rebase.expected`
Expected: no diff

Note: the awk script emits the comment FIRST, then checks for phase transition.
This means when "Current branch..." arrives, its comment is printed, then
`ok 1 - stash` is emitted (closing the previous phase). This produces the
correct ordering where all comments for a phase appear before its test point.

- [ ] **Step 5: Commit**

```
git add go-crap/awk/git/rebase.awk go-crap/awk/git/testdata/
git commit -m "feat: add git rebase awk phase classifier

Emits TAP with stash/rebase/summary phases. Comments for status line.
Closed phases are not reopened."
```

### Task 2: Write remaining git awk scripts

Use the same pattern established in Task 1.

**Files:**
- Create: `go-crap/awk/git/pull.awk`
- Create: `go-crap/awk/git/push.awk`
- Create: `go-crap/awk/git/clone.awk`
- Create: `go-crap/awk/git/fetch.awk`
- Create: `go-crap/awk/git/testdata/pull.input`
- Create: `go-crap/awk/git/testdata/pull.expected`
- Create: `go-crap/awk/git/testdata/push.input`
- Create: `go-crap/awk/git/testdata/push.expected`
- Create: `go-crap/awk/git/testdata/clone.input`
- Create: `go-crap/awk/git/testdata/clone.expected`
- Create: `go-crap/awk/git/testdata/fetch.input`
- Create: `go-crap/awk/git/testdata/fetch.expected`

- [ ] **Step 1: Create testdata for all git subcommands**

`go-crap/awk/git/testdata/pull.input`:
```
remote: Enumerating objects: 5, done.
remote: Counting objects: 100% (5/5), done.
Receiving objects: 100% (3/3), done.
Resolving deltas: 100% (2/2), done.
Unpacking objects: 100% (3/3), done.
Updating abc1234..def5678
Fast-forward
 file.go | 3 +++
 1 file changed, 3 insertions(+)
```

`go-crap/awk/git/testdata/pull.expected`:
```
TAP version 14
# remote: Enumerating objects: 5, done.
# remote: Counting objects: 100% (5/5), done.
# Receiving objects: 100% (3/3), done.
# Resolving deltas: 100% (2/2), done.
ok 1 - fetch
# Unpacking objects: 100% (3/3), done.
ok 2 - unpack
# Updating abc1234..def5678
# Fast-forward
ok 3 - merge
#  file.go | 3 +++
#  1 file changed, 3 insertions(+)
ok 4 - summary
1..4
```

`go-crap/awk/git/testdata/push.input`:
```
Enumerating objects: 5, done.
Counting objects: 100% (5/5), done.
Delta compression using up to 10 threads
Compressing objects: 100% (3/3), done.
Writing objects: 100% (3/3), 1.23 KiB | 1.23 MiB/s, done.
Total 3 (delta 2), reused 0 (delta 0), pack-reused 0
To github.com:org/repo.git
   abc1234..def5678  main -> main
```

`go-crap/awk/git/testdata/push.expected`:
```
TAP version 14
# Enumerating objects: 5, done.
# Counting objects: 100% (5/5), done.
# Delta compression using up to 10 threads
# Compressing objects: 100% (3/3), done.
# Writing objects: 100% (3/3), 1.23 KiB | 1.23 MiB/s, done.
ok 1 - pack
# Total 3 (delta 2), reused 0 (delta 0), pack-reused 0
# To github.com:org/repo.git
ok 2 - transfer
#    abc1234..def5678  main -> main
ok 3 - summary
1..3
```

`go-crap/awk/git/testdata/clone.input`:
```
Cloning into 'repo'...
remote: Enumerating objects: 100, done.
remote: Counting objects: 100% (100/100), done.
Receiving objects: 100% (100/100), 1.23 MiB | 5.00 MiB/s, done.
Resolving deltas: 100% (20/20), done.
```

`go-crap/awk/git/testdata/clone.expected`:
```
TAP version 14
# Cloning into 'repo'...
ok 1 - init
# remote: Enumerating objects: 100, done.
# remote: Counting objects: 100% (100/100), done.
# Receiving objects: 100% (100/100), 1.23 MiB | 5.00 MiB/s, done.
ok 2 - receive
# Resolving deltas: 100% (20/20), done.
ok 3 - resolve
1..3
```

`go-crap/awk/git/testdata/fetch.input`:
```
remote: Enumerating objects: 5, done.
remote: Counting objects: 100% (5/5), done.
Receiving objects: 100% (3/3), done.
Resolving deltas: 100% (2/2), done.
From github.com:org/repo
   abc1234..def5678  main       -> origin/main
```

`go-crap/awk/git/testdata/fetch.expected`:
```
TAP version 14
# remote: Enumerating objects: 5, done.
# remote: Counting objects: 100% (5/5), done.
ok 1 - negotiate
# Receiving objects: 100% (3/3), done.
ok 2 - receive
# Resolving deltas: 100% (2/2), done.
ok 3 - resolve
# From github.com:org/repo
#    abc1234..def5678  main       -> origin/main
1..3
```

Note: for fetch, "From" and ref-update lines appear AFTER resolve. They
classify as "negotiate" but that phase is already closed, so they appear
as comments only (no new test point). The plan remains `1..3`.

- [ ] **Step 2: Write pull.awk, push.awk, clone.awk, fetch.awk**

Each follows the same structure as `rebase.awk` — BEGIN prints version, `classify()` maps patterns, main block emits comments and detects phase transitions, END emits final test point and plan.

**`pull.awk` classify function:**
```awk
function classify(line) {
    if (line ~ /^remote: / || line ~ /^Receiving objects:/ || line ~ /^Resolving deltas:/ || line ~ /^From / || (line ~ /^ / && line ~ /->/ && line ~ /origin\//)) {
        return "fetch"
    }
    if (line ~ /^Unpacking objects:/) {
        return "unpack"
    }
    if (line ~ /^Updating / || line == "Fast-forward" || line == "Already up to date." || line ~ /^Merge made by/) {
        return "merge"
    }
    if ((line ~ /^ .+\|/) || line ~ /files? changed/ || line ~ /insertion/ || line ~ /deletion/) {
        return "summary"
    }
    return ""
}
```

**`push.awk` classify function:**
```awk
function classify(line) {
    if (line ~ /^Enumerating objects:/ || line ~ /^Counting objects:/ || line ~ /^Delta compression/ || line ~ /^Compressing objects:/ || line ~ /^Writing objects:/) {
        return "pack"
    }
    if (line ~ /^Total / || line ~ /^To /) {
        return "transfer"
    }
    if ((line ~ /^ / && line ~ /->/) || line ~ /\[new branch\]/ || line ~ /\[new tag\]/) {
        return "summary"
    }
    return ""
}
```

**`clone.awk` classify function:**
```awk
function classify(line) {
    if (line ~ /^Cloning into /) {
        return "init"
    }
    if (line ~ /^remote: / || line ~ /^Receiving objects:/) {
        return "receive"
    }
    if (line ~ /^Resolving deltas:/) {
        return "resolve"
    }
    if (line ~ /^Updating files:/ || line ~ /^Checking out files:/) {
        return "checkout"
    }
    return ""
}
```

**`fetch.awk` classify function:**
```awk
function classify(line) {
    if (line ~ /^remote: / || line ~ /^From / || (line ~ /^ / && line ~ /->/)) {
        return "negotiate"
    }
    if (line ~ /^Receiving objects:/) {
        return "receive"
    }
    if (line ~ /^Resolving deltas:/) {
        return "resolve"
    }
    return ""
}
```

The rest of each script (BEGIN, main block, END) is identical to `rebase.awk`.

- [ ] **Step 3: Verify all awk scripts against testdata**

Run for each: `awk -f go-crap/awk/git/<name>.awk < go-crap/awk/git/testdata/<name>.input | diff - go-crap/awk/git/testdata/<name>.expected`

All should produce no diff.

- [ ] **Step 4: Commit**

```
git add go-crap/awk/git/
git commit -m "feat: add awk phase classifiers for git pull/push/clone/fetch"
```

### Task 3: Write brew awk scripts

**Files:**
- Create: `go-crap/awk/brew/install.awk`
- Create: `go-crap/awk/brew/upgrade.awk`
- Create: `go-crap/awk/brew/update.awk`
- Create: `go-crap/awk/brew/tap.awk`
- Create: `go-crap/awk/brew/testdata/install.input`
- Create: `go-crap/awk/brew/testdata/install.expected`
- Create: `go-crap/awk/brew/testdata/upgrade.input`
- Create: `go-crap/awk/brew/testdata/upgrade.expected`
- Create: `go-crap/awk/brew/testdata/update.input`
- Create: `go-crap/awk/brew/testdata/update.expected`
- Create: `go-crap/awk/brew/testdata/tap.input`
- Create: `go-crap/awk/brew/testdata/tap.expected`

- [ ] **Step 1: Create testdata**

`go-crap/awk/brew/testdata/install.input`:
```
==> Fetching jq
==> Downloading https://ghcr.io/v2/homebrew/core/jq/manifests/1.7.1
==> Installing jq
==> Pouring jq--1.7.1.arm64_sonoma.bottle.tar.gz
==> Linking jq
```

`go-crap/awk/brew/testdata/install.expected`:
```
TAP version 14
# ==> Fetching jq
# ==> Downloading https://ghcr.io/v2/homebrew/core/jq/manifests/1.7.1
ok 1 - download
# ==> Installing jq
# ==> Pouring jq--1.7.1.arm64_sonoma.bottle.tar.gz
ok 2 - install
# ==> Linking jq
ok 3 - link
1..3
```

`go-crap/awk/brew/testdata/upgrade.input` (multi-package):
```
==> Upgrading foo
==> Downloading https://example.com/foo-1.1.tar.gz
==> Pouring foo-1.1.arm64_sonoma.bottle.tar.gz
==> Upgrading bar
==> Downloading https://example.com/bar-2.1.tar.gz
==> Pouring bar-2.1.arm64_sonoma.bottle.tar.gz
```

`go-crap/awk/brew/testdata/upgrade.expected`:
```
TAP version 14
# ==> Upgrading foo
    # ==> Downloading https://example.com/foo-1.1.tar.gz
    ok 1 - download
    # ==> Pouring foo-1.1.arm64_sonoma.bottle.tar.gz
    ok 2 - install
    1..2
ok 1 - foo
# ==> Upgrading bar
    # ==> Downloading https://example.com/bar-2.1.tar.gz
    ok 1 - download
    # ==> Pouring bar-2.1.arm64_sonoma.bottle.tar.gz
    ok 2 - install
    1..2
ok 2 - bar
1..2
```

`go-crap/awk/brew/testdata/update.input`:
```
==> Fetching newest version of Homebrew...
remote: Enumerating objects: 100, done.
==> Updated 2 taps (homebrew/core and homebrew/cask).
==> New Formulae
```

`go-crap/awk/brew/testdata/update.expected`:
```
TAP version 14
# ==> Fetching newest version of Homebrew...
# remote: Enumerating objects: 100, done.
ok 1 - fetch
# ==> Updated 2 taps (homebrew/core and homebrew/cask).
# ==> New Formulae
ok 2 - update
1..2
```

`go-crap/awk/brew/testdata/tap.input`:
```
==> Tapping homebrew/cask
Cloning into '/opt/homebrew/Library/Taps/homebrew/homebrew-cask'...
remote: Enumerating objects: 100, done.
==> Tapped 1 command and 4000 casks (4,123 files, 300MB).
```

`go-crap/awk/brew/testdata/tap.expected`:
```
TAP version 14
# ==> Tapping homebrew/cask
# Cloning into '/opt/homebrew/Library/Taps/homebrew/homebrew-cask'...
# remote: Enumerating objects: 100, done.
ok 1 - clone
# ==> Tapped 1 command and 4000 casks (4,123 files, 300MB).
ok 2 - install
1..2
```

- [ ] **Step 2: Write install.awk, update.awk, tap.awk**

These are single-stream scripts (same pattern as git scripts).

**`install.awk` classify function:**
```awk
function classify(line) {
    if (line ~ /^==> Downloading/ || line ~ /^==> Fetching/ || line ~ /^Already downloaded:/) {
        return "download"
    }
    if (line ~ /^==> Installing/ || line ~ /^==> Pouring/) {
        return "install"
    }
    if (line ~ /^==> Linking/) {
        return "link"
    }
    if (line ~ /^==> Caveats/) {
        return "caveats"
    }
    return ""
}
```

**`update.awk` and `tap.awk`:** similar single-stream classify functions
matching the patterns from the current Go classifiers.

- [ ] **Step 3: Write upgrade.awk (multi-package)**

This script is different — it detects package boundaries (`==> Upgrading <name>`)
and emits indented TAP subtests.

```awk
BEGIN {
    print "TAP version 14"
    pkg_n = 0
    phase_n = 0
    current_phase = ""
    current_pkg = ""
    in_pkg = 0
    split("", closed)
}

function classify(line) {
    if (line ~ /^==> Downloading/ || line ~ /^==> Fetching/ || line ~ /^Already downloaded:/) {
        return "download"
    }
    if (line ~ /^==> Installing/ || line ~ /^==> Pouring/) {
        return "install"
    }
    if (line ~ /^==> Linking/) {
        return "link"
    }
    if (line ~ /^==> Caveats/) {
        return "caveats"
    }
    return ""
}

function close_phase() {
    if (current_phase != "") {
        phase_n++
        printf "    ok %d - %s\n", phase_n, current_phase
        closed[current_phase] = 1
    }
}

function close_pkg() {
    if (in_pkg) {
        close_phase()
        printf "    1..%d\n", phase_n
        pkg_n++
        printf "ok %d - %s\n", pkg_n, current_pkg
        phase_n = 0
        current_phase = ""
        split("", closed)
        in_pkg = 0
    }
}

/^==> Upgrading / {
    close_pkg()
    current_pkg = $0
    sub(/^==> Upgrading /, "", current_pkg)
    in_pkg = 1
    printf "# %s\n", $0
    next
}

{
    if (in_pkg) {
        line = $0
        sub(/^[[:space:]]+/, "", line)
        phase = classify(line)

        if (phase != "" && phase != current_phase && !(phase in closed)) {
            close_phase()
            current_phase = phase
        }

        printf "    # %s\n", $0
    } else {
        printf "# %s\n", $0
    }
}

END {
    close_pkg()
    printf "1..%d\n", pkg_n
}
```

- [ ] **Step 4: Verify all brew awk scripts against testdata**

Run for each: `awk -f go-crap/awk/brew/<name>.awk < go-crap/awk/brew/testdata/<name>.input | diff - go-crap/awk/brew/testdata/<name>.expected`

- [ ] **Step 5: Commit**

```
git add go-crap/awk/brew/
git commit -m "feat: add awk phase classifiers for brew install/upgrade/update/tap

upgrade.awk handles multi-package boundaries with TAP subtests."
```

### Task 4: Add bats tests for awk scripts

**Files:**
- Create: `tests/awk-scripts.bats`

- [ ] **Step 1: Write bats tests**

Create `tests/awk-scripts.bats`:
```bash
#!/usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
}

# --- git awk scripts ---

function git_rebase_awk_emits_correct_tap { # @test
  run awk -f go-crap/awk/git/rebase.awk < go-crap/awk/git/testdata/rebase.input
  assert_success
  diff <(echo "$output") go-crap/awk/git/testdata/rebase.expected
}

function git_pull_awk_emits_correct_tap { # @test
  run awk -f go-crap/awk/git/pull.awk < go-crap/awk/git/testdata/pull.input
  assert_success
  diff <(echo "$output") go-crap/awk/git/testdata/pull.expected
}

function git_push_awk_emits_correct_tap { # @test
  run awk -f go-crap/awk/git/push.awk < go-crap/awk/git/testdata/push.input
  assert_success
  diff <(echo "$output") go-crap/awk/git/testdata/push.expected
}

function git_clone_awk_emits_correct_tap { # @test
  run awk -f go-crap/awk/git/clone.awk < go-crap/awk/git/testdata/clone.input
  assert_success
  diff <(echo "$output") go-crap/awk/git/testdata/clone.expected
}

function git_fetch_awk_emits_correct_tap { # @test
  run awk -f go-crap/awk/git/fetch.awk < go-crap/awk/git/testdata/fetch.input
  assert_success
  diff <(echo "$output") go-crap/awk/git/testdata/fetch.expected
}

# --- brew awk scripts ---

function brew_install_awk_emits_correct_tap { # @test
  run awk -f go-crap/awk/brew/install.awk < go-crap/awk/brew/testdata/install.input
  assert_success
  diff <(echo "$output") go-crap/awk/brew/testdata/install.expected
}

function brew_upgrade_awk_emits_correct_tap { # @test
  run awk -f go-crap/awk/brew/upgrade.awk < go-crap/awk/brew/testdata/upgrade.input
  assert_success
  diff <(echo "$output") go-crap/awk/brew/testdata/upgrade.expected
}

function brew_update_awk_emits_correct_tap { # @test
  run awk -f go-crap/awk/brew/update.awk < go-crap/awk/brew/testdata/update.input
  assert_success
  diff <(echo "$output") go-crap/awk/brew/testdata/update.expected
}

function brew_tap_awk_emits_correct_tap { # @test
  run awk -f go-crap/awk/brew/tap.awk < go-crap/awk/brew/testdata/tap.input
  assert_success
  diff <(echo "$output") go-crap/awk/brew/testdata/tap.expected
}
```

- [ ] **Step 2: Run bats tests**

Run: `bats --no-sandbox --tap tests/awk-scripts.bats`
Expected: all 9 tests pass

- [ ] **Step 3: Commit**

```
git add tests/awk-scripts.bats
git commit -m "test: add bats tests for awk phase classifiers"
```

---

## Chunk 2: Go Harness

### Task 5: Build the Go harness (`convertWithAwk`)

**Files:**
- Create: `go-crap/awkharness.go`
- Create: `go-crap/awkharness_test.go`

- [ ] **Step 1: Write test for harness with simple TAP input**

Create `go-crap/awkharness_test.go`:
```go
package crap

import (
	"bytes"
	"strings"
	"testing"
)

func TestConvertTAPToCRAP(t *testing.T) {
	tap := "TAP version 14\n# status line text\nok 1 - stash\nok 2 - rebase\n1..2\n"
	var buf bytes.Buffer

	convertTAPToCRAP(strings.NewReader(tap), &buf, false)

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "CRAP-2") {
		t.Errorf("expected CRAP-2 header, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 1 - stash") {
		t.Errorf("expected stash test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - rebase") {
		t.Errorf("expected rebase test point, got:\n%s", out)
	}
	if !strings.Contains(out, "1::2") {
		t.Errorf("expected plan 1::2, got:\n%s", out)
	}
	if strings.Contains(out, "TAP version") {
		t.Errorf("TAP version line should be consumed, got:\n%s", out)
	}
}

func TestConvertTAPToCRAPSkipsComments(t *testing.T) {
	tap := "TAP version 14\n# hello world\nok 1 - test\n1..1\n"
	var buf bytes.Buffer

	convertTAPToCRAP(strings.NewReader(tap), &buf, false)

	out := buf.String()
	// Comments should NOT appear in non-color output (they drive status line only)
	if strings.Contains(out, "# hello world") {
		t.Errorf("bare comment should not appear in output without color/TTY, got:\n%s", out)
	}
}

func TestConvertTAPToCRAPWithSubtests(t *testing.T) {
	tap := "TAP version 14\n    # status\n    ok 1 - download\n    ok 2 - install\n    1..2\nok 1 - foo\n1..1\n"
	var buf bytes.Buffer

	convertTAPToCRAP(strings.NewReader(tap), &buf, false)

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - foo") {
		t.Errorf("expected top-level test point, got:\n%s", out)
	}
	if !strings.Contains(out, "1::1") {
		t.Errorf("expected plan 1::1, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-crap && nix develop ../ --command go test -run TestConvertTAPToCRAP ./...`
Expected: FAIL — `convertTAPToCRAP` not defined

- [ ] **Step 3: Write `convertTAPToCRAP` and `convertWithAwk`**

Create `go-crap/awkharness.go`:
```go
package crap

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

//go:embed awk/git/*.awk
var gitAwkScripts embed.FS

//go:embed awk/brew/*.awk
var brewAwkScripts embed.FS

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

// convertTAPToCRAP reads TAP from r and writes CRAP-2 to w.
// TAP version line is consumed. Bare comments are suppressed (they drive
// the status line in the full harness, not written to output).
// Test points are re-emitted via Writer. Plan lines trigger Writer.Plan().
func convertTAPToCRAP(r io.Reader, w io.Writer, color bool) {
	tw := NewColorWriter(w, color)
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()

		// Determine indentation
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		depth := indent / 4

		// Skip TAP version lines
		if strings.HasPrefix(trimmed, "TAP version") {
			continue
		}

		// Skip bare comments (status line — handled elsewhere in streaming mode)
		if strings.HasPrefix(trimmed, "#") && depth == 0 {
			continue
		}
		// Skip indented comments too (subtest status lines)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Plan line
		if strings.HasPrefix(trimmed, "1..") && depth == 0 {
			tw.Plan()
			continue
		}

		// Test points
		if strings.HasPrefix(trimmed, "ok ") || strings.HasPrefix(trimmed, "not ok ") {
			tp, _ := parseTestPoint(trimmed)
			if depth == 0 {
				if tp.OK {
					tw.Ok(tp.Description)
				} else {
					tw.NotOk(tp.Description, nil)
				}
			}
			continue
		}
	}
}

// convertWithAwk runs a command, pipes its output through an embedded awk
// script, reads the resulting TAP, and emits CRAP-2 with spinner/status line.
func convertWithAwk(ctx context.Context, binPath string, args []string, w io.Writer, awkScript string, color bool, toolName string) int {
	tw := NewColorWriter(w, color)
	if color {
		tw.EnableTTYBuildLastLine()
	}
	spinner := newStatusSpinner()

	// Verify awk is available
	if _, err := exec.LookPath("awk"); err != nil {
		tw.BailOut("awk not found in PATH")
		return 1
	}

	// Write awk script to temp file
	tmpFile, err := os.CreateTemp("", "crap-*.awk")
	if err != nil {
		tw.BailOut(fmt.Sprintf("failed to create temp awk script: %v", err))
		return 1
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(awkScript); err != nil {
		tw.BailOut(fmt.Sprintf("failed to write awk script: %v", err))
		return 1
	}
	tmpFile.Close()

	// Build pipeline: command | awk -f script
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

	awkCmd := exec.CommandContext(ctx, "awk", "-f", tmpFile.Name())
	// Merge command's stdout+stderr into awk's stdin
	awkStdin, err := awkCmd.StdinPipe()
	if err != nil {
		tw.BailOut(fmt.Sprintf("failed to create awk stdin pipe: %v", err))
		return 1
	}
	awkStdout, err := awkCmd.StdoutPipe()
	if err != nil {
		tw.BailOut(fmt.Sprintf("failed to create awk stdout pipe: %v", err))
		return 1
	}

	if err := cmd.Start(); err != nil {
		tw.BailOut(fmt.Sprintf("failed to start %s: %v", toolName, err))
		return 1
	}
	if err := awkCmd.Start(); err != nil {
		tw.BailOut(fmt.Sprintf("failed to start awk: %v", err))
		return 1
	}

	// Goroutine: merge cmd stdout+stderr into awk stdin
	var mergeWg sync.WaitGroup
	mergeWg.Add(2)
	go func() {
		defer mergeWg.Done()
		io.Copy(awkStdin, cmdStdout)
	}()
	go func() {
		defer mergeWg.Done()
		io.Copy(awkStdin, cmdStderr)
	}()
	go func() {
		mergeWg.Wait()
		awkStdin.Close()
	}()

	// Read TAP from awk stdout, emit CRAP-2
	var mu sync.Mutex
	var lastContent string
	stopTicker := startStatusTicker(tw, spinner, &mu, &lastContent)

	hasTestPoints := false
	scanner := bufio.NewScanner(awkStdout)
	for scanner.Scan() {
		line := scanner.Text()

		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)

		// Skip TAP version
		if strings.HasPrefix(trimmed, "TAP version") {
			continue
		}

		// Bare comments → status line
		if strings.HasPrefix(trimmed, "#") {
			comment := strings.TrimPrefix(trimmed, "# ")
			if indent == 0 {
				mu.Lock()
				lastContent = comment
				spinner.Touch()
				tw.UpdateLastLine(spinner.prefix() + comment)
				mu.Unlock()
			}
			continue
		}

		// Plan line
		if strings.HasPrefix(trimmed, "1..") {
			mu.Lock()
			tw.FinishLastLine()
			tw.Plan()
			mu.Unlock()
			continue
		}

		// Test points
		if strings.HasPrefix(trimmed, "ok ") || strings.HasPrefix(trimmed, "not ok ") {
			tp, _ := parseTestPoint(trimmed)
			mu.Lock()
			tw.FinishLastLine()
			if tp.OK {
				tw.Ok(tp.Description)
			} else {
				tw.NotOk(tp.Description, nil)
			}
			mu.Unlock()
			hasTestPoints = true
			continue
		}
	}

	stopTicker()
	tw.FinishLastLine()

	// Wait for command to finish
	cmdErr := cmd.Wait()
	awkCmd.Wait()

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

	// If command failed but awk didn't emit any not-ok, add one
	if exitCode != 0 && !tw.HasFailures() {
		if hasTestPoints {
			tw.NotOk(toolName+" failed", map[string]string{
				"exit-code": fmt.Sprintf("%d", exitCode),
			})
		} else {
			tw.NotOk(toolName+" "+strings.Join(args, " "), map[string]string{
				"exit-code": fmt.Sprintf("%d", exitCode),
			})
		}
		tw.Plan()
	}

	return exitCode
}
```

- [ ] **Step 4: Run tests**

Run: `cd go-crap && nix develop ../ --command go test -run TestConvertTAPToCRAP ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```
git add go-crap/awkharness.go go-crap/awkharness_test.go
git commit -m "feat: add convertWithAwk harness for TAP→CRAP-2 streaming

Embeds awk scripts via go:embed. Pipes command output through awk,
reads TAP, emits CRAP-2 with spinner and status line support."
```

### Task 6: Wire up git.go and brew.go to use awk harness

**Files:**
- Modify: `go-crap/git.go`
- Modify: `go-crap/brew.go`
- Modify: `go-crap/wrap.go`
- Modify: `go-crap/git_test.go`
- Modify: `go-crap/brew_test.go`

- [ ] **Step 1: Replace `ConvertGit` to use awk harness**

In `go-crap/git.go`:
- Delete `PhaseParser`, `Phase` types
- Delete all `NewGit*Parser()` constructors
- Delete all `classify*Line()` functions
- Delete `Classify()`, `Feed()`, `Phases()` methods
- Keep `FindGit()`, `parserForSubcommand` (renamed to `awkScriptForSubcommand`)
- Replace `ConvertGit` to call `convertWithAwk` instead of `convertWithPhases`

New `git.go` should be roughly:
```go
package crap

import (
	"context"
	"fmt"
	"io"
)

func FindGit(selfExe string) (string, error) {
	return findBinary(selfExe, "git")
}

func gitAwkScript(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("no subcommand")
	}
	switch args[0] {
	case "pull", "push", "clone", "fetch", "rebase":
		return lookupAwkScript("git", args[0])
	default:
		return "", fmt.Errorf("unrecognized: %s", args[0])
	}
}

func ConvertGit(ctx context.Context, selfExe string, args []string, w io.Writer, stdin io.Reader, stderrW io.Writer, verbose bool, color bool) int {
	gitPath, err := FindGit(selfExe)
	if err != nil {
		fmt.Fprintf(stderrW, "::git: %s\n", err)
		return 1
	}

	script, err := gitAwkScript(args)
	if err != nil {
		return execPassthrough(ctx, gitPath, args, w, stdin, stderrW)
	}

	return convertWithAwk(ctx, gitPath, args, w, script, color, "git")
}
```

- [ ] **Step 2: Replace `ConvertBrew` similarly**

In `go-crap/brew.go`:
- Delete all `NewBrew*Parser()` constructors
- Delete all `classifyBrew*Line()` functions
- Replace `ConvertBrew` to call `convertWithAwk`

- [ ] **Step 3: Delete `convertWithPhases` and `emitPhases` from `wrap.go`**

In `go-crap/wrap.go`, delete:
- `convertWithPhases()` function
- `emitPhases()` function

Keep:
- `findBinary()`
- `execPassthrough()`

- [ ] **Step 4: Update git_test.go**

Delete:
- `TestGitPullPhases`, `TestGitPushPhases`, `TestGitClonePhases`, `TestGitFetchPhases`, `TestGitRebasePhases`
- `TestGitPhaseParserCollectsPhases`, `TestGitPhaseParserSkipsEmptyPhases`
- `TestEmitGitPhasesSuccess`, `TestEmitGitPhasesAlreadyUpToDate`, `TestEmitGitPhasesFailure`
- `TestEmitGitPushPhases`, `TestEmitGitClonePhases`
- `TestEmitGitRebaseUpToDate`, `TestEmitGitRebaseWithSummary`

Keep:
- `TestFindGitSkipsSelf`, `TestFindGitDetectsRecursion`, `TestFindGitSelectsNextCandidate`
- `TestConvertGitPassthroughSuccess`, `TestConvertGitPassthroughFailure`

- [ ] **Step 5: Update brew_test.go**

Delete all tests (they all test the now-removed Go classifiers and emitPhases).

The file can either be deleted entirely or left with just the package declaration.

- [ ] **Step 6: Run all tests**

Run: `just test`
Expected: all Go, Rust, and bats tests pass

- [ ] **Step 7: Commit**

```
git add go-crap/git.go go-crap/brew.go go-crap/wrap.go go-crap/git_test.go go-crap/brew_test.go
git commit -m "refactor: replace Go phase parsers with awk harness

Delete PhaseParser, Phase, all classify functions, emitPhases,
convertWithPhases. ConvertGit and ConvertBrew now use convertWithAwk
which pipes command output through embedded awk scripts."
```

### Task 7: Build and verify

**Files:**
- None (verification only)

- [ ] **Step 1: Nix build**

Run: `nix build --show-trace`
Expected: build succeeds

- [ ] **Step 2: Verify binaries**

Run: `ls result/bin/`
Expected: `::`, `::brew`, `::git`, `large-colon`

- [ ] **Step 3: Run full test suite**

Run: `just test`
Expected: all tests pass (Go + Rust + bats including new awk-scripts.bats)

- [ ] **Step 4: Manual smoke test**

Run: `result/bin/::git version` (passthrough — should print git version, no CRAP-2)

- [ ] **Step 5: Commit if any fixups needed, otherwise done**
