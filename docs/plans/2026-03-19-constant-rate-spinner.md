# Constant-Rate Spinner Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Animate the braille spinner at a constant frame rate independent of output, and remove the monkey emoji / 💤 from status lines.

**Architecture:** The Go ticker goroutine (`startStatusTicker`) gains an `UpdateInProgress()` call so the braille snake animates at 3fps regardless of output cadence. The `statusSpinner` type is stripped down to just track content for re-rendering (no frames, no sleep detection). In Rust, `Spinner` gains a `constant_rate()` constructor that disables rate-limiting so every `prefix()` call advances. Sleep detection is removed from both implementations.

**Tech Stack:** Go (go-crap), Rust (rust-crap)

**Rollback:** Revert the commits. Purely additive to the spinner behavior.

---

### Task 1: Go — Strip statusSpinner down to content tracker

**Promotion criteria:** N/A

**Files:**
- Modify: `go-crap/execparallel.go:238-296` (statusSpinner type and methods)

**Step 1: Remove frames, sleep detection, and rate-limiting from statusSpinner**

Replace the `statusSpinner` type and all its methods with a minimal struct that
just tracks whether the spinner is disabled:

```go
// statusSpinner is a minimal wrapper that controls whether status line
// content is rendered. It has no animation frames — the braille snake on
// in-progress test points handles all animation.
type statusSpinner struct {
	disabled bool
}

func newStatusSpinner() *statusSpinner {
	return &statusSpinner{}
}
```

Remove: `monkeyFrames`, `Touch()`, `prefix()`, `currentPrefix()`, and all
fields except `disabled`.

**Step 2: Update all callers that used spinner.Touch() and spinner.prefix()**

In `go-crap/execparallel.go`:

- Line 336 (`spinner.Touch()`): delete
- Line 342 (`spinner.Touch()`): delete
- Line 343 (`tw.UpdateLastLine(spinner.prefix() + line)`): change to `tw.UpdateLastLine(line)`
- Line 375 (`spinner.Touch()`): delete
- Line 377 (`tw.UpdateLastLine(spinner.prefix() + lastContent)`): change to `tw.UpdateLastLine(lastContent)`

In `go-crap/awkharness.go`:

- Line 187 (`spinner.Touch()`): delete
- Line 188 (`tw.UpdateLastLine(spinner.prefix() + comment)`): change to `tw.UpdateLastLine(comment)`

**Step 3: Update startStatusTicker to drop spinner prefix and add UpdateInProgress**

In `go-crap/execparallel.go:302-326`, the ticker body becomes:

```go
func startStatusTicker(tw *Writer, spinner *statusSpinner, mu *sync.Mutex, content *string) func() {
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		ticker := time.NewTicker(time.Second / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				tw.UpdateInProgress()
				if *content != "" {
					tw.UpdateLastLine(*content)
				}
				mu.Unlock()
			case <-done:
				return
			}
		}
	}()
	return func() {
		close(done)
		<-exited
	}
}
```

Key change: `tw.UpdateInProgress()` is called every tick, and status line
content is rendered without a spinner prefix.

**Step 4: Update runWithStatusLine signature**

`runWithStatusLine` at line 332 still takes `spinner *statusSpinner` — keep this
since `execParallelWithRunningCount` uses `spinner.disabled` to control whether
status lines render at all.

**Step 5: Run tests**

Run: `cd go-crap && go test ./...`
Expected: PASS (status line tests may need updated expectations if they checked
for monkey emoji in output)

**Step 6: Commit**

```
git add go-crap/execparallel.go go-crap/awkharness.go
git commit -m "refactor: strip statusSpinner to content tracker, move animation to ticker"
```

---

### Task 2: Go — Clean up execparallel tests

**Promotion criteria:** N/A

**Files:**
- Modify: `go-crap/execparallel_test.go`

**Step 1: Check for monkey emoji or spinner prefix in test expectations**

Search `execparallel_test.go` for `🙈`, `🙉`, `🙊`, `spinner`, or `prefix`.
Update any assertions that expect emoji prefixes in status line output to expect
plain content instead.

**Step 2: Run tests**

Run: `cd go-crap && go test ./...`
Expected: PASS

**Step 3: Commit (if changes were needed)**

```
git add go-crap/execparallel_test.go
git commit -m "test: update execparallel tests for plain status lines"
```

---

### Task 3: Rust — Update SPINNER_FRAMES to 4-dot snake

**Promotion criteria:** N/A

**Files:**
- Modify: `rust-crap/src/lib.rs:917-919` (SPINNER_FRAMES constant)
- Modify: `rust-crap/src/lib.rs` (tests at lines 2790, 2800, 3034, 3044)

**Step 1: Update SPINNER_FRAMES**

```rust
const SPINNER_FRAMES: &[&str] = &[
    "⡇⠀", "⠏⠀", "⠋⠁", "⠉⠉", "⠈⠙", "⠀⠹", "⠀⢸", "⠀⣰", "⢀⣠", "⣀⣀", "⣄⡀", "⣆⠀",
];
```

**Step 2: Update test assertions that reference old frame values**

- `start_test_point_emits_spinner` (line ~2800): change `⡀⠀` to `⡇⠀`
- `update_in_progress_advances_frame` (line ~3044): change `⠄⠀` to `⠏⠀`

**Step 3: Run tests**

Run: `cd rust-crap && cargo test`
Expected: PASS

**Step 4: Commit**

```
git add rust-crap/src/lib.rs
git commit -m "feat(rust): update SPINNER_FRAMES to 4-dot braille snake"
```

---

### Task 4: Rust — Add Spinner::constant_rate() and remove sleep detection

**Promotion criteria:** N/A

**Files:**
- Modify: `rust-crap/src/lib.rs:990-1100` (Spinner struct and impl)
- Modify: `rust-crap/src/lib.rs` (spinner tests at lines 2724-2785)

**Step 1: Write the failing test for constant_rate**

Add after the existing spinner tests:

```rust
#[test]
fn spinner_constant_rate_advances_every_call() {
    let mut s = Spinner::constant_rate();
    let f1 = s.prefix();
    let f2 = s.prefix();
    assert_ne!(f1, f2, "constant_rate spinner should advance on every prefix() call");
}
```

**Step 2: Run test to verify it fails**

Run: `cd rust-crap && cargo test spinner_constant_rate`
Expected: FAIL — `constant_rate` method does not exist

**Step 3: Add Spinner::constant_rate() constructor**

```rust
/// Create a spinner that advances on every `prefix()` call with no rate
/// limiting. Use this with a background ticker thread that calls `prefix()`
/// at the desired frame rate.
pub fn constant_rate() -> Self {
    Self {
        min_dur: Duration::ZERO,
        ..Self::new()
    }
}
```

**Step 4: Run test to verify it passes**

Run: `cd rust-crap && cargo test spinner_constant_rate`
Expected: PASS

**Step 5: Remove sleep detection**

Remove from `Spinner`:
- Field: `sleep_after`
- Field: `last_content`
- Method: `touch()`
- Method: `is_sleeping()`
- Method: `formatted_prefix()`
- Method: `formatted_current_prefix()`
- Constant: `SPINNER_SLEEP_AFTER`

Update `Spinner::new()` and `Spinner::constant_rate()` to not set the removed
fields.

**Step 6: Remove sleep-related tests**

Delete these test functions:
- `spinner_not_sleeping_initially`
- `spinner_not_sleeping_after_touch`
- `spinner_sleeping_detection`
- `spinner_formatted_prefix_includes_zzz_when_sleeping`
- `spinner_formatted_prefix_no_zzz_when_active`

**Step 7: Update Spinner doc comment**

Remove references to 💤, sleep detection, and `touch()` from the struct-level
doc comment (around line 925-938). Update the example to use `constant_rate()`
and remove `touch()` calls and 💤 references.

**Step 8: Run tests**

Run: `cd rust-crap && cargo test`
Expected: PASS

**Step 9: Commit**

```
git add rust-crap/src/lib.rs
git commit -m "feat(rust): add Spinner::constant_rate(), remove sleep detection"
```

---

### Task 5: Verify end-to-end

**Files:** None (testing only)

**Step 1: Build everything**

Run: `just build`
Expected: nix build succeeds

**Step 2: Run all tests**

Run: `just test`
Expected: all tests pass

**Step 3: Manual verification**

Run: `nix run .#crappy-git -- fetch`

Verify: the braille snake animates smoothly at constant rate, status line shows
plain text without monkey emoji.
