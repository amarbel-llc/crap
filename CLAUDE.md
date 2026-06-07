# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with
code in this repository.

## Overview

CRAP (Command Result Accessibility Protocol) is a fork of TAP focused on making
trees of script output easy to visually understand for human consumers. This
repo contains the CRAP-2 specification, amendments, and two implementation
libraries:

- **go-crap** --- Go library + CLI (`large-colon`, aka `::`) for validating,
  converting, and writing CRAP-2 streams. Also home to the **canonical**
  pieces: the `ndjsoncrap` package (the ndjson-crap wire format), the
  `viewport` package (the bubbletea presenter), and the `crap-present` binary
  (`:: present`) that renders an ndjson-crap stream via the viewport.
- **rust-crap** --- Rust library for writing CRAP-2 (text profile) streams

## Canonical format & presenter (ndjson-crap + viewport)

CRAP-2's **canonical wire format** is **ndjson-crap** (newline-delimited JSON,
specified in `docs/ndjson-crap-schema.md`) and its **canonical presenter** is
the **viewport**. ndjson-crap is a superset that dries up the ecosystem's
divergent ndjson-tap schemas: tap-dancer's `tap-ndjson(7)` result model
(`plan`/`test`/`bailout`/`summary`) and just-us's `--events-fd` execution
model (`node_start`/`command`/`output`/`node_end`, accepted via its
`recipe_*` aliases). Go consumers import `go-crap/ndjsoncrap` +
`go-crap/viewport`; non-Go producers (just-us, piggy) pipe their stream into
the `crap-present` binary.

`:: present` **delegates to the `crap-present` binary** rather than importing
the viewport: bubbletea's `init()` probes the terminal (OSC 11) for any
process that imports it, which must not happen for the general-purpose `::`
subcommands. Keep `cmd/large-colon` free of bubbletea.

The line-oriented CRAP-2 **text profile** (the `Writer`/`Reader` in `crap.go`
/ `reader.go`, mirrored by rust-crap and exercised by the bats suites) is now
**legacy**. Retargeting the converters (`gotest`/`cargotest`/`execparallel`)
to emit ndjson-crap and retiring the text core is a tracked, incremental
migration — do not assume it is done.

## Build & Test

``` sh
just build          # nix build --show-trace (builds large-colon + rust-crap)
just test           # run all tests (Go + Rust)
just test-go        # Go tests only (cd go-crap && go test ./...)
just test-cargo     # Rust tests only (cargo test)

just codemod-fmt    # format all code (Go + Rust + Nix)
just run-nix <args> # run large-colon via nix run
```

## Architecture

### Go library (`go-crap/`)

The Go module (`github.com/amarbel-llc/crap/go-crap`) is both a library and the
source for the `large-colon` CLI (binary name `::` in usage). Key files:

- `crap.go` --- `Writer` type: core CRAP-2 stream writer with color, locale
  formatting, subtests, streamed output, and status line support
- `reader.go` --- `Reader` type: CRAP-2 parser producing diagnostics and summary
- `parse.go` --- Low-level line parsing (plans, test points, directives)
- `classify.go` --- Line classification for the parser
- `diagnostic.go` --- Diagnostic types (severity, rules) for validation
- `gotest.go` --- Converts `go test -json` output to CRAP-2
- `cargotest.go` --- Converts `cargo test` output to CRAP-2
- `reformat.go` --- Reads TAP/CRAP from stdin, emits colorized CRAP-2
- `execparallel.go` --- Parallel command execution with CRAP-2 output
- `cmd/large-colon/main.go` --- CLI entry point with subcommands: `validate`,
  `go-test`, `cargo-test`, `reformat`, `exec`, `exec-parallel`

### Rust library (`rust-crap/`)

`CrapWriter` with builder pattern (`CrapWriterBuilder`), supporting color, ICU
locale formatting, subtests, YAML diagnostics, status line, and streamed output.
Library only --- no binary.

### Specification

The CRAP-2 spec lives in `crap-version-2-specification.md`. Amendments in
separate files extend the base spec with ANSI display hints, ANSI in YAML
output, locale number formatting, status line, streamed output, and subtest YAML
rewriting.

## Nix Flake

Uses the standard stable-first nixpkgs convention (see parent `eng` CLAUDE.md).
DevShell combines Go, Rust, and shell devenvs.

## `::` Responsibility Model

There are two presentation paths:

- **`:: present`** (canonical) — the utility emits **ndjson-crap** and `::
  present` (which delegates to `crap-present`) renders it via the viewport.
  This is the path new producers should target.
- **`:: <utility>` / `:: reformat`** (legacy text profile) — described below;
  the utility emits TAP-14/CRAP text and `::` reformats it into the CRAP-2
  text profile with improved UX.

`:: <utility>` expects the utility to emit **TAP-14** (or CRAP-2) on stdout.
`::` reformats this into CRAP-2 with improved UX (colorization, status line,
spinner). The responsibility for producing well-formed TAP belongs to the
**utility**, not to `::`.

- **Utility emits TAP-14** → `::` reformats into CRAP-2 (happy path)
- **Utility has no TAP support** → an awk fallback script in
  `go-crap/awk/<tool>/` transforms tool-specific output into TAP, which `::`
  then reformats
- **No awk fallback exists** → `::` wraps the entire output as a single opaque
  test point

If a utility's output is not being reformatted correctly, the fix almost always
belongs in the utility (or in a new awk fallback), **not** in `::` itself. Do
not add heuristic TAP detection or line-scanning to `RunWithPTYReformat`.

## Key Conventions

- The CLI binary is named `large-colon` in Nix but its usage text shows `::` as
  the command name
- CRAP-2 version line is `CRAP version 2` (not TAP version 14)
- All pragmas defined by CRAP-2 are **enabled by default** (unlike TAP-14 where
  they require opt-in) --- pragma lines primarily disable features
- Subtests are indented 4 spaces; YAML diagnostics 2 spaces relative to their
  test point
- GPG signing is required for commits
- `TODO.md` is a symlink to `TODOODOO.md` --- yes, this is hilarious
