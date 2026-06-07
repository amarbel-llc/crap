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
/ `reader.go`) is now **retired** — deleted from go-crap (along with the
PTY-reformat path, the awk fallbacks, and the `crappy-git`/`brew`/`direnv`
wrappers) and replaced in rust-crap (the old text-profile `CrapWriter` is gone;
`NdjsonCrapWriter` emits ndjson-crap). Both the Go converters and the Rust
library now emit ndjson-crap. The text-format bats suites were removed;
re-adding ndjson-crap-oriented integration tests is the remaining work (bats
and nix are unavailable in the dev container, so they were not verified here).

## Build & Test

``` sh
just                # default: validate lint build test (the CI/merge gate)
just build          # regenerate gomod2nix.toml + nix build --show-trace
just test           # run all tests (Go + Rust)
just test-go        # Go tests only (cd go-crap && go test ./...)
just test-cargo     # Rust tests only (cargo test)

just lint-fmt       # read-only format/lint gate (conformist check)
just codemod-fmt    # format all code via conformist (Go/Nix/Rust/shell)
just run-nix <args> # run large-colon via nix run

just release <ver>  # bump version.env, commit, tag go-crap/v<ver>, gh release
```

Formatting/linting is driven by **conformist** (the treefmt successor);
config lives in `./conformist.toml`, wired as the flake `formatter`
(`nix fmt`) and gated by `just lint-fmt`.

`version.env` (`CRAP_VERSION`) is the single version source of truth
(eng-versioning(7)): flake.nix reads it for all three derivations, and the
fork's `buildGoApplication` burns it into the Go binaries as
`-X main.version` (commit from the flake rev). `:: version` /
`crap-present --version` print `<version>+<commit>`.

## Architecture

### Go library (`go-crap/`)

The Go module (`github.com/amarbel-llc/crap/go-crap`) is both a library and the
source for the `large-colon` (`::`) and `crap-present` CLIs. It is built
around ndjson-crap + the viewport. Key packages/files:

- `ndjsoncrap/` --- the canonical ndjson-crap wire format: tolerant
  `Reader`/`Writer` and the record types (`Meta`, `Plan`, `Test`, `Bailout`,
  `Summary`, `NodeStart`, `Command`, `Output`, `NodeEnd`, `Unknown`).
- `viewport/` --- the bubbletea presenter: `Model` + messages + a `Driver`
  that turns ndjson-crap records into viewport messages, plus `Present()`
  with a plain non-TTY fallback.
- `presentcli/` --- shared CLI glue for the presenter.
- `produce.go` --- the shared result-stream emitter (`writeResultStream` +
  summary tally) for the ndjson-crap producers.
- `gotest.go` / `cargotest.go` --- `ConvertGoTest` / `ConvertCargoTest`: turn
  `go test -json` / `cargo test` output into ndjson-crap (packages/suites are
  top-level test records; tests nest as subtests).
- `exec.go` --- `ConvertExec`: run a command, emit execution-family records
  (`node_start`/`output`/`node_end`).
- `cmd/large-colon/main.go` --- the `::` CLI: producer subcommands (`go-test`,
  `cargo-test`, `exec`, `validate`) plus `present`/`reformat`/no-args, which
  delegate to `crap-present`.
- `cmd/crap-present/main.go` --- the standalone viewport presenter.

### Rust library (`rust-crap/`)

`NdjsonCrapWriter` --- a direct producer of result-family ndjson-crap (plan /
test / bailout / summary), field-compatible with `tap-ndjson(7)`. Built on
serde/serde_json; no color/locale/status-line (those are the viewport's job).
Library only --- no binary.

### Specification

The CRAP-2 spec lives in `crap-version-2-specification.md`. Amendments in
separate files extend the base spec with ANSI display hints, ANSI in YAML
output, locale number formatting, status line, streamed output, and subtest YAML
rewriting.

## Nix Flake

Uses the standard stable-first nixpkgs convention (see parent `eng` CLAUDE.md).
DevShell combines Go, Rust, and shell devenvs (Go via `mkGoEnv` +
the `gomod2nix` CLI).

The Go binaries build with the fork's `buildGoApplication` against
`go-crap/gomod2nix.toml` (no vendoring; regenerate with
`just build-gomod2nix` after dependency changes, or `just update-go` to
tidy + regenerate).

## `::` Responsibility Model

`::` is a set of **ndjson-crap producers** plus a presenter. Every producer
subcommand writes ndjson-crap to stdout; presentation is the viewport's job:

```sh
:: go-test ./... | crap-present      # or: | :: present
```

- **Producers** (`go-test`, `cargo-test`, `exec`) run a tool and emit
  ndjson-crap. A producer owns turning its tool's output into well-formed
  records; the presenter never re-parses tool output.
- **Presenter** (`:: present` / no-args / `reformat`) reads ndjson-crap on
  stdin and renders it via the viewport. `::` delegates to the standalone
  `crap-present` binary rather than importing the viewport, because
  bubbletea's `init()` probes the terminal for any process that imports it
  (keep `cmd/large-colon` bubbletea-free).
- **`validate`** decodes an ndjson-crap stream and reports undecodable records.

The old "wrap any tool, reformat its text output, awk fallback" model is gone;
producers emit the canonical format directly.

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
