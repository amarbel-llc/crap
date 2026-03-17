{
  description = "CRAP: Command Result Accessibility Protocol";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/3e20095fe3c6cbb1ddcef89b26969a69a1570776";
    nixpkgs-master.url = "github:NixOS/nixpkgs/ca82feec736331f4c438121a994344e08ed547f5";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";
    go = {
      url = "github:amarbel-llc/purse-first?dir=devenvs/go";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
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
      inputs.nixpkgs.follows = "nixpkgs";
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
      go,
      rust,
      shell,
      bob,
    }:
    utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            go.overlays.default
          ];
        };

        large-colon = pkgs.buildGoModule {
          pname = "large-colon";
          version = "0.1.0";
          src = ./go-crap;
          subPackages = [ "cmd/large-colon" ];
          vendorHash = null;

          nativeCheckInputs = [ pkgs.git ];

          postInstall = ''
            ln -s $out/bin/large-colon "$out/bin/::"
          '';

          meta = {
            description = "CRAP-2 validator and writer toolkit";
            homepage = "https://github.com/amarbel-llc/crap";
            license = pkgs.lib.licenses.mit;
          };
        };

        crappy-git = pkgs.buildGoModule {
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

        crappy-brew = pkgs.buildGoModule {
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
            ];
          };
          inherit
            large-colon
            crappy-git
            crappy-brew
            rust-crap
            ;
        };

        devShells.default = pkgs.mkShell {
          inputsFrom = [
            go.devShells.${system}.default
            rust.devShells.${system}.default
            shell.devShells.${system}.default
          ];

          packages = [
            pkgs.just
            bob.packages.${system}.batman
          ];
        };
      }
    );
}
