bats_load_library bats-support
bats_load_library bats-assert
bats_load_library bats-emo

require_bin LARGE_COLON_BIN large-colon

run_crap() {
  local bin="${LARGE_COLON_BIN:-large-colon}"
  "$bin" "$@"
}
