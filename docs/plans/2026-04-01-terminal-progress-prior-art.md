# Prior Art: Terminal Progress Rendering in Package Managers

**Date:** 2026-04-01
**Related:** TTY Rendering Helpers, Constant-Rate Spinner, PTY-Wrapping Reformatter

## Purpose

Empirical analysis of how npm, yarn classic, and pnpm render progress to the
terminal. Captured via `script -c` with a 40x120 PTY. Informs the rendering
strategy for `::` (large-colon).

## npm v11.9.0 (node 24.14.0)

**Strategy:** Single-line spinner, no progress bar, no multi-line.

The entire rendering model is three operations:

1. Write one Braille spinner character (from `⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏`)
2. `ESC[1G` — CHA (cursor to column 1)
3. `ESC[0K` — EL (erase to end of line)

When a log line needs to emit, the spinner is cleared first, the line is written
(scrolling the terminal), then the spinner re-renders on the new current line.

**Source:** `npm/lib/utils/display.js`, `Progress` class (lines 441–551). Uses
`stream.cursorTo(0)` and `stream.clearLine(1)` from Node's readline. No
cursor-up, no cursor-hide, no save/restore, no alternate screen.

**Notable decisions:**
- Uses `ESC[1G` (CHA) instead of `\r` (CR) for portability
- 200ms delay before first spinner render (avoids flicker on fast operations)
- Spinner interval is unref'd (`setInterval().unref()`) so it doesn't keep the
  process alive
- Log lines are interleaved: spinner clears → log writes → spinner re-renders
- Final summary output still has spinner clear/render interleaved (no clean
  shutdown of spinner before final output)

## yarn classic v1.22.22

**Strategy:** Single-line progress bar with phase transitions.

Rendering primitives:
- `ESC[2K` — EL2 (erase entire line) for phase transitions
- `ESC[1G` — CHA (cursor to column 1) for bar redraws
- `ESC[0K` — EL (erase to end of line) for partial updates
- `ESC[1B` — cursor down 1 (used once, for "Building fresh packages" phase)

Four visible phases, each with different rendering:
1. **Resolving** — single-line spinner showing current package name
2. **Fetching** — `[####---...---]  N/total` progress bar, redrawn in place
3. **Linking** — same progress bar style, two sub-phases (coarse then fine)
4. **Building** — brief phase, uses cursor-down once

**Notable:** The progress bar redraws the entire `[###---]` line on each update
using CHA + EL. No cursor-up — each phase starts on a new line.

## pnpm v10.33.0

**Strategy:** Single-line in-place counter using cursor-up.

Rendering primitives:
- `ESC[1A` — CUU (cursor up 1 line) — **the key differentiator**
- Standard ANSI colors (`ESC[96m` cyan, `ESC[32m` green, `ESC[90m` dim)
- `ESC[0K` — EL (erase to end of line) for final package count line

Progress line format:
```
Progress: resolved <cyan>N</cyan>, reused <cyan>N</cyan>, downloaded <cyan>N</cyan>, added <cyan>N</cyan>
```

Each update writes `ESC[1A` then overwrites the previous progress line. The
counter values increase monotonically. When complete, appends `, done` and stops
rewriting.

After the progress line settles, a summary block scrolls normally below:
```
Packages: +231
+++++++++++++++++...
dependencies:
+ express 4.22.1
...
Done in 5.2s
```

**Notable:** pnpm uses cursor-up (`[1A`) rather than CR or CHA. This means it
prints a full line with newline, then moves back up to overwrite. This is the
approach that produces the "multi-line progress" appearance on slow connections
where multiple status lines might be visible simultaneously.

## Comparison Matrix

| Feature                  | npm v11     | yarn classic | pnpm v10    |
|--------------------------|-------------|--------------|-------------|
| Spinner                  | Braille dot | Braille dot  | None        |
| Progress bar             | No          | `[####---]`  | Counters    |
| Cursor movement          | CHA only    | CHA + down   | CUU (up)    |
| Multi-line rewrite       | No          | No           | Yes (`[1A`) |
| Cursor hide/show         | No          | No           | No          |
| Alternate screen         | No          | No           | No          |
| Scroll regions (DECSTBM) | No          | No           | No          |
| Synchronized output      | No          | No           | No          |

## Implications for `::`

### What all three agree on

- **No alternate screen buffer.** All three render inline in the main scrollback.
- **No cursor hide/show.** None use `ESC[?25l`/`ESC[?25h`.
- **No scroll regions (DECSTBM).** None use `ESC[T;Br` to reserve a status area.
- **No synchronized output.** None use DEC mode 2026.
- **Single-line is enough.** Even npm, with hundreds of concurrent fetches, uses
  a single spinner character. Users don't need to see N parallel operations.

### What `::` should adopt

1. **CHA (`ESC[1G`) + EL (`ESC[0K`) for spinner/status line** — matches npm's
   approach, which is the simplest and most portable. This is already what
   `UpdateLastLine` does in go-crap.

2. **Delay before first render** — npm waits 200ms. Prevents spinner flicker on
   sub-second operations. The constant-rate spinner plan should adopt this.

3. **No DECSTBM scroll regions** — the `clear-cherry` branch explored this, but
   none of the major package managers use it. CHA+EL is sufficient and doesn't
   risk terminal state corruption.

### What `::` should NOT adopt

1. **CUU (`ESC[1A`) for multi-line rewrite** — only pnpm does this, and `::` is
   wrapping arbitrary commands, not a known counter format. CUU requires knowing
   exactly how many lines to go back, which is fragile with wrapped lines.

2. **Full-width progress bars** — yarn classic's `[####---]` is specific to
   "fetching N of M packages." `::` doesn't have that kind of bounded progress
   information from arbitrary TAP streams.

### Open question

Should `::` use DECAWM (disable autowrap) for status lines? The TTY rendering
helpers plan already includes this. None of the three package managers do it, but
they also don't have the long-line wrapping problem because their status content
is always short. `::` wraps arbitrary command output which can be arbitrarily
wide, so DECAWM remains justified.
