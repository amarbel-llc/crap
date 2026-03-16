---
layout: default
title: "CRAP-2 Amendment: Status Line"
---

<!-- SPDX-License-Identifier: Artistic-2.0 -->

# CRAP-2 Amendment: Status Line

## Problem

Build systems and test runners often display a continuously updated
status line at the bottom of the terminal — a progress bar, a spinner,
or a summary of passed/failed counts. Tools like `npm` and `nix build`
use this pattern extensively: a single trailing line that shows build
progress, rewritten in place using ANSI cursor control sequences and
color output, while completed output scrolls above it.

CRAP-2 has no standard way to represent this pattern. Producers that
want a live-updating status line must either:

1. Emit status updates to stderr, losing association with the CRAP
   stream.
2. Emit multiple comment lines that scroll the terminal, cluttering
   the output with stale status.
3. Use ad-hoc ANSI sequences inline, confusing readers that do not
   expect cursor movement in CRAP output.

This amendment provides a standard mechanism for a single trailing
status line that producers update in place.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119.

## Specification

### Default State

The status line feature is enabled by default in CRAP-2. Producers
do not need to emit `pragma +status-line` to use it. Producers that
do not want this feature MAY disable it with:

```crap-2
pragma -status-line
```

### Behavior

When `status-line` is active (the default), the producer MAY maintain a
single trailing line at the end of the CRAP output that is continuously
updated using ANSI escape sequences (cursor movement, line clearing,
SGR color codes). This line:

1. MUST always be the last line of the output at any point in time.
2. MUST be prefixed with `# ` (hash, space), making it a valid CRAP
   comment.
3. MAY contain ANSI SGR sequences for colored output.
4. MAY be rewritten in place using ANSI cursor control sequences
   (e.g., carriage return `\r`, `ESC [2K` to clear the line).
5. MUST NOT span more than one line.

The content after the `# ` prefix is display-only and MUST be ignored
by readers, consistent with CRAP-2's treatment of comment lines.

### Producer Requirements

Producers SHOULD only use the status line feature when standard
output is a terminal (TTY). Producers SHOULD emit `pragma -status-line`
when output is redirected to a file or pipe, unless the user has
explicitly requested it (e.g., via a `--color=always` flag).

Producers MAY respect the `NO_COLOR` environment variable. When
`NO_COLOR` is set to a non-empty value, producers SHOULD suppress ANSI
sequences in the trailing line but MAY still update it in place.

When the CRAP stream is complete, the producer SHOULD emit a final
newline after the last update to the trailing line, ensuring the
terminal prompt appears on a clean line.

### Reader Behavior

Readers MUST treat the trailing line as an ordinary comment — its
content after `# ` is ignored like any other comment line.

Readers that consume CRAP from a pipe or file will never see ANSI
cursor control sequences (since producers MUST NOT emit the pragma in
non-TTY contexts), so no special handling is required.

Readers that consume CRAP directly from a TTY MAY strip ANSI
sequences from the trailing line before processing. Since the line is
a comment, this is purely cosmetic.

### Example

A CRAP stream with a live-updating build status line, shown at two
points in time. ANSI sequences are shown in `\033[...m` and `\r`
notation:

At time T1 (first test passed):

```crap-2
CRAP-2
1::3
ok 1 - compile
# \033[36m⠋ running tests... 1/3 passed\033[0m
```

At time T2 (all tests complete, trailing line rewritten):

```crap-2
CRAP-2
1::3
ok 1 - compile
ok 2 - unit tests
not ok 3 - integration tests
  ---
  message: "connection refused"
  severity: fail
  ...
# \033[32m✓ 2 passed\033[0m, \033[31m✗ 1 failed\033[0m
```

The trailing `# ...` line is updated in place on the terminal. A
reader sees it as a comment and ignores its content.

### Interaction with Streamed Output

When both `streamed-output` and `status-line` are active, the
trailing line is distinct from YAML diagnostic block content. YAML
`output` field lines are delivered incrementally within their block and
are associated with a specific test point; the trailing line is a
single comment line that is rewritten in place and has no association
with any test point.

Producers SHOULD ensure the trailing line remains below all YAML
diagnostic block content.

### Interaction with Subtests

In a subtest, the status line feature applies only to that subtest's
document, consistent with CRAP-2's rule that subtest pragmas do not
affect parent document parsing. A subtest may disable the feature with
`pragma -status-line`.

The parent document's status line state does not automatically apply to
child subtests. In practice, only the outermost document is likely to
use this feature, since only one trailing line can occupy the terminal's
last row.

### Backwards Compatibility

The trailing line is a valid CRAP comment and will be treated as such
by any CRAP-2 reader. In non-TTY contexts, producers should disable
the feature with `pragma -status-line`, so piped or file-based consumers
are unaffected.

## Security Considerations

The trailing line MAY contain ANSI cursor control sequences beyond SGR
(e.g., `\r`, `ESC [2K`). Readers that process TTY output directly
SHOULD sanitize or strip CSI sequences to prevent terminal injection
attacks, consistent with the guidance in the ANSI Display Hints
amendment.

Producers MUST NOT use the trailing line to emit escape sequences that
alter terminal state beyond the current line (e.g., scrolling regions,
alternate screen buffers, window title changes).

## Authors

This amendment is authored by Sasha F as an extension to the CRAP-2
specification by Isaac Z. Schlueter.
