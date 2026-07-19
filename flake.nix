{
  description = "CRAP: Command Result Accessibility Protocol";

  inputs = {
    # Fork of upstream nixpkgs. overlays.default exposes buildGoApplication,
    # gomod2nix, mkGoEnv, and other amarbel-llc additions.
    igloo.url = "https://code.linenisgreat.com/igloo/archive/master.tar.gz";
    nixpkgs-master.url = "github:NixOS/nixpkgs/567a49d1913ce81ac6e9582e3553dd90a955875f";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";
    bats = {
      url = "https://code.linenisgreat.com/bats/archive/master.tar.gz";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    # conformist: the linter + formatter multiplexer (treefmt successor),
    # adopted via its Nix module (conformist.lib.evalModule) rather than a
    # hand-written conformist.toml. presets.eng + presets.eng-go supply the
    # eng-convention linters and the canonical goimports → gofumpt chain;
    # ./conformist.nix layers nixfmt/rustfmt/shfmt/shellcheck +
    # repo-specific excludes. Exposed as the flake `formatter` and gated by
    # both `just lint-fmt` (the sandboxed checks.formatting derivation) and
    # `nix flake check`.
    conformist = {
      url = "https://code.linenisgreat.com/conformist/archive/master.tar.gz";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
    utils.inputs.systems.follows = "igloo/systems";
    igloo.inputs.nixpkgs-master.follows = "nixpkgs-master";
    bats.inputs.conformist.follows = "conformist";
  };

  outputs =
    {
      self,
      igloo,
      nixpkgs-master,
      utils,
      bats,
      conformist,
    }:
    let
      # version.env at repo root is the single source of truth for the
      # release version (eng-versioning(7)). Burnt into the Go binaries via
      # the fork's auto-injected -ldflags. `just bump-version` sed-rewrites
      # version.env. The match captures everything after `CRAP_VERSION=` up
      # to the line break.
      crapVersion = builtins.head (
        builtins.match ".*CRAP_VERSION=([^\n]+).*" (builtins.readFile ./version.env)
      );
      # shortRev for clean builds, dirtyShortRev for dirty working trees (so
      # devshell builds show `dirty-abcdef` rather than masquerading as a
      # clean release), "unknown" as a last-resort fallback.
      crapCommit = self.shortRev or self.dirtyShortRev or "unknown";
    in
    utils.lib.eachDefaultSystem (
      system:
      let
        # The fork's default.nix shim auto-applies overlays.default, so an
        # explicit `overlays = [ igloo.overlays.default ]` would just
        # compose the overlay twice.
        pkgs = import igloo { inherit system; };
        pkgs-master = import nixpkgs-master { inherit system; };

        conformistPkg = conformist.packages.${system}.default;

        # Pure lane: the eng presets (+ the canonical goimports -> gofumpt
        # chain) and this repo's overlay (./conformist.nix). Drives `nix fmt`
        # and the sandboxed `checks.formatting`.
        conformistEval = conformist.lib.evalModule pkgs {
          imports = [
            conformist.lib.presets.eng
            conformist.lib.presets.eng-go
            ./conformist.nix
          ];
          package = conformistPkg;
        };

        # Impure lane: the git-state checks (git-remotes, sweatfile,
        # agents-md, gomod2nix) run against the working tree via
        # `just lint-worktree`. Also carries clippy (conformist#69, opt-in —
        # not in the eng-impure roster): impure because it compiles the
        # crate, so it can only run here, never in the sandboxed
        # checks.formatting. rust-crap has a real [package] (not a virtual
        # workspace), but `--workspace` is harmless on a single crate and
        # matches the module default; manifest-path points at the
        # rust-crap/ subtree since Cargo.toml isn't at the repo root.
        conformistImpureEval = conformist.lib.evalModule pkgs {
          imports = [
            conformist.lib.presets.eng-impure
            {
              linters.clippy.enable = true;
              linters.clippy.manifest-path = "rust-crap/Cargo.toml";
            }
          ];
          package = conformistPkg;
          projectRootFile = "flake.nix";
        };

        # Producer side of the flake-input-go_mod protocol (RFC 0001):
        # exposes the go-crap module's source tree as `go-pkgs` so sibling
        # flakes (e.g. cutting-garden) can bridge it via goFlakeInputs
        # instead of fetching a published version. crap is polyglot, so the
        # module manifests are anchored under go-crap/.
        goPkgs = pkgs.mkGoPkgs {
          src = self;
          extras = [
            "^go-crap/go\\.mod$"
            "^go-crap/go\\.sum$"
          ];
        };

        large-colon = pkgs.buildGoApplication {
          pname = "large-colon";
          version = crapVersion;
          commit = crapCommit;
          src = ./go-crap;
          pwd = ./go-crap;
          modules = ./go-crap/gomod2nix.toml;
          subPackages = [ "cmd/large-colon" ];
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";

          nativeCheckInputs = [ pkgs-master.git ];

          postInstall = ''
            ln -s $out/bin/large-colon "$out/bin/::"
          '';

          meta = {
            description = "CRAP-2 validator and writer toolkit";
            homepage = "https://github.com/amarbel-llc/crap";
            license = pkgs.lib.licenses.mit;
          };
        };

        crap-present = pkgs.buildGoApplication {
          pname = "crap-present";
          version = crapVersion;
          commit = crapCommit;
          src = ./go-crap;
          pwd = ./go-crap;
          modules = ./go-crap/gomod2nix.toml;
          subPackages = [ "cmd/crap-present" ];
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";

          meta = {
            description = "ndjson-crap viewport presenter (standalone)";
            homepage = "https://github.com/amarbel-llc/crap";
            license = pkgs.lib.licenses.mit;
          };
        };

        rust-crap = pkgs-master.rustPlatform.buildRustPackage {
          pname = "rust-crap";
          version = crapVersion;
          src = ./rust-crap;

          # build.rs guards Cargo.toml's version against version.env; the
          # nix src is the rust-crap/ subtree (no ../version.env), so the
          # authoritative version arrives via this env var instead.
          CRAP_VERSION = crapVersion;

          cargoLock.lockFile = ./rust-crap/Cargo.lock;

          meta = {
            description = "CRAP-2 writer library";
            homepage = "https://github.com/amarbel-llc/crap";
            license = pkgs.lib.licenses.mit;
          };
        };
      in
      {
        packages = {
          default = pkgs.symlinkJoin {
            name = "crap";
            paths = [
              large-colon
              crap-present
            ];
          };
          inherit
            large-colon
            crap-present
            rust-crap
            ;
          # go-crap module source for sibling-flake consumers (RFC 0001).
          inherit (goPkgs) go-pkgs;
          # The generated config for the impure lane's `just lint-worktree`.
          conformist-impure-config = conformistImpureEval.config.build.configFile;
          # The store-pinned, toolchain-hermetic hook pair (conformist#47/#51/#54):
          # pre-commit runs `conformist --staged --exit-zero-on-fix`; repair runs
          # `conformist --commit --amend --exit-zero-on-fix`. Also on the devShell
          # PATH below, under these same names, for a future sweatfile to wire.
          conformist-pre-commit = conformistEval.config.build.preCommit;
          conformist-repair = conformistEval.config.build.repair;
        };

        # `nix fmt` runs conformist wrapped with the generated config
        # (conformistEval above).
        formatter = conformistEval.config.build.wrapper;

        # Sandboxed read-only gate: `conformist check` against a /nix/store
        # snapshot of the tracked tree, no writes. Gated by `just lint-fmt`
        # and `nix flake check`.
        checks.formatting = conformistEval.config.build.check self;

        devShells.default = pkgs.mkShell {
          packages = [
            # Go: gomod2nix-aware env; reads go-crap/gomod2nix.toml for
            # module resolution (drop-in for a bare go toolchain).
            (pkgs.mkGoEnv { pwd = ./go-crap; })
            # gomod2nix CLI lives in the fork's overlay alongside
            # buildGoApplication / mkGoEnv — not in upstream nixpkgs.
            pkgs.gomod2nix
            pkgs-master.gopls
            pkgs-master.gotools
            pkgs-master.gofumpt
            pkgs-master.goawk
            pkgs-master.delve
            pkgs-master.golangci-lint
            pkgs-master.golines
            pkgs-master.govulncheck
            pkgs-master.parallel

            # Rust
            pkgs-master.rustc
            pkgs-master.cargo
            pkgs-master.rustfmt
            pkgs-master.rust-analyzer
            pkgs-master.cargo-deny
            pkgs-master.cargo-edit
            pkgs-master.cargo-watch
            pkgs.openssl
            pkgs.pkg-config

            # Shell
            pkgs-master.bash-language-server
            pkgs-master.shellcheck
            pkgs-master.shfmt

            # conformist (treefmt successor) + the store-pinned hook pair
            # from this repo's own config (conformistEval above) — `nix fmt`
            # / `just lint-fmt` / `just codemod-fmt` resolve their formatter
            # binaries from the generated config, not from PATH, so no
            # per-formatter package needs to be listed here for those to
            # work; gofumpt/rustfmt/shfmt/shellcheck above remain for
            # interactive/LSP use (gopls, rust-analyzer,
            # bash-language-server).
            conformistPkg
            conformistEval.config.build.preCommit
            conformistEval.config.build.repair
            pkgs.nixfmt

            # Tools
            pkgs.just
            # gum: terminal UI logging for the maintenance recipes
            # (bump-version / tag / release).
            pkgs-master.gum
            bats.packages.${system}.bats
            bats.packages.${system}.batman
          ];
        };
      }
    );
}
