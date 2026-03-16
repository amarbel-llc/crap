---
layout: default
title: "CRAP-2 Amendment: ANSI in YAML Output Blocks"
---

<!-- SPDX-License-Identifier: Artistic-2.0 -->

# CRAP-2 Amendment: ANSI in YAML Output Blocks

## Abstract

This amendment defines how CRAP-2 producers MAY embed ANSI escape
sequences in YAML diagnostic block values when standard output is a
terminal (TTY). It specifies which YAML fields are permitted to contain
ANSI sequences, how harnesses SHOULD handle them, and how the CRAP
stream remains valid and parseable regardless of whether a consumer
supports color.

## Problem

Test output frequently captures the stdout or stderr of child
processes — compiler output, linter results, diff output, test runner
summaries — that uses ANSI color codes to improve readability. When
this output is included in a YAML diagnostic block (e.g., in an
`output` or `message` field), producers today must choose between:

1. Stripping all ANSI sequences from captured output, losing the
   color information that makes failures easier to diagnose at a
   glance.
2. Including ANSI sequences in YAML values unconditionally, which
   pollutes log files and confuses YAML parsers that do not expect
   non-printable characters.
3. Omitting captured output from YAML blocks entirely and sending it
   to stderr, losing association with the test point.

The ANSI Display Hints amendment addresses color in test point status
keywords and directives, but explicitly excludes YAML diagnostic
blocks. This amendment fills that gap.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119.

## Specification

### When to Emit Color in YAML Blocks

Producers SHOULD embed ANSI sequences in YAML diagnostic values only
when standard output is a terminal (TTY). Producers MUST NOT embed
ANSI sequences in YAML diagnostic values when output is redirected to
a file or pipe, unless the user has explicitly requested color (e.g.,
via a `--color=always` flag).

Producers MAY respect the `NO_COLOR` environment variable
([no-color.org](https://no-color.org)). When `NO_COLOR` is set to a
non-empty value, producers SHOULD suppress ANSI sequences in YAML
diagnostic values regardless of TTY detection.

### Permitted Sequences

Producers MUST limit ANSI sequences in YAML values to SGR (Select
Graphic Rendition) sequences only — the `ESC [ <params> m` form.
Producers MUST NOT emit non-SGR escape sequences (cursor movement,
screen clearing, terminal title changes, etc.) in YAML diagnostic
values.

### Permitted Fields

Producers MAY include ANSI SGR sequences in YAML diagnostic values
that represent captured process output or human-readable messages.
Common fields include:

| Field      | Content                                     |
|------------|---------------------------------------------|
| `output`   | Captured stdout/stderr of a child process   |
| `message`  | Human-readable description of the result    |
| `stderr`   | Captured stderr specifically                |
| `diff`     | Diff output comparing expected and actual   |

Producers MUST NOT include ANSI sequences in YAML field names (keys).

Producers SHOULD NOT include ANSI sequences in YAML values that
represent structured data intended for programmatic consumption, such
as:

- Numeric values (`exitcode`, `line`, `column`)
- File paths (`file`)
- Identifiers (`severity`, `type`)
- Data comparison fields (`found`, `wanted`, `expected`, `actual`)

### YAML Validity

ANSI escape characters (`0x1B`) are valid content within YAML 1.2
quoted strings and block scalars. Producers that include ANSI
sequences in YAML values MUST ensure the resulting YAML block remains
valid YAML 1.2. In practice, this means:

- Block scalar values (literal `|` or folded `>`) naturally
  accommodate ANSI sequences with no additional escaping.
- Double-quoted strings MAY contain ANSI sequences directly, as
  `0x1B` is permitted in YAML double-quoted scalars.
- Single-quoted strings MUST NOT contain ANSI sequences, as YAML
  single-quoted scalars do not support escape sequences.

### Harness Behavior

Harnesses MUST be able to parse YAML diagnostic blocks that contain
ANSI SGR sequences. Since ANSI escape characters are valid YAML
content, compliant YAML 1.2 parsers will handle them without
modification.

Harnesses that compare YAML diagnostic values programmatically (e.g.,
comparing `found` against `wanted`) SHOULD strip ANSI SGR sequences
before comparison.

Harnesses that display YAML diagnostic content to a terminal MAY pass
through ANSI sequences in their output. Harnesses that display to a
non-terminal SHOULD strip ANSI sequences before output.

Harnesses that strip ANSI sequences SHOULD strip all `ESC [` CSI
sequences, not just SGR, to prevent injection of terminal control
codes.

### Interaction with ANSI Display Hints Amendment

The ANSI Display Hints amendment permits ANSI sequences in test point
status keywords and directives, and explicitly excludes YAML
diagnostic blocks. This amendment extends ANSI support into YAML
diagnostic values. The two amendments are complementary:

- ANSI Display Hints governs the CRAP protocol lines themselves.
- This amendment governs the content within YAML diagnostic blocks.

A producer MAY implement either or both amendments independently.

### Interaction with Subtests

YAML diagnostic blocks in subtests follow the same rules as in the
parent document. The indentation of the YAML block does not affect
the handling of ANSI sequences within its values.

## Example

A YAML diagnostic block with colored diff output as it appears on a
terminal (SGR sequences shown in `\033[...m` notation):

```crap-2
CRAP-2
1::1
not ok 1 - output matches expected
  ---
  message: "output differs from expected"
  severity: fail
  diff: |
    \033[36m@@ -1,3 +1,3 @@\033[0m
     first line
    \033[31m-expected second line\033[0m
    \033[32m+actual second line\033[0m
     third line
  output: |
    \033[1mRunning tests...\033[0m
    \033[31mFAILED\033[0m: test_parse expected 42 got 41
  ...
```

After ANSI stripping by a harness writing to a log file, this is
equivalent to:

```crap-2
CRAP-2
1::1
not ok 1 - output matches expected
  ---
  message: "output differs from expected"
  severity: fail
  diff: |
    @@ -1,3 +1,3 @@
     first line
    -expected second line
    +actual second line
     third line
  output: |
    Running tests...
    FAILED: test_parse expected 42 got 41
  ...
```

## Security Considerations

The security considerations from the ANSI Display Hints amendment
apply here as well. ANSI escape sequences can encode more than
color — cursor movement, screen clearing, and terminal title changes
are all possible with broader escape codes. This amendment restricts
producers to SGR sequences only.

YAML diagnostic values may contain arbitrary text captured from child
processes. Harnesses that display YAML content to a terminal SHOULD
strip all `ESC [` CSI sequences, not just SGR, to prevent injection
of terminal control codes through captured output.

## Backwards Compatibility

YAML 1.2 parsers handle `0x1B` bytes in string values without issue,
so existing harnesses will parse YAML blocks containing ANSI
sequences correctly. The ANSI characters will appear as part of the
string value.

Harnesses that do not implement ANSI stripping will display raw
escape sequences when writing YAML values to a non-terminal. This is
cosmetically unpleasant but not a correctness issue — the YAML data
remains structurally intact and all values are preserved.

For maximum compatibility, producers SHOULD only emit ANSI sequences
in YAML values when writing to a terminal.

## Authors

This amendment is authored by Sasha F as an extension to the CRAP-2
specification by Isaac Z. Schlueter.
