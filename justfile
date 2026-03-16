default: build test

build: build-nix

build-nix:
    nix build --show-trace

test: test-go test-cargo test-bats

test-go:
    cd go-crap && nix develop ../ --command go test ./...

test-cargo:
    nix develop --command cargo test --manifest-path rust-crap/Cargo.toml

test-bats:
    nix build --show-trace
    LARGE_COLON_BIN=result/bin/large-colon bats --no-sandbox --tap tests/

run-nix *ARGS:
    nix run . -- {{ARGS}}

codemod-fmt: codemod-fmt-go codemod-fmt-rust codemod-fmt-nix

codemod-fmt-go:
    nix develop --command gofumpt -w .

codemod-fmt-rust:
    nix develop --command cargo fmt --manifest-path rust-crap/Cargo.toml

codemod-fmt-nix:
    nix run github:amarbel-llc/purse-first?dir=devenvs/nix#fmt -- .

update: update-nix

update-nix:
    nix flake update

test-awk:
    #!/usr/bin/env sh
    set -e
    fail=0
    for expected in go-crap/awk/*/testdata/*.expected; do
        input="${expected%.expected}.input"
        dir="$(dirname "$(dirname "$expected")")"
        name="$(basename "${expected%.expected}")"
        script="$dir/$name.awk"
        if [ ! -f "$script" ] || [ ! -f "$input" ]; then
            continue
        fi
        if ! awk -f "$script" < "$input" | diff -u - "$expected"; then
            echo "FAIL: $script"
            fail=1
        else
            echo "ok: $script"
        fi
    done
    exit $fail

capture *ARGS:
    mkdir -p captures
    script -q captures/stdout.txt sh -c '{{ARGS}}' 2>captures/stderr.txt

clean: clean-build

clean-build:
    rm -rf result build/
