// Guard Cargo.toml's package.version against drifting from version.env,
// the repo's single version source of truth (eng-versioning(7); see
// amarbel-llc/eng#162). `just bump-version` rewrites both files; this
// build script fails the build if they ever disagree.
//
// Resolution order for the authoritative version:
//   1. $CRAP_VERSION — set by the nix derivation, whose `src` is the
//      rust-crap/ subtree and so has no ../version.env to read.
//   2. ../version.env — dev builds from the workspace checkout.
// If neither exists (e.g. a published crate tarball), the guard is a
// no-op and CARGO_PKG_VERSION stands on its own.

use std::env;
use std::fs;

fn version_from_env_file(contents: &str) -> Option<String> {
    contents.lines().find_map(|line| {
        line.trim()
            .strip_prefix("export CRAP_VERSION=")
            .or_else(|| line.trim().strip_prefix("CRAP_VERSION="))
            .map(str::to_owned)
    })
}

fn main() {
    println!("cargo:rerun-if-changed=../version.env");
    println!("cargo:rerun-if-env-changed=CRAP_VERSION");

    let authoritative = env::var("CRAP_VERSION").ok().or_else(|| {
        fs::read_to_string("../version.env")
            .ok()
            .as_deref()
            .and_then(version_from_env_file)
    });

    let Some(want) = authoritative else {
        return;
    };
    let have = env::var("CARGO_PKG_VERSION").expect("cargo always sets CARGO_PKG_VERSION");
    if want != have {
        panic!(
            "rust-crap/Cargo.toml version ({have}) disagrees with version.env CRAP_VERSION ({want}); \
             run `just bump-version {want}` to resync"
        );
    }
}
