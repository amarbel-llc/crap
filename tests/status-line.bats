#!/usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  export output
}

# --- exec: basic status line output ---

function exec_emits_crap_version_and_plan { # @test
  run run_crap exec --no-spinner echo hello
  assert_success
  assert_line --index 0 "CRAP-2"
  assert_line --partial "1::1"
}

function exec_passes_on_success { # @test
  run run_crap exec --no-spinner echo hello
  assert_success
  assert_line --partial "ok 1"
}

function exec_fails_on_nonzero_exit { # @test
  run run_crap exec --no-spinner false
  assert_failure
  assert_line --partial "not ok 1"
}

# --- exec-parallel: basic functionality ---

function exec_parallel_runs_multiple_commands { # @test
  run run_crap exec-parallel --no-spinner echo ::: one two three
  assert_success
  assert_line --partial "1::3"
}

# --- validate: CRAP stream validation ---

function validate_accepts_valid_stream { # @test
  run run_crap validate --input "$(printf 'CRAP-2\n1::1\nok 1 - test\n')"
  assert_success
  assert_line --partial "valid"
}

function validate_rejects_missing_plan { # @test
  run run_crap validate --input "$(printf 'CRAP-2\nok 1 - test\n')"
  assert_success
  assert_line --partial "invalid"
}

# --- reformat: ANSI handling ---

function reformat_emits_crap_version { # @test
  local bin="${LARGE_COLON_BIN:-large-colon}"
  run sh -c "printf 'TAP version 14\n1::1\nok 1 - test\n' | \"$bin\" reformat"
  assert_success
  assert_line --index 0 "CRAP-2"
}
