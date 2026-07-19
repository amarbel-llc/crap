default: validate lint build test

validate: validate-devshell

# Verify the devShell evaluates and builds without errors. Catches
# mkGoEnv / gomod2nix.toml breakage that the prod-binary build can mask.
# Uses builtins.currentSystem (not a hardcoded system) because CI also
# runs aarch64-darwin. No store-output usage --- just a build-check.
validate-devshell:
    #!/usr/bin/env bash
    set -euo pipefail
    system=$(nix eval --raw --impure --expr 'builtins.currentSystem')
    nix build --no-link ".#devShells.${system}.default"

lint: lint-fmt

# Read-only format + lint gate via conformist (the treefmt successor),
# through the sandboxed checks.formatting derivation (conformist.nix +
# presets.eng/eng-go). Fails on formatter drift (Go/Nix/Rust/shell) plus
# shellcheck and the eng-convention linters. `just codemod-fmt` is the
# write mode. Folded into `just lint` → `just default`, so CI and the
# pre-merge hook enforce fmt-cleanliness on every merge.
lint-fmt:
    #!/usr/bin/env bash
    set -euo pipefail
    system=$(nix eval --raw --impure --expr 'builtins.currentSystem')
    nix build ".#checks.${system}.formatting" --no-link --print-build-logs

lint-impure: lint-worktree

# The impure eng checks (git remotes, sweatfile, agents-md, gomod2nix)
# against the working tree, where .git is available. Needs a sweatfile;
# add lint-impure to the `lint` aggregate above once crap has one. Runs
# conformist from the devShell (direnv `use flake`).
lint-worktree:
    #!/usr/bin/env bash
    set -euo pipefail
    cfg=$(nix build --no-link --print-out-paths '.#conformist-impure-config')
    conformist check --config-file "$cfg" --tree-root .

build: build-gomod2nix build-nix

# Regenerate go-crap/gomod2nix.toml from go.mod/go.sum so the nix build
# resolves the same module set the worktree sees.
build-gomod2nix:
    nix develop --command gomod2nix --dir go-crap

# Build the default nix package (large-colon + crap-present, symlink-joined).
# The fork's buildGoApplication burns CRAP_VERSION + the flake rev into the
# Go binaries via -ldflags, which a raw `go build` would not.
build-nix:
    nix build --show-trace

test: test-go test-cargo

# Go test suite (go-crap/...), via the root devShell's gomod2nix-aware go.
test-go:
    cd go-crap && nix develop ../ --command go test ./...

# Go tests under the race detector: slower than `test-go`, so this is a
# separate opt-in lane rather than part of the default `test` aggregate.
# Catches concurrent-writer bugs like #23.
[group("debug")]
debug-go-test-race:
    cd go-crap && nix develop ../ --command go test -race ./...

# Rust test suite (rust-crap's cargo test), via the devShell's pinned
# rustc/cargo.
test-cargo:
    nix develop --command cargo test --manifest-path rust-crap/Cargo.toml

# Run large-colon (::) via `nix run`, forwarding ARGS to the CLI.
run-nix *ARGS:
    nix run . -- {{ARGS}}

codemod-fmt: codemod-fmt-conformist

# Format all source files via conformist (the treefmt successor): Go
# (goimports → gofumpt), Nix (nixfmt), Rust (rustfmt), shell (shfmt).
# Config lives in conformist.nix (conformist.lib.evalModule, flake.nix).
# The read-only counterpart is `lint-fmt`.
codemod-fmt-conformist:
    nix fmt

update: update-nix

# Refresh all flake inputs to their latest revisions.
update-nix:
    nix flake update

# Tidy the Go module, then regenerate gomod2nix.toml to match.
update-go: && build-gomod2nix
    cd go-crap && nix develop ../ --command go mod tidy

clean: clean-build

# Remove the nix build result symlink and the build/ output directory.
clean-build:
    rm -rf result build/

# Rewrite the CRAP_VERSION line in version.env (the single version
# source of truth) and resync rust-crap/Cargo.toml's package.version,
# which must mirror it (rust-crap/build.rs fails the build on drift —
# Cargo.toml's version field is mandatory and can't read version.env
# itself; see amarbel-llc/eng#162). Pure mutation: staging and
# committing is `release`'s responsibility. Usage: just bump-version 0.1.1
[group("maintenance")]
bump-version new_version:
    sed -E -i "s/^(export CRAP_VERSION)=.*/\\1={{new_version}}/" version.env
    sed -E -i "s/^version = \".*\"/version = \"{{new_version}}\"/" rust-crap/Cargo.toml
    sed -E -i '/^name = "rust-crap"$/,/^version = /s/^version = ".*"/version = "{{new_version}}"/' rust-crap/Cargo.lock

# Sign and push a go-crap/v<version> tag for the version currently in
# version.env. The go-crap/ tag prefix is required by the Go module
# proxy for sub-directory modules. The $message env-param form avoids
# {{ }} splicing a changelog with backticks into the script.
[group("maintenance")]
tag $message:
    #!/usr/bin/env bash
    set -euo pipefail
    . ./version.env
    tag="go-crap/v${CRAP_VERSION:?missing CRAP_VERSION in version.env}"
    git tag -s -m "$message" "$tag"
    gum log --level info "Created tag: $tag"
    git push origin "$tag"
    gum log --level info "Pushed $tag"
    git tag -v "$tag"

# Cut a release from master: changelog from commits since the last
# go-crap/v* tag (unscoped --- crap is polyglot with one repo-wide
# version), bump version.env, commit, tag, and create the GitHub
# release. Usage: just release 0.1.1
[group("maintenance")]
release new_version:
    #!/usr/bin/env bash
    set -euo pipefail

    branch=$(git rev-parse --abbrev-ref HEAD)
    if [[ "$branch" != "master" ]]; then
        gum log --level error "release only allowed from master (on '$branch')"
        exit 1
    fi

    prev=$(git tag --sort=-v:refname -l "go-crap/v*" | head -1)
    header="release go-crap/v{{new_version}}"
    if [[ -n "$prev" ]]; then
        summary=$(git log --format='- %s' "$prev"..HEAD)
        if [[ -n "$summary" ]]; then
            msg="$header"$'\n\n'"$summary"
        else
            msg="$header"
        fi
    else
        msg="$header"
    fi

    # Bump + commit only when version.env isn't already at the target.
    # Sourcing version.env (the single source of truth, as the `tag` recipe
    # does) makes release idempotent: if the version was already bumped and
    # committed in an earlier commit (e.g. to satisfy rust-crap/build.rs's
    # Cargo.toml == version.env check), re-running release skips the empty
    # commit instead of aborting on `git commit`.
    . ./version.env
    if [[ "${CRAP_VERSION:-}" != "{{new_version}}" ]]; then
        just bump-version "{{new_version}}"
        git add version.env rust-crap/Cargo.toml rust-crap/Cargo.lock
        git commit -m "$header"
    else
        gum log --level info "version.env already at {{new_version}} --- skipping bump/commit"
    fi
    git push origin master

    just tag "$msg"

    fj release create "$header" --tag "go-crap/v{{new_version}}" --body "$msg"
