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

    # conformist: the linter + formatter multiplexer (treefmt successor).
    # Drives the goimports → gofumpt → nixfmt → rustfmt → shfmt chain plus
    # shellcheck; config lives in ./conformist.toml. Exposed as the flake
    # `formatter` and gated by `just lint-fmt` (conformist check).
    conformist = {
      url = "https://code.linenisgreat.com/conformist/archive/master.tar.gz";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
    utils.inputs.systems.follows = "igloo/systems";
    igloo.inputs.nixpkgs-master.follows = "nixpkgs-master";
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

        # `nix fmt` entry point: conformist (the treefmt successor) wrapped
        # with the formatter binaries its ./conformist.toml drives on PATH.
        # Formatting drift is gated by `just lint-fmt` (conformist check).
        conformistFmt = pkgs.writeShellApplication {
          name = "conformist-fmt";
          runtimeInputs = [
            conformist.packages.${system}.default
            pkgs-master.gofumpt
            pkgs-master.gotools
            pkgs.nixfmt
            pkgs-master.rustfmt
            pkgs.shfmt
            pkgs.shellcheck
          ];
          text = ''exec conformist "$@"'';
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
        };

        # `nix fmt` runs conformist (see conformistFmt above).
        formatter = conformistFmt;

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

            # conformist (treefmt successor) + nixfmt, the one formatter
            # its ./conformist.toml drives that isn't already above, so
            # `just codemod-fmt` / `just lint-fmt` work in the devshell.
            conformist.packages.${system}.default
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
