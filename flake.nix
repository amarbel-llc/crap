{
  description = "CRAP: Command Result Accessibility Protocol";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/3e20095fe3c6cbb1ddcef89b26969a69a1570776";
    nixpkgs-master.url = "github:NixOS/nixpkgs/e2dde111aea2c0699531dc616112a96cd55ab8b5";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";
    rust = {
      url = "github:amarbel-llc/purse-first?dir=devenvs/rust";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
    shell = {
      url = "github:amarbel-llc/purse-first?dir=devenvs/shell";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
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
      rust,
      shell,
      bob,
    }:
    utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        pkgs-master = import nixpkgs-master { inherit system; };

        large-colon = pkgs-master.buildGoModule {
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

        crappy-git = pkgs-master.buildGoModule {
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

        crappy-brew = pkgs-master.buildGoModule {
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

        crappy-direnv = pkgs-master.buildGoModule {
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

        rust-crap = pkgs.rustPlatform.buildRustPackage {
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
          inputsFrom = [
            rust.devShells.${system}.default
            shell.devShells.${system}.default
          ];

          packages = [
            pkgs-master.go
            pkgs-master.gopls
            pkgs-master.gotools
            pkgs-master.gofumpt
            pkgs-master.goawk
            pkgs-master.delve
            pkgs.just
            bob.packages.${system}.batman
          ];
        };
      }
    );
}
