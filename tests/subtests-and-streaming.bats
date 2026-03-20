#!/usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  export output

  TAP_SCRIPT="$BATS_TEST_TMPDIR/emit-tap.sh"
  cat > "$TAP_SCRIPT" <<'SCRIPT'
#!/bin/sh
cat <<'EOF'
TAP version 14
1::3
# Subtest: compilation
    1::2
    ok 1 - compile main.rs
      ---
      output: |
        compiling main.rs
        compiling lib.rs
        linking binary
      ...
    ok 2 - compile tests
      ---
      output: |
        compiling test_parse.rs
        compiling test_format.rs
      ...
ok 1 - compilation
# Subtest: test suite
    1::3
    ok 1 - test_parse
      ---
      output: |
        running test_parse
        assertion passed: expected 42 got 42
      ...
    not ok 2 - test_format
      ---
      message: "assertion failed"
      severity: fail
      output: |
        running test_format
        FAIL: expected "hello" got "world"
      ...
    ok 3 - test_lint # skip no linter configured
ok 2 - test suite
ok 3 - cleanup
1::3
EOF
SCRIPT
  chmod +x "$TAP_SCRIPT"
}

# --- :: <script>: subtests with streaming output ---

function script_emits_crap_version { # @test
  run run_crap --no-spinner "$TAP_SCRIPT"
  assert_success
  assert_line --index 0 "CRAP-2"
}

function script_preserves_subtest_comments { # @test
  run run_crap --no-spinner "$TAP_SCRIPT"
  assert_success
  assert_line --partial "# Subtest: compilation"
  assert_line --partial "# Subtest: test suite"
}

function script_preserves_nested_test_points { # @test
  run run_crap --no-spinner "$TAP_SCRIPT"
  assert_success
  assert_line --partial "ok 1 - compile main.rs"
  assert_line --partial "ok 2 - compile tests"
}

function script_preserves_not_ok_in_subtest { # @test
  run run_crap --no-spinner "$TAP_SCRIPT"
  assert_success
  assert_line --partial "not ok 2 - test_format"
}

function script_preserves_skip_directive { # @test
  run run_crap --no-spinner "$TAP_SCRIPT"
  assert_success
  assert_line --partial "test_lint"
  assert_line --partial "SKIP"
}

function script_preserves_streamed_output_content { # @test
  run run_crap --no-spinner "$TAP_SCRIPT"
  assert_success
  assert_line --partial "compiling main.rs"
  assert_line --partial "linking binary"
}

function script_preserves_correlated_parent_test_points { # @test
  run run_crap --no-spinner "$TAP_SCRIPT"
  assert_success
  assert_line --partial "ok 1 - compilation"
  assert_line --partial "ok 2 - test suite"
  assert_line --partial "ok 3 - cleanup"
}

function script_preserves_diagnostic_from_failing_subtest { # @test
  run run_crap --no-spinner "$TAP_SCRIPT"
  assert_success
  assert_line --partial "running test_format"
  assert_line --partial 'FAIL: expected "hello" got "world"'
}
