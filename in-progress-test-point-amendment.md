---
layout: default
title: "CRAP-2 Amendment: In-Progress Test Points"
---

<!-- SPDX-License-Identifier: Artistic-2.0 -->

# CRAP-2 Amendment: In-Progress Test Points

## Problem

CRAP-2 test points are emitted only after a test completes — the
`ok` or `not ok` status token appears once the result is known. For
long-running tests, this means the terminal shows no indication that
a test has started until it finishes. Users watching a build or test
run have no way to tell which test is currently executing, whether it
is making progress, or whether the process has stalled.

Build tools commonly solve this with a status line (see the Status
Line amendment), but the status line is a single unstructured comment
at the bottom of the output. It cannot convey which *test point* is
running, and it disappears when the test completes.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119.

## Specification

### Default State

The in-progress test point feature is enabled by default in CRAP-2.
Producers do not need to emit `pragma +in-progress` to use it.
Producers that do not want this feature MAY disable it with:

```crap-2
pragma -in-progress
```

### Behavior

When `in-progress` is active (the default) and output is a TTY, the
producer MAY emit a test point line with a progress spinner in place
of the `ok` or `not ok` status token. This line indicates that the
test has started but its result is not yet known.

An in-progress test point line:

1. MUST use a spinning/animating character sequence in place of the
   `ok` or `not ok` token. The spinner character set is
   implementation-defined (e.g., braille dots `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`, or
   similar). The spinner MUST be rendered in ANSI SGR yellow
   (SGR 33) when color is enabled, making it visually distinct from
   both green `ok` and red `not ok`. The spinner SHOULD be visually
   distinct from both `ok` and `not ok` even without color (by
   virtue of using a non-alphanumeric character).
2. MUST include the test point number and description, formatted
   identically to a completed test point (e.g., `⠋ 1 - test name`).
3. MUST be followed by a newline, placing the cursor on the
   following line (consistent with the Status Line amendment).
4. MAY be updated in place by moving the cursor up one line
   (`ESC [A`), clearing the line (`ESC [2K`), and rewriting with the
   next spinner frame.
5. MUST be rewritten with the final `ok` or `not ok` status when
   the test completes, using the same cursor-up-and-rewrite
   mechanism.

Only one test point MAY be in-progress at any given time. Producers
MUST NOT emit a new in-progress test point while another is still
in-progress — the prior test point MUST be resolved (rewritten with
its final status) first.

### Producer Requirements

Producers MUST only use in-progress test points when standard output
is a terminal (TTY). When output is redirected to a file or pipe,
producers MUST NOT emit in-progress lines — they MUST wait until the
test result is known and emit the completed test point directly.

Producers SHOULD emit `pragma -in-progress` when output is not a TTY,
unless the user has explicitly requested TTY behavior (e.g., via a
`--color=always` flag).

When the in-progress test point is the last line of output, the
producer updates it using the same cursor-up mechanism defined by the
Status Line amendment:

1. Move cursor up one line (`ESC [A`).
2. Return to column 1 and clear the line (`\r`, `ESC [2K`).
3. Write the new content (next spinner frame, or final status).
4. Emit a trailing newline.

### Synchronized Output

To prevent visible cursor flicker during in-progress updates,
producers SHOULD wrap cursor movement sequences in DEC private mode
2026 (Synchronized Output):

1. Emit `ESC [?2026h` to begin a synchronized update.
2. Perform all cursor movement, line clearing, and rewriting.
3. Emit `ESC [?2026l` to end the synchronized update.

The terminal buffers all drawing operations between these markers and
renders them as a single atomic visual update, eliminating the
momentary blank line that would otherwise appear between clearing and
rewriting.

Terminals that do not support synchronized output (DEC private mode
2026) silently ignore the escape sequences, so producers MAY emit
them unconditionally when color/TTY output is active.

This guidance applies equally to spinner frame updates, in-progress
completion rewrites, and any combined updates involving both the
in-progress line and the status line.

### Completing an In-Progress Test Point

When the test result is known, the producer MUST rewrite the
in-progress line with the final test point:

1. Move cursor up to the in-progress line (`ESC [A`).
2. Clear the line (`\r`, `ESC [2K`).
3. Emit the completed test point (e.g., `ok 1 - test name\n`).

After completion, the producer MAY immediately emit a new in-progress
line for the next test, YAML diagnostics for the completed test, or
any other valid CRAP-2 content.

### Interaction with Status Line

When both `in-progress` and `status-line` are active, two lines may
be under cursor control: the in-progress test point line and the
status line below it. The in-progress line is always above the status
line.

When the producer needs to update the in-progress line while a status
line is also active, it MUST account for the status line occupying the
line between the in-progress content and the cursor:

1. Move cursor up two lines (`ESC [A` twice) — one past the status
   line, one to the in-progress line.
2. Clear and rewrite the in-progress line.
3. Move cursor back down past the status line.

When completing an in-progress test point while a status line is
active, the producer MUST similarly move past the status line to
reach the in-progress line.

Producers MAY simplify this by temporarily clearing the status line
before updating the in-progress line, then redrawing the status line
afterward.

### Interaction with YAML Diagnostics

YAML diagnostic blocks are emitted after a test point completes. An
in-progress test point MUST NOT have a YAML diagnostic block — the
block is emitted only after the in-progress line is rewritten with
its final status.

### Interaction with Streamed Output

When both `in-progress` and `streamed-output` are active, the
producer MAY begin emitting YAML `output` field lines while the test
is in-progress. In this case, the YAML block opener (`---`) and
output lines appear below the in-progress line. The in-progress line
itself is still rewritten with the final status when the test
completes.

### Interaction with Subtests

In a subtest, the in-progress feature applies only to that subtest's
document, consistent with CRAP-2's rule that subtest pragmas do not
affect parent document parsing. A subtest may disable the feature
with `pragma -in-progress`.

### Reader Behavior

Readers MUST treat in-progress lines as non-CRAP output (since they
contain a spinner character rather than `ok` or `not ok`). By the
time a reader processes a completed CRAP stream (from a file or
pipe), all in-progress lines will have been rewritten with their final
status, so no special handling is required.

Readers that consume CRAP directly from a TTY in real-time MAY
recognize spinner-prefixed lines as in-progress indicators, but MUST
NOT assign pass/fail semantics to them.

### Example

A test run with three tests. ANSI sequences shown in escape notation.
The in-progress line is rewritten as each test completes.

At time T1 (first test running, yellow spinner shown in SGR notation):

```
CRAP-2
1::3
\033[33m⠋\033[0m 1 - compile
```

At time T2 (first test done, second running):

```
CRAP-2
1::3
ok 1 - compile
\033[33m⠙\033[0m 2 - unit tests
```

At time T3 (second test done with diagnostics, third running):

```
CRAP-2
1::3
ok 1 - compile
not ok 2 - unit tests
  ---
  message: "assertion failed"
  severity: fail
  ...
\033[33m⠹\033[0m 3 - integration tests
```

At time T4 (all done):

```
CRAP-2
1::3
ok 1 - compile
not ok 2 - unit tests
  ---
  message: "assertion failed"
  severity: fail
  ...
ok 3 - integration tests
```

### Non-TTY Behavior

When output is not a TTY, in-progress lines MUST NOT be emitted.
The producer MUST wait until each test completes and emit the final
test point directly. The resulting output is identical to CRAP-2
without this amendment.

### Backwards Compatibility

In non-TTY contexts, this amendment has no effect on the output
format — the stream is standard CRAP-2.

In TTY contexts, in-progress lines are transient: they are always
rewritten with a valid test point before the stream completes. Any
reader processing the final output (captured from a terminal session
or a script file) will see only completed test points.

Readers that encounter an unrewritten spinner-prefixed line (e.g., if
the producer crashed mid-test) SHOULD treat it as non-CRAP output,
consistent with CRAP-2's handling of unrecognized lines.

## Security Considerations

In-progress lines use the same ANSI cursor control sequences as the
Status Line amendment (`ESC [A`, `ESC [2K`, `\r`). The security
guidance from that amendment applies here as well: producers MUST NOT
use sequences that alter terminal state beyond the current and
adjacent lines.

## Authors

This amendment is authored by Sasha F as an extension to the CRAP-2
specification by Isaac Z. Schlueter.
