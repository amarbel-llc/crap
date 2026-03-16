# Design: TTY Rendering Helpers for rust-crap (and go-crap)

## Problem

Consumers of rust-crap that stream child process output as CRAP-2 status lines
must independently solve the same set of TTY rendering problems that just-us
already solved:

- Long lines wrap and leave ghost artifacts (DECAWM)
- ANSI-only lines produce blank `# ` status updates
- PTY output uses `\r` for in-place progress, requiring `\r`/`\n` splitting
- Status lines persist alongside permanent test points without manual clearing
- YAML output blocks contain PTY artifacts (`\r\n`, blank lines)

These are protocol-level concerns, not application-specific logic.

## Changes (rust-crap)

### 1. DECAWM wrapping in `update_last_line()`

Bracket status line content with `\x1b[?7l` (disable autowrap) and `\x1b[?7h`
(re-enable) when `config.color` is true. Track `status_line_active: bool` on
`CrapWriter` for use by auto-clear (#4).

### 2. `has_visible_content()` public utility

Port from just-us `recipe.rs`. Walks chars, skips CSI sequences and
whitespace/control, returns true on first visible character. Exposed as
`pub fn has_visible_content(s: &str) -> bool`.

### 3a. `StatusLineProcessor` (standalone)

Stateful struct holding an internal byte buffer. `feed(&mut self, chunk: &[u8])`
appends to buffer, splits on `\r` and `\n`, trims, filters via
`has_visible_content`, drains consumed bytes, yields clean `String`s. Consumer
passes each to `update_last_line()`.

### 3b. `CrapWriter::feed_status_bytes()` (convenience)

`CrapWriter` gains an `Option<StatusLineProcessor>` field, lazily initialized.
`feed_status_bytes(&mut self, chunk: &[u8])` feeds the chunk through the
processor and calls `update_last_line()` for each yielded line. One call does
everything.

### 4. Auto-clear before test points

In `test_point()`, if `status_line_active` is true, call `finish_last_line()`
before emitting the test point. `update_last_line()` sets the flag;
`finish_last_line()` clears it.

### 5. Blank-line filtering in `sanitize_yaml_value()`

After `normalize_line_endings()`, filter out blank/whitespace-only lines from
multiline values. Handles PTY `\r\n` translation artifacts.

## Changes (go-crap) — follow-up

Port the same five changes to `go-crap/crap.go`:

1. DECAWM wrapping in `UpdateLastLine()`
2. `HasVisibleContent()` public function
3. `StatusLineProcessor` struct with `Feed()` method
4. Convenience method on `Writer` (equivalent to `feed_status_bytes`)
5. Auto-clear in test point emission
6. Blank-line filtering in `sanitizeYAMLValue()`

## No rollback concern

All changes are additive API additions or bug fixes to existing methods.
Existing callers are unaffected except for DECAWM (#1) and auto-clear (#4),
which are pure improvements — no consumer relies on the current broken behavior.
