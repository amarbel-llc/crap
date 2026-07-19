# crap's conformist overlay, merged with conformist.lib.presets.{eng,eng-go}
# in flake.nix (conformist.lib.evalModule). presets.eng enables the
# eng-convention linters (eng-versioning, flake-outputs/lock, the justfile-*
# roster); presets.eng-go carries the canonical goimports -> gofumpt chain.
# Here live nixfmt, rustfmt, shfmt, shellcheck (crap's pre-conformist-module
# toml roster: goimports/gofumpt/nixfmt/rustfmt/shfmt formatters +
# shellcheck linter — see conformist.toml's former content), the
# eng-versioning key, and repo-specific excludes.
{ ... }:
{
  # go.mod lives at go-crap/, not the tree root (rust-crap/ is a sibling).
  # Without this, presets.eng-go's goimports/gofumpt chain runs with cwd at
  # the tree root, where Go tooling can't resolve the module — confirmed in
  # langlang (see langlang/conformist.nix) to SILENTLY DELETE correctly-used
  # imports as apparently-unused when the imported package's declared name
  # differs from its path's last segment, because the resolver can't
  # discover which identifier the import provides. That's a silent build
  # break, not a style nit. workingDir (conformist#38) scopes the
  # formatter's cwd to go-crap/, matching crap's single Go module.
  programs.goimports.workingDir = "go-crap";
  programs.gofumpt.workingDir = "go-crap";

  # Nix: format the flake + this file.
  programs.nixfmt.enable = true;

  # Rust: rustfmt directly on files (module default already matches the old
  # hand-rolled config: `--config skip_children=true --edition 2024`).
  programs.rustfmt.enable = true;

  # Shell: module default already matches the old hand-rolled config
  # (`-w -i 2 -s -ci`). No shell files exist today; *.bats is added below to
  # keep covering the planned ndjson-crap bats suite reintroduction (the same
  # future-proofing the old conformist.toml called out).
  programs.shfmt.enable = true;
  programs.shfmt.includes = [
    "*.sh"
    "*.bash"
    "*.envrc"
    "*.envrc.*"
    "*.bats"
  ];

  # Linter: shellcheck runs read-only in `conformist check` (no autofix),
  # same *.bats future-proofing as shfmt above.
  linters.shellcheck.enable = true;
  linters.shellcheck.includes = [
    "*.sh"
    "*.bash"
    "*.envrc"
    "*.envrc.*"
    "*.bats"
  ];

  # eng-versioning(7) would otherwise derive the key from a root-level go.mod
  # / Cargo.toml, but crap is polyglot with go.mod under go-crap/ and
  # Cargo.toml under rust-crap/ (neither at the tree root) — so the
  # derivation would fail. version.env declares CRAP_VERSION; pin it
  # explicitly.
  linters.eng-versioning.key = "CRAP_VERSION";

  # Generated / locked / prose / hand-formatted — not formatted. Mirrors the
  # old conformist.toml's excludes minus "conformist.toml" itself (retired by
  # this migration).
  settings.excludes = [
    "flake.lock"
    "go.sum"
    "Cargo.lock"
    "gomod2nix.toml"
    "version.env"
    "LICENSE"
    "*.md"
    "result"
    "result-*"
    ".tmp/**"
  ];
}
