{
  description = "CRAP: Command Result Accessibility Protocol";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/4590696c8693fea477850fe379a01544293ca4e2";
    nixpkgs-master.url = "github:NixOS/nixpkgs/e2dde111aea2c0699531dc616112a96cd55ab8b5";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";
    bob = {
      url = "github:amarbel-llc/bob";
      inputs.nixpkgs.follows = "nixpkgs-master";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-master,
      utils,
      bob,
    }:
    utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        pkgs-master = import nixpkgs-master { inherit system; };

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

        crappy-git = pkgs-master.buildGoModule.override { go = pkgs-master.go_1_26; } {
          pname = "crappy-git";
          version = "0.1.0";
          src = ./go-crap;
          subPackages = [ "cmd/crappy-git" ];
          vendorHash = null;

          postInstall = ''
            mv $out/bin/crappy-git "$out/bin/::git"
          '';

          meta = {
            description = "Git wrapper that emits CRAP-2 output";
            homepage = "https://github.com/amarbel-llc/crap";
            license = pkgs.lib.licenses.mit;
          };
        };

        crappy-brew = pkgs-master.buildGoModule.override { go = pkgs-master.go_1_26; } {
          pname = "crappy-brew";
          version = "0.1.0";
          src = ./go-crap;
          subPackages = [ "cmd/crappy-brew" ];
          vendorHash = null;

          postInstall = ''
            mv $out/bin/crappy-brew "$out/bin/::brew"
          '';

          meta = {
            description = "Brew wrapper that emits CRAP-2 output";
            homepage = "https://github.com/amarbel-llc/crap";
            license = pkgs.lib.licenses.mit;
          };
        };

        crappy-direnv = pkgs-master.buildGoModule.override { go = pkgs-master.go_1_26; } {
          pname = "crappy-direnv";
          version = "0.1.0";
          src = ./go-crap;
          subPackages = [ "cmd/crappy-direnv" ];
          vendorHash = null;

          postInstall = ''
            mv $out/bin/crappy-direnv "$out/bin/::direnv"
          '';

          meta = {
            description = "Direnv wrapper that emits CRAP-2 output";
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
              crappy-git
              crappy-brew
              crappy-direnv
            ];
          };
          inherit
            large-colon
            crappy-git
            crappy-brew
            crappy-direnv
            rust-crap
            ;
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
            bob.packages.${system}.batman
          ];
        };
      }
    );
}
