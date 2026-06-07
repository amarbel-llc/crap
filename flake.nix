{
  description = "CRAP: Command Result Accessibility Protocol";

  inputs = {
    # Fork of upstream nixpkgs. overlays.default exposes buildGoApplication,
    # gomod2nix, and other amarbel-llc additions.
    igloo.url = "github:amarbel-llc/igloo";
    nixpkgs-master.url = "github:NixOS/nixpkgs/d233902339c02a9c334e7e593de68855ad26c4cb";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";
    bats = {
      url = "github:amarbel-llc/bats";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
  };

  outputs =
    {
      self,
      igloo,
      nixpkgs-master,
      utils,
      bats,
    }:
    utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import igloo {
          inherit system;
          overlays = [ igloo.overlays.default ];
        };
        pkgs-master = import nixpkgs-master { inherit system; };

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

        large-colon = pkgs-master.buildGoModule.override { go = pkgs-master.go_1_26; } {
          pname = "large-colon";
          version = "0.1.0";
          src = ./go-crap;
          subPackages = [ "cmd/large-colon" ];
          vendorHash = null;

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

        crap-present = pkgs-master.buildGoModule.override { go = pkgs-master.go_1_26; } {
          pname = "crap-present";
          version = "0.1.0";
          src = ./go-crap;
          subPackages = [ "cmd/crap-present" ];
          vendorHash = null;

          meta = {
            description = "ndjson-crap viewport presenter (standalone)";
            homepage = "https://github.com/amarbel-llc/crap";
            license = pkgs.lib.licenses.mit;
          };
        };

        rust-crap = pkgs-master.rustPlatform.buildRustPackage {
          pname = "rust-crap";
          version = "0.1.0";
          src = ./rust-crap;

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

        devShells.default = pkgs.mkShell {
          packages = [
            # Go
            pkgs-master.go_1_26
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

            # Tools
            pkgs.just
            bats.packages.${system}.bats
            bats.packages.${system}.batman
          ];
        };
      }
    );
}
