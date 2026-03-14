---
layout: default
title: "CRAP-2 Amendment: ANSI Display Hints"
---

<!-- SPDX-License-Identifier: Artistic-2.0 -->

# CRAP-2 Amendment: ANSI Display Hints

## Abstract

This amendment defines how CRAP-2 producers MAY embed ANSI escape
sequences in test point status keywords (`ok`, `not ok`, `Bail out!`)
and directives (`# SKIP`, `# TODO`) to provide colored terminal output.
It specifies where sequences are permitted, how harnesses SHOULD handle
them, and how the CRAP stream remains valid and parseable regardless of
whether a consumer supports color.

## Problem

CRAP output is read by both machines (harnesses, CI systems, log
aggregators) and humans (developers watching test runs in a terminal).
For human readers, a wall of monochrome `ok` and `not ok` lines makes
failures hard to spot at a glance. Colored output — green for pass, red
for fail — is a widely expected affordance in modern test runners, but
CRAP-2 does not address it.

Producers that want colored output today must either:

1. Emit ANSI codes unconditionally, breaking harnesses that match on
   exact strings like `/^ok/` or `/^not ok/`.
2. Avoid color entirely, sacrificing readability.
3. Use a non-standard out-of-band channel (stderr), losing association
   with the CRAP stream.

This amendment provides a standard way to embed display hints that
preserves machine parseability.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119.

## Specification

### Permitted Locations

Producers MAY wrap the following tokens in ANSI SGR (Select Graphic
Rendition) escape sequences:

| Token | Typical Color |
|-------|---------------|
| `ok` (in test status) | Green (SGR 32) |
| `not ok` (in test status) | Red (SGR 31) |
| `# SKIP` (directive) | Yellow (SGR 33) |
| `# TODO` (directive) | Yellow (SGR 33) |
| `Bail out!` | Red (SGR 31) |

An SGR sequence has the form `ESC [ <code> m`, where `ESC` is the byte
`0x1B`. The reset sequence is `ESC [ 0 m`.

Producers MUST wrap entire tokens, not partial tokens. For example,
`\033[32mok\033[0m` is valid; `\033[32mo\033[0mk` is not.

Producers MUST NOT insert ANSI sequences in any other part of a CRAP
line, including:

- Test point IDs
- Descriptions
- YAML diagnostic blocks
- Plan lines
- Pragma lines
- Subtest comment headers (`# Subtest:`)

### When to Emit Color

Producers SHOULD emit ANSI sequences only when standard output is a
terminal (TTY). Producers MUST NOT emit ANSI sequences when output is
redirected to a file or pipe, unless the user has explicitly requested
color (e.g., via a `--color=always` flag).

Producers MAY respect the `NO_COLOR` environment variable
([no-color.org](https://no-color.org)). When `NO_COLOR` is set to a
non-empty value, producers SHOULD suppress ANSI sequences regardless of
TTY detection.

### Harness Behavior

Harnesses MUST strip ANSI SGR escape sequences (the pattern
`ESC [ ... m`) from test point lines before parsing. This ensures that
colored CRAP streams are parsed identically to uncolored streams.

Harnesses that display results to a terminal MAY pass through or
regenerate ANSI sequences in their own output.

Harnesses that do not implement ANSI stripping SHOULD still function
correctly with uncolored CRAP streams. The presence of ANSI sequences
in a CRAP stream that is not a TTY indicates a non-conformant producer,
and harnesses MAY treat such streams as containing non-CRAP characters.

### Interaction with Subtests

ANSI sequences in a subtest's test points are indented along with the
rest of the subtest content. Harnesses MUST strip ANSI sequences before
checking indentation level.

### Interaction with Escaping

ANSI escape characters (`0x1B`) are not `#` or `\` and are therefore
not subject to CRAP-2 escaping rules. They do not need to be escaped
and MUST NOT be interpreted as escape-sequence initiators for CRAP
escaping purposes.

## Example

A colored CRAP stream as it appears on a terminal (SGR sequences shown
in `\033[...m` notation):

```tap
CRAP version 2
1..3
\033[32mok\033[0m 1 - database connection
\033[31mnot ok\033[0m 2 - query returns results
  ---
  message: "expected 5 rows, got 0"
  severity: fail
  ...
\033[32mok\033[0m 3 - cleanup \033[33m# SKIP\033[0m not needed
```

After ANSI stripping by a harness, this is equivalent to:

```tap
CRAP version 2
1..3
ok 1 - database connection
not ok 2 - query returns results
  ---
  message: "expected 5 rows, got 0"
  severity: fail
  ...
ok 3 - cleanup # SKIP not needed
```

## Security Considerations

ANSI escape sequences can encode more than color — cursor movement,
screen clearing, and terminal title changes are all possible with
broader escape codes. This amendment restricts producers to SGR
sequences only (the `ESC [ <params> m` form). Harnesses that strip
escapes SHOULD strip all `ESC [` CSI sequences, not just SGR, to
prevent injection of terminal control codes through crafted test
descriptions or diagnostic output.

Producers MUST NOT emit non-SGR escape sequences in CRAP output.

## Compatibility

### Backwards Compatibility with CRAP-2

CRAP-2 specifies that harnesses SHOULD NOT treat invalid CRAP lines as
test failure by default, and that non-CRAP characters are silently
ignored or passed through. A CRAP-2 harness that does not implement
ANSI stripping will typically still parse colored output correctly if
ANSI sequences appear only around the status keywords, because:

1. The `ok` and `not ok` tokens remain present in the line.
2. Most regex-based parsers match on `/^(not )?ok/` which may or may
   not match with a leading `ESC [` sequence.

For maximum compatibility, producers SHOULD only emit colored output to
terminals and MUST produce clean (ANSI-free) CRAP when piped.

### Forwards Compatibility

Future CRAP versions MAY define a pragma (e.g., `pragma +color`) to
signal that a stream contains ANSI sequences. This amendment does not
define such a pragma, as TTY detection by the producer is sufficient
for current use cases.

## Authors

This amendment is authored by Sasha F as an extension to the CRAP-2
specification by Isaac Z. Schlueter.
