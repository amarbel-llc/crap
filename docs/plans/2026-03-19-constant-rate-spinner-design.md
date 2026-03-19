# Constant-Rate Spinner Animation

**Date:** 2026-03-19
**Issue:** #2

## Problem

The in-progress test point spinner advances one frame per output event, so
animation speed is tied to output rate. Bursty output makes the spinner stall
or jump.

## Design

### Go

Extend `startStatusTicker` to call `tw.UpdateInProgress()` on each tick (3fps),
giving the braille snake constant animation independent of output.

Remove the monkey emoji spinner from status lines. Status lines become plain
content text. Remove 💤 sleep detection entirely.

Changes:
- `startStatusTicker`: add `tw.UpdateInProgress()` call in ticker loop
- `statusSpinner`: remove frame cycling, `prefix()`, `currentPrefix()`,
  sleep detection, monkey frames
- Simplify to just tracking whether content exists for the ticker to re-render
- Status line rendering: plain text, no prefix

### Rust

Keep `Spinner` as a pure state machine (no threads). Add
`Spinner::constant_rate()` constructor that sets `min_dur` to `Duration::ZERO`,
so every `prefix()` call unconditionally advances the frame. Callers control
cadence via their own ticker thread.

Update `SPINNER_FRAMES` to the 4-dot braille snake.

Remove sleep detection: `is_sleeping`, `sleep_after`, `formatted_prefix`,
`formatted_current_prefix`.

### Testing

- Go: update hardcoded frame values in existing tests
- Rust: add test for `constant_rate()` verifying every `prefix()` advances;
  update `SPINNER_FRAMES` and existing test frame values
