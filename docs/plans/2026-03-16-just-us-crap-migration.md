# just-us: Migrate from tap-dancer to rust-crap

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Replace tap-dancer dependency with rust-crap in just-us, removing inline TTY helpers and adopting a single-writer lifecycle where one CrapWriter handles version/plan, status line streaming, and test point emission.

**Architecture:** Single `Mutex<CrapWriter>` created in `run_tap()`, threaded through recipe execution. Replaces the current split-writer pattern (one writer for plan, fresh writer per test point) and ~40 lines of inline TTY helpers in `recipe.rs`. Also replaces `TapTally` — `CrapWriter` already tracks counter and failure state.

**Tech Stack:** Rust, rust-crap (git dep from github.com/amarbel-llc/crap)

**Rollback:** Revert the Cargo.toml change + `git checkout src/justfile.rs src/recipe.rs`

**Reference:** See `docs/migration-from-tap-dancer.md` for the full API mapping, YAML format differences, and single-writer pattern.

---

### Task 1: Update Cargo.toml dependency

**Files:**
- Modify: `Cargo.toml`

**Step 1: Replace tap-dancer with rust-crap**

Find the line:
```toml
tap-dancer = { git = "https://github.com/amarbel-llc/bob" }
```

Replace with:
```toml
rust-crap = { git = "https://github.com/amarbel-llc/crap" }
```

**Step 2: Run cargo check to verify the dependency resolves**

Run: `cargo check 2>&1 | head -20`
Expected: Compilation errors about `tap_dancer` not found (expected — we haven't updated imports yet). The dependency itself should resolve.

**Step 3: Commit**

```
feat: replace tap-dancer dependency with rust-crap
```

---

### Task 2: Refactor justfile.rs to single-writer lifecycle

**Files:**
- Modify: `src/justfile.rs`

This is the key architectural change. Currently `run_tap()` creates a writer at the top for version/plan, drops it, then creates a fresh `build_without_printing()` writer per test point. We refactor to one `Mutex<CrapWriter>` for the entire lifecycle.

**Step 1: Understand the current flow**

Read `src/justfile.rs` and find `run_tap()`. The current pattern:

1. Lines ~293-304: Create writer #1, emit version+plan, drop it
2. Lines ~306-322: Run recipes via `run_recipe()`, passing `tap_tally: Mutex<TapTally>`
3. Lines ~510-522: Per recipe, create writer #2 via `build_without_printing()`, emit test point

`TapTally` (search for its definition) tracks `counter: usize`, `failures: usize`, and `color: bool`. `CrapWriter` already has `has_failures()` and an internal counter, so `TapTally` can be eliminated.

**Step 2: Replace the plan-emission block**

Replace the scoped block at lines ~293-304:

```rust
// Before
{
    let mut stdout = io::stdout().lock();
    let mut writer = tap_dancer::TapWriterBuilder::new(&mut stdout)
        .color(color)
        .default_locale()
        .tty_build_last_line(output_format == OutputFormat::TapStreamedOutput)
        .build()
        .map_err(|io_error| Error::StdoutIo { io_error })?;
    writer
        .plan_ahead(plan_count)
        .map_err(|io_error| Error::StdoutIo { io_error })?;
}

// After
let stdout = io::stdout();
let mut stdout_lock = stdout.lock();
let writer = rust_crap::CrapWriterBuilder::new(&mut stdout_lock)
    .color(color)
    .default_locale()
    .status_line(output_format == OutputFormat::TapStreamedOutput)
    .build()
    .map_err(|io_error| Error::StdoutIo { io_error })?;
writer
    .plan_ahead(plan_count)
    .map_err(|io_error| Error::StdoutIo { io_error })?;
let writer = Mutex::new(writer);
```

**Important lifetime note:** The `CrapWriter` borrows `stdout_lock`, so `stdout` and `stdout_lock` must live as long as the writer. Declare them before the writer and don't drop them. If lifetime issues arise, consider using `Box<dyn Write>` or collecting to a `Vec<u8>` buffer. Another option: since `CrapWriter` takes `&mut impl Write`, you may need to restructure so the lock is held for the duration of `run_tap()`. If that causes contention with recipe execution (which also writes to stdout for streamed output), you may need to use a buffer that flushes to stdout, or pass the writer's Mutex through to recipes.

**Step 3: Replace `TapTally` with the shared writer**

Replace `let tap_tally = Mutex::new(TapTally::new(color));` with passing the writer Mutex to `run_recipe`. The writer tracks failures via `has_failures()` and the counter internally.

Find where `tap_tally` is passed through `run_recipe` and trace its usage:
- In `run_recipe`, it's passed as `tap_tally: Option<&Mutex<TapTally>>`
- Recipes lock it to increment the counter and record failures
- After all recipes, `tap_tally.into_inner()` checks for failures

Change the parameter from `Option<&Mutex<TapTally>>` to `Option<&Mutex<rust_crap::CrapWriter>>` (or a type alias). Update all call sites.

**Step 4: Replace per-test-point writer creation**

Find the block around lines ~510-522 where `build_without_printing()` creates a fresh writer per test point. Replace with locking the shared writer:

```rust
// Before
let mut stdout = io::stdout().lock();
if output_format == OutputFormat::TapStreamedOutput {
    write!(stdout, "\r\x1b[2K").map_err(|io_error| Error::StdoutIo { io_error })?;
    stdout.flush().map_err(|io_error| Error::StdoutIo { io_error })?;
}
let mut writer = tap_dancer::TapWriterBuilder::new(&mut stdout)
    .color(tap.color)
    .default_locale()
    .build_without_printing()
    .map_err(|io_error| Error::StdoutIo { io_error })?;
writer
    .test_point(&test_result)
    .map_err(|io_error| Error::StdoutIo { io_error })?;

// After
let mut writer = writer_mutex.lock().unwrap();
writer
    .test_point(&test_result)
    .map_err(|io_error| Error::StdoutIo { io_error })?;
```

Note: The manual `\r\x1b[2K` clear is gone — `test_point` auto-clears because the same writer tracked the status line state from `feed_status_bytes`.

**Step 5: Replace the failure check**

```rust
// Before
let tap = tap_tally.into_inner().unwrap();
if tap.failures > 0 {
    Err(Error::TapFailure { count: tap.counter, failures: tap.failures })
}

// After
let writer = writer.into_inner().unwrap();
if writer.has_failures() {
    // You'll need to track the failure count separately, or add a method.
    // For now, if CrapWriter doesn't expose failure count, keep a simple
    // AtomicUsize counter alongside the writer mutex.
}
```

Check whether `CrapWriter` exposes enough state to replace `TapTally` entirely. If it doesn't expose a failure count (only `has_failures() -> bool`), either:
- Keep a separate `AtomicUsize` for the failure count
- Add a `failure_count()` method to CrapWriter (preferred — note this as upstream feedback)

**Step 6: Rename TestResult references**

Apply the mechanical renames:

| Before | After |
|---|---|
| `tap_dancer::TestResult` | `rust_crap::TestResult` |

`TestResult` field names are identical between tap-dancer and rust-crap.

**Step 7: Verify it compiles**

Run: `cargo check`
Expected: Errors in `recipe.rs` (Task 3) but `justfile.rs` should be clean.

**Step 8: Commit**

```
refactor: single CrapWriter lifecycle in run_tap, remove TapTally
```

---

### Task 3: Replace inline TTY helpers in recipe.rs with feed_status_bytes

**Files:**
- Modify: `src/recipe.rs`

**Step 1: Delete `has_visible_content` function**

Remove the `has_visible_content` function at the top of `recipe.rs` (approximately lines 3-23). This is now provided by `rust_crap::has_visible_content`, though we won't need to import it — `feed_status_bytes` uses it internally.

**Step 2: Thread the writer Mutex into recipe execution**

Find where `run_linewise` and `run_script` are called. They need access to the shared `Mutex<CrapWriter>`. Add a parameter — trace the call chain from `run_recipe` to find the right signature.

The writer Mutex should be passed as an `Option` (since non-TAP output formats don't use it), matching the existing `tap_output: Option<&Mutex<Vec<u8>>>` pattern.

**Step 3: Replace the streaming callback in run_linewise**

Find the `OutputFormat::TapStreamedOutput` match arm in `run_linewise` (around line 522). Replace the entire inline PTY streaming block:

```rust
// Before
OutputFormat::TapStreamedOutput => {
    use std::io::IsTerminal;
    let stdout_lock = io::stdout();
    let is_tty = stdout_lock.is_terminal();
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
}

// After
OutputFormat::TapStreamedOutput => {
    stream_command_output(cmd, &|chunk| {
        let mut writer = writer_mutex.lock().unwrap();
        writer.feed_status_bytes(chunk)
            .map_err(|e| io::Error::new(io::ErrorKind::Other, e))
    })
}
```

Note: Check the error type — `feed_status_bytes` returns `io::Result<()>` which should be compatible with `stream_command_output`'s callback signature. If not, map the error.

**Step 4: Apply the same replacement in run_script**

Find the second identical streaming block in `run_script` (around line 763-785) and apply the same replacement.

**Step 5: Clean up unused imports**

After removing `has_visible_content`, check if any imports are now unused.

**Step 6: Verify it compiles**

Run: `cargo check`
Expected: Clean compilation.

**Step 7: Commit**

```
refactor: replace inline TTY helpers with CrapWriter.feed_status_bytes
```

---

### Task 4: Update integration tests

**Files:**
- Modify: `tests/tap.rs`

**Step 1: Find all version and pragma assertions**

Search `tests/tap.rs` for:
- `"TAP version 14"` — replace with `"CRAP version 2"`
- `"tty-build-last-line"` — replace with `"status-line"`

**Step 2: Find YAML format assertions**

Search for `output: |` in test assertions. rust-crap uses quoted scalars for
single-line values:

```rust
// Before (tap-dancer: always block scalar)
assert!(out.contains("  output: |\n    hello\n"));

// After (rust-crap: single-line → quoted scalar)
assert!(out.contains("  output: \"hello\"\n"));
```

Multi-line values still use block scalar (`|`). Only single-line values change.

**Step 3: Apply all replacements**

This is partly mechanical (version string, pragma name) and partly requires
judgment (YAML format). Run the tests after each batch of changes to find
remaining failures.

**Step 4: Run the tests**

Run: `cargo test`
Expected: All tests pass. Iterate on any remaining failures.

**Step 5: Commit**

```
test: update assertions for CRAP version 2 output and YAML format
```

---

### Task 5: Verify and smoke test

**Step 1: Run the full test suite**

Run: `cargo test`
Expected: All tests pass.

**Step 2: Run clippy**

Run: `cargo clippy`
Expected: No new warnings.

**Step 3: Run fmt**

Run: `cargo fmt`

**Step 4: Commit if needed**

```
chore: clean up after tap-dancer to rust-crap migration
```

---

### Task 6: Record deviations

**Files:**
- Create: `docs/plans/2026-03-16-just-us-crap-migration-deviations.md`

Record any deviations from this plan: unexpected compilation errors, API
mismatches, test failures that required non-obvious fixes, lifetime issues with
the shared writer, etc. This feedback improves the crap migration guide and API.

Format each deviation as:

```markdown
### N. Short title

**Plan (Task N Step N):** What the plan said to do.

**Actual:** What actually happened.

**Upstream action:** What should change in rust-crap or the migration guide.
```
