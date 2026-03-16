# Migrating from tap-dancer to rust-crap

This guide covers migrating a Rust project from `tap-dancer` (TAP version 14) to
`rust-crap` (CRAP version 2). It is written for an agent or developer with no
prior context on the crap codebase.

## What changes

| tap-dancer | rust-crap | Notes |
|---|---|---|
| `TapWriterBuilder` | `CrapWriterBuilder` | Same builder pattern |
| `TapWriter` | `CrapWriter` | Same API shape |
| `TapConfig` | `CrapConfig` | Same fields |
| `tap_dancer::TestResult` | `rust_crap::TestResult` | Same struct fields |
| `write_version()` | `write_version()` | Emits `CRAP version 2` instead of `TAP version 14` |
| `write_plan()` | `write_plan()` | Same |
| `write_test_point()` | `write_test_point()` | Same |
| `.tty_build_last_line(bool)` | `.status_line(bool)` | Pragma renamed |
| `pragma +tty-build-last-line` | `pragma +status-line` | CRAP-2 name |
| N/A | `has_visible_content()` | New: public utility |
| N/A | `StatusLineProcessor` | New: PTY line splitting |
| N/A | `feed_status_bytes()` | New: convenience method |

### Version line

tap-dancer emits `TAP version 14`. rust-crap emits `CRAP version 2`. If your
tests assert the version string, update them.

### Pragma rename

tap-dancer's `tty_build_last_line` builder method maps to rust-crap's
`status_line` method. The emitted pragma changes from
`pragma +tty-build-last-line` to `pragma +status-line`.

### YAML single-line values use quoted scalars

tap-dancer used block scalar (`|`) for all output values. rust-crap uses quoted
scalars (`"value"`) for single-line values and block scalar for multi-line. This
also interacts with blank-line filtering: if a value like `"hello\n"` has its
trailing newline stripped, it becomes single-line and gets quoted. Tests
asserting exact YAML format will need updating.

```yaml
# tap-dancer (always block scalar)
  output: |
    hello

# rust-crap (single-line → quoted scalar)
  output: "hello"

# rust-crap (multi-line → block scalar, same as before)
  output: |
    line one
    line two
```

## Step-by-step migration

### 1. Update Cargo.toml

Replace the tap-dancer dependency with rust-crap:

```toml
# Before
tap-dancer = { git = "https://github.com/amarbel-llc/bob" }

# After
rust-crap = { git = "https://github.com/amarbel-llc/crap" }
```

### 2. Find and replace imports

```rust
// Before
use tap_dancer;
// or
tap_dancer::TapWriterBuilder
tap_dancer::TestResult

// After
use rust_crap;
// or
rust_crap::CrapWriterBuilder
rust_crap::TestResult
```

Search for all occurrences of `tap_dancer` in your source and replace with
`rust_crap`. The struct and function names change as follows:

- `TapWriterBuilder` → `CrapWriterBuilder`
- `TapWriter` → `CrapWriter`
- `TapConfig` → `CrapConfig`

All free functions (`write_version`, `write_plan`, `write_test_point`,
`write_bail_out`, `write_comment`, `write_skip`, `write_todo`, `write_pragma`,
`write_plan_skip`, `write_plan_locale`) keep the same names.

`TestResult` and `Spinner` keep the same names.

### 3. Rename builder method

```rust
// Before
.tty_build_last_line(enabled)

// After
.status_line(enabled)
```

### 4. Replace inline TTY helpers with rust-crap utilities

If your code has inline implementations of any of these patterns, replace them
with the rust-crap equivalents:

#### has_visible_content

```rust
// Before (inline in your code)
fn has_visible_content(s: &str) -> bool {
    // ... manual ANSI stripping logic
}

// After
use rust_crap::has_visible_content;
```

#### Single-writer lifecycle (recommended)

The intended usage pattern is **one CrapWriter for the entire output lifecycle**.
The same writer that emits the version/plan also handles status line updates and
test point emission. This enables auto-clear: the writer tracks whether a status
line is active and automatically clears it before emitting test points.

If your code creates separate writers for plan emission and test point emission,
refactor to use a single writer. Wrap it in a `Mutex` if needed for concurrent
access:

```rust
// Before (split writers — auto-clear DOES NOT work)
{
    let mut writer = CrapWriterBuilder::new(&mut stdout).build()?;
    writer.plan_ahead(count)?;
} // writer dropped

// ... recipes run, status lines written directly to stdout ...

{
    // Fresh writer has no status line state — auto-clear cannot fire
    let mut writer = CrapWriterBuilder::new(&mut stdout)
        .build_without_printing()?;
    writer.test_point(&result)?;
}

// After (single writer — auto-clear works)
let writer = Mutex::new(
    CrapWriterBuilder::new(&mut stdout)
        .color(color)
        .status_line(streaming)
        .build()?
);
writer.lock().unwrap().plan_ahead(count)?;

// In streaming callback:
writer.lock().unwrap().feed_status_bytes(chunk)?;

// After recipe completes:
writer.lock().unwrap().test_point(&result)?;
// ^ auto-clears any active status line before emitting
```

#### PTY line splitting + DECAWM + visible content filtering

With a single writer, replace inline PTY streaming with `feed_status_bytes()`:

```rust
// Before (inline PTY streaming)
let line_buf = Mutex::new(Vec::<u8>::new());
stream_command_output(cmd, &|chunk| {
    let mut buf = line_buf.lock().unwrap();
    buf.extend_from_slice(chunk);
    let mut stdout = stdout_lock.lock();
    while let Some(pos) = buf.iter().position(|&b| b == b'\n' || b == b'\r') {
        let line = String::from_utf8_lossy(&buf[..pos]);
        let line = line.trim();
        if has_visible_content(line) {
            if is_tty {
                write!(stdout, "\r\x1b[2K\x1b[?7l# {line}\x1b[?7h")?;
            } else {
                write!(stdout, "\r\x1b[2K# {line}")?;
            }
            stdout.flush()?;
        }
        buf.drain(..=pos);
    }
    Ok(())
})

// After (single writer)
stream_command_output(cmd, &|chunk| {
    writer.lock().unwrap().feed_status_bytes(chunk)
})
```

`feed_status_bytes` handles:
- Buffering partial lines
- Splitting on `\r` and `\n`
- Trimming whitespace
- Filtering ANSI-only/blank lines via `has_visible_content`
- DECAWM wrapping when color is enabled
- Writing the `# <text>` status line format
- Tracking `status_line_active` for auto-clear

#### Standalone StatusLineProcessor

If you cannot use a single writer (e.g., the writer is not available during
streaming), use `StatusLineProcessor` directly. You retain manual control over
output and must handle status line clearing yourself:

```rust
use rust_crap::StatusLineProcessor;

let mut proc = StatusLineProcessor::new();
// In your streaming callback:
for line in proc.feed(chunk) {
    // `line` is trimmed, visible-content-only
    // You must write the status line and handle clearing manually
}
```

#### Auto-clear before test points

When using a single CrapWriter for both streaming and test point emission,
auto-clear works automatically. The writer clears any active status line before
`ok`, `not ok`, `skip`, `todo`, `bail_out`, and `test_point` calls.

**Important:** Auto-clear only works when the same CrapWriter instance is used
for both `feed_status_bytes`/`update_last_line` and test point emission. If you
use `build_without_printing()` to create a fresh writer per test point, it has no
knowledge of active status lines and auto-clear will not fire. In that case,
you must clear manually:

```rust
// Manual clear (only needed with split writers)
write!(stdout, "\r\x1b[2K")?;
stdout.flush()?;
writer.test_point(&test_result)?;
```

### 5. Update test assertions

If your integration tests assert on output format, update:

```rust
// Before
assert!(output.contains("TAP version 14"));
assert!(output.contains("pragma +tty-build-last-line"));

// After
assert!(output.contains("CRAP version 2"));
assert!(output.contains("pragma +status-line"));
```

### 6. YAML blank-line filtering

rust-crap's `sanitize_yaml_value` now filters out blank/whitespace-only lines
from multiline YAML diagnostic values. If your code does its own blank-line
filtering on output before passing it to `TestResult.output`, that filtering is
now redundant (but harmless to leave).

## API reference (quick)

### Builder

```rust
// Recommended: single writer for entire lifecycle
let mut writer = CrapWriterBuilder::new(&mut stdout)
    .color(true)              // Enable ANSI colors
    .default_locale()         // Use system locale for number formatting
    .status_line(true)        // Enable status line pragma
    .build()?;                // Emits version + pragmas

// build_without_printing: skips version/pragma emission.
// Use only when you need a writer mid-stream (e.g., subtests).
// NOTE: a writer created this way has no status line state —
// auto-clear before test points will not fire.
let mut writer = CrapWriterBuilder::new(&mut stdout)
    .color(true)
    .build_without_printing()?;
```

### Writer methods

```rust
writer.plan_ahead(count)?;           // 1..N
writer.ok("description")?;           // ok N - description
writer.not_ok("desc")?;              // not ok N - description
writer.not_ok_diag("desc", diag)?;   // not ok N + YAML block
writer.skip("desc", "reason")?;      // ok N - desc # SKIP reason
writer.todo("desc", "reason")?;      // not ok N - desc # TODO reason
writer.bail_out("reason")?;          // Bail out! reason
writer.comment("text")?;             // # text
writer.test_point(&result)?;         // Emit from TestResult struct
writer.update_last_line("text")?;    // Status line (transient)
writer.finish_last_line()?;          // Clear status line
writer.feed_status_bytes(chunk)?;    // PTY streaming convenience
writer.plan()?;                      // Trailing plan
```

### Free functions

```rust
rust_crap::write_version(&mut w)?;
rust_crap::write_plan(&mut w, count)?;
rust_crap::write_test_point(&mut w, &result)?;
rust_crap::write_bail_out(&mut w, "reason")?;
rust_crap::write_comment(&mut w, "text")?;
rust_crap::write_pragma(&mut w, "key", true)?;
rust_crap::has_visible_content("text");  // -> bool
```

### TestResult struct

```rust
rust_crap::TestResult {
    number: usize,
    name: String,
    ok: bool,
    directive: Option<String>,
    error_message: Option<String>,
    exit_code: Option<i32>,
    output: Option<String>,
    suppress_yaml: bool,
}
```

### StatusLineProcessor

```rust
let mut proc = rust_crap::StatusLineProcessor::new();
for line in proc.feed(chunk) {
    // line: String — trimmed, visible content only
}
```
