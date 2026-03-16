# Design: ::git

## Overview

`::git` is a new binary that wraps `git` output in CRAP-2 format. It finds
`git` in `$PATH`, runs it with the provided arguments, streams output as status
line updates, and emits semantically meaningful test points for supported
commands. Unsupported commands fall back to generic single-test-point behavior.

## Architecture

- New binary source at `go-crap/cmd/crap-git/main.go`
- Same Go module as `large-colon` (`github.com/amarbel-llc/crap/go-crap`)
- Uses the `crap` library for all CRAP-2 output
- Git command parsing lives in `go-crap/git.go` (library code, testable)
- Nix package `crap-git` in `flake.nix`, binary renamed to `::git`

## Behavior

### Generic fallback (all unrecognized commands)

1. Run `git <args>`
2. Stream all output (stdout + stderr) as status line updates
3. On completion, emit one test point: `ok` or `not ok` based on exit code
4. stdout/stderr captured in YAML diagnostics on failure

### `git pull` — semantic phases

Git pull output is parsed into four phases, each becoming a test point:

1. **fetch** — `remote: Enumerating objects`, `remote: Counting objects`,
   `remote: Compressing objects`, `remote: Total ...`, `Receiving objects`,
   `Resolving deltas`. Lines matching these patterns accumulate in phase 1.
2. **unpack** — `Unpacking objects: ...`. Single line typically.
3. **merge** — `Updating abc1234..def5678`, `Fast-forward`, or
   `Already up to date.` The merge strategy/result.
4. **summary** — Diffstat lines (`file.go | 3 +++`) and the final summary
   (`1 file changed, 3 insertions(+)`).

Each phase emits `ok <N> - <description>` when the next phase begins or the
command exits. If a phase produces no output, it is skipped (no empty test
point). All raw lines within a phase stream as status line updates.

On failure (non-zero exit), remaining phases are not emitted; instead a single
`not ok` with the error output in diagnostics.

### `git push` — semantic phases

1. **pack** — `Enumerating objects`, `Counting objects`,
   `Delta compression using up to N threads`, `Compressing objects`,
   `Writing objects`. The local packing phase.
2. **transfer** — `Total N (delta M)`, remote URL line. Transfer to remote.
3. **summary** — Ref update lines (`abc1234..def5678 main -> main`),
   including new branch/tag notifications.

Same rules as pull: phases that produce no output are skipped, failure
short-circuits to a single `not ok`.

## Nix packaging

New `buildGoModule` in `flake.nix`:

```nix
crap-git = pkgs.buildGoModule {
  pname = "crap-git";
  version = "0.1.0";
  src = ./go-crap;
  subPackages = [ "cmd/crap-git" ];
  vendorHash = large-colon.vendorHash;  # same module, same deps

  postInstall = ''
    mv $out/bin/crap-git $out/bin/::git
  '';
};
```

Exposed as `packages.crap-git` alongside `large-colon`.

## What this does NOT do

- No special parsing for commands other than `pull` and `push`
- No config file or pluggable parser system
- No modification of git's behavior (no extra flags injected)
- No interactive git support (e.g., `git rebase -i`)
