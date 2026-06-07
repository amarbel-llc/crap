default: build test

build: lint-vendor build-nix

build-nix:
    nix build --show-trace

test: test-go test-cargo

test-go:
    cd go-crap && nix develop ../ --command go test ./...

test-cargo:
    nix develop --command cargo test --manifest-path rust-crap/Cargo.toml

run-nix *ARGS:
    nix run . -- {{ARGS}}

codemod-fmt: codemod-fmt-go codemod-fmt-rust codemod-fmt-nix

codemod-fmt-go:
    nix develop --command gofumpt -w .

codemod-fmt-rust:
    nix develop --command cargo fmt --manifest-path rust-crap/Cargo.toml

codemod-fmt-nix:
    nix run github:amarbel-llc/purse-first?dir=devenvs/nix#fmt -- .

lint-vendor:
    #!/usr/bin/env sh
    set -e
    if [ -d go-crap/vendor ] && grep -q 'vendorHash = "sha256-' flake.nix; then
        echo "FAIL: go-crap/vendor/ exists but flake.nix has a non-null vendorHash"
        echo "Set vendorHash = null; for all Go packages"
        exit 1
    fi
    if [ ! -d go-crap/vendor ] && grep -q 'vendorHash = null;' flake.nix; then
        echo "FAIL: go-crap/vendor/ missing but flake.nix has vendorHash = null"
        echo "Run 'go mod vendor' or set vendorHash to the correct hash"
        exit 1
    fi

update: update-nix

update-nix:
    nix flake update

clean: clean-build

clean-build:
    rm -rf result build/
