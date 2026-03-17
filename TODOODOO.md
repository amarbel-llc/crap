- [ ] Use vhs to demo the :: tool
- [ ] Update CRAP nomenclature: test points remain but use acronym TP more often throughout spec
- [ ] Flatten all amendments into the CRAP-2 specification (no separate amendment docs)
- [ ] brainstorm replacements for "test plan", "subtest", "test point", "bail
  out"
- [ ] Research local nix build verification that bypasses store cache. Problem:
  `nix build` skips rebuilding when a derivation's store path already exists, so
  stale `vendorHash` or other flake.nix misconfigurations pass locally but fail
  on clean machines. `--rebuild` doesn't help — it only re-runs the top-level
  derivation, not intermediate FODs (fixed-output derivations like go-modules).
  Need a `just test-nix` recipe that forces full re-evaluation without requiring
  CI. Possible angles: `nix-store --delete` of specific paths before build,
  `nix eval` to extract and validate derivation attributes, or `nix flake check`
  with custom checks.
