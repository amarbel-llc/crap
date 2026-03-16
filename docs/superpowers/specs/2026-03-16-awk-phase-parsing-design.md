# Awk-Based Phase Parsing Design

**Goal:** Replace Go phase parsers with awk scripts that emit TAP as an
intermediate representation, consumed by a Go harness that emits CRAP-2.

**Context:** The current Go phase parsers (`PhaseParser`, classifiers, etc.)
are pattern matchers on line prefixes — exactly what awk excels at. Moving
classification to awk scripts makes adding new command support trivial (write
an awk script, no Go changes) while keeping CRAP-2 emission, spinners, and
status lines in Go.

## Architecture

```
real-command --> awk script --> TAP stream --> Go harness --> CRAP-2
   (stderr+stdout)              (stdout)                    (stdout)
```

1. crappy-git/crappy-brew recognizes a subcommand
2. Finds the real binary via `findBinary`
3. Runs the real command, piping stdout+stderr through the embedded awk script
4. The awk script emits TAP on stdout
5. The Go harness reads the TAP stream and emits CRAP-2

Unrecognized subcommands continue to use `execPassthrough` (no change).

## Awk Script Location

Scripts live in `go-crap/awk/{git,brew}/<subcommand>.awk` and are embedded
into the Go binary via `//go:embed`. Go's embed directive does not follow
symlinks, so the scripts must live under the Go module directory.

Directory structure:

```
go-crap/awk/
  git/
    pull.awk
    push.awk
    clone.awk
    fetch.awk
    rebase.awk
  brew/
    install.awk
    upgrade.awk
    update.awk
    tap.awk
```

## TAP to CRAP-2 Mapping

| TAP construct | CRAP-2 output |
|---|---|
| `TAP version 14` | Consumed by harness (triggers `CRAP version 2` via `NewColorWriter`) |
| `ok N - description` | `ok N - description` (with color, renumbered by Writer) |
| `not ok N - description` | `not ok N - description` (with color, renumbered by Writer) |
| `1..N` | `1::N` (emitted by `Writer.Plan()`) |
| Indented TAP (subtests) | Indented CRAP-2 subtests (no version line) |
| YAML diagnostics | YAML diagnostics (with ANSI handling) |
| `# comment` (no directive) | Status line update via `UpdateLastLine` |
| `# TODO ...` | TODO directive |
| `# SKIP ...` | SKIP directive |

## Version Line Handling

The Go harness uses `NewColorWriter()` which automatically emits
`CRAP version 2`. The `TAP version 14` line from the awk script is consumed
by the harness as a validity signal and is NOT passed through to output.
The harness skips/discards the TAP version line when reading the stream.

## Awk Script Responsibilities

Each awk script:

- Emits `TAP version 14` in BEGIN
- Reads lines from stdin (the real command's merged stdout+stderr)
- Emits `# <line>` for each input line (drives the status line)
- Tracks the current phase via pattern matching
- Emits `ok N - <phase>` when a phase transition occurs (the previous phase
  completed successfully)
- Emits the final test point and `1..N` at END
- Phases emit in order of first appearance — once a phase is emitted as a
  test point, it stays closed and later lines matching that phase are emitted
  as comments only

### Example: `git/rebase.awk`

For input:
```
Created autostash: e55899e
Current branch lucid-aspen is up to date.
Applied autostash.
```

Emits:
```
TAP version 14
# Created autostash: e55899e
# Current branch lucid-aspen is up to date.
ok 1 - stash
# Applied autostash.
ok 2 - rebase
1..2
```

Phase transitions: "Created autostash" is stash. "Current branch..." is
rebase, which triggers emitting `ok 1 - stash`. "Applied autostash."
classifies as stash but that phase is already closed, so it's emitted as a
comment only. At END, the current phase (rebase) gets its test point.

### Example: `brew/upgrade.awk` (multi-package)

Package boundaries are detected by `==> Upgrading <name>` lines. Each
package becomes a TAP subtest (indented 4 spaces). Phases within each
package follow the same transition rules.

For input:
```
==> Upgrading foo
==> Downloading https://...foo...
==> Pouring foo-1.1.arm64_sonoma.bottle.tar.gz
==> Upgrading bar
==> Downloading https://...bar...
==> Pouring bar-2.1.arm64_sonoma.bottle.tar.gz
```

Emits:
```
TAP version 14
# ==> Upgrading foo
    # ==> Downloading https://...foo...
    ok 1 - download
    # ==> Pouring foo-1.1.arm64_sonoma.bottle.tar.gz
    ok 2 - install
    1..2
ok 1 - foo
# ==> Upgrading bar
    # ==> Downloading https://...bar...
    ok 1 - download
    # ==> Pouring bar-2.1.arm64_sonoma.bottle.tar.gz
    ok 2 - install
    1..2
ok 2 - bar
1..2
```

For `brew install`, the same logic applies — `==> Installing <name>` is the
package boundary.

## Go Harness Responsibilities

The Go harness (`convertWithAwk` or similar):

1. Looks up the embedded awk script for the subcommand
2. Starts the real command
3. Pipes the command's stdout+stderr into `awk -f <script>` (writes awk
   script to a temp file from embedded content)
4. Uses `NewColorWriter()` which emits `CRAP version 2`
5. Reads TAP lines from awk's stdout, processing each as it arrives:
   - `TAP version 14` — skip (already emitted CRAP version)
   - `# comment` (bare, no directive) — `UpdateLastLine(spinner + comment)`
   - `# TODO ...` / `# SKIP ...` — pass directive to test point
   - `ok N - desc` — `tw.Ok(desc)`
   - `not ok N - desc` — `tw.NotOk(desc, ...)`
   - `1..N` — `tw.Plan()` (uses Writer's internal count)
   - Indented lines — delegate to `Writer.Subtest` child writer
   - `---` / `...` — accumulate YAML, attach to next test point
6. After awk exits, checks the real command's exit code
7. If non-zero and no `not ok` was emitted by the awk script, emits
   `not ok` with the exit code as a diagnostic

The harness does NOT batch output — it processes each TAP line as it arrives,
enabling real-time status line updates and streaming test points.

### Spinner and Status Line

The harness uses `statusSpinner` and `startStatusTicker` (same as current
`convertWithPhases`). Bare TAP comments drive `UpdateLastLine` with the
spinner prefix. The ticker keeps the spinner animating between comment lines.

## Exit Code Handling

The Go harness owns exit code handling, not the awk scripts:

1. The harness starts the real command and the awk pipeline
2. The awk script classifies output and emits TAP — it does not know or
   care about exit codes
3. After the pipeline completes, the harness checks the real command's exit
   code via `cmd.Wait()`
4. If exit code is 0: no additional action (awk already emitted `ok` points)
5. If exit code is non-zero:
   - If the awk script emitted any test points, the harness emits a final
     `not ok` test point with `exit-code` diagnostic
   - If the awk script emitted nothing (command failed before producing
     output), the harness emits `not ok 1 - <tool> <subcommand>` with
     exit code and any captured stderr

## Verbosity Levels

Awk scripts always emit full detail — comments for every input line, YAML
diagnostics blocks where appropriate. The Go harness controls what to
display based on verbosity:

- **default:** suppress comments (no status line), suppress subtests, show
  only top-level test points
- **`-v`:** show status line (comments drive spinner), show subtests with
  phase test points
- **`-vv`:** same as `-v` plus render YAML diagnostic blocks on each test
  point

## What Gets Deleted

- `PhaseParser`, `Phase` types
- All `NewGit*Parser()` / `NewBrew*Parser()` constructors
- All `classify*Line()` functions
- `emitPhases()`, `convertWithPhases()`
- Related Go tests for classifiers and phase emission

## What Stays

- `findBinary()`, `execPassthrough()` in `wrap.go`
- `Writer` and all CRAP-2 emission logic in `crap.go`
- `Reader` and reformat logic (may be reused by the harness)
- CLI entry points in `cmd/crappy-git/` and `cmd/crappy-brew/`
- `FindGit()`, `FindBrew()`, subcommand routing in `git.go`/`brew.go`

## Testing

- **Awk scripts:** tested independently via bats by piping sample input
  files and asserting TAP output. Sample inputs stored alongside awk scripts
  in `go-crap/awk/{git,brew}/testdata/`.
- **Go harness:** tested with mock TAP input strings (no real git/brew
  needed). Verifies CRAP-2 output, status line behavior, exit code handling.
- **Integration:** `just capture` recipe captures real command output with
  PTY for verifying awk scripts against real tool behavior.
