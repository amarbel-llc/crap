---
layout: default
title: "CRAP-2 Amendment: Streamed Output"
---

<!-- SPDX-License-Identifier: Artistic-2.0 -->

# CRAP-2 Amendment: Streamed Output

## Problem

CRAP-2 captures process output in YAML diagnostic blocks, which are
emitted after a test point completes. Harnesses traditionally buffer
the entire YAML block — waiting for the closing `...` marker — before
displaying any content. For long-running tests, build steps, or
interactive CI environments, this means output is invisible until the
test finishes.

Common workarounds include sending output to stderr (losing association
with test points) or emitting ad-hoc `#` comment lines (which harnesses
treat as unstructured noise). Neither approach preserves the structured
association between output and test points that YAML diagnostics
provide.

## Solution

Define a new pragma, `streamed-output`, that tells harnesses to display
lines of a YAML diagnostic block's `output` field incrementally as they
are written, rather than buffering the entire block until the closing
`...` marker.

## Specification

### Activation

The pragma is activated with:

```tap
pragma +streamed-output
```

This pragma is document-wide. Once set, it applies to all subsequent
YAML diagnostic blocks in the document. Producers _must not_ deactivate
it with `pragma -streamed-output` after activating it.

### Incremental Output Delivery

When `streamed-output` is active, producers _may_ flush individual
lines of a YAML block scalar value as they become available, rather
than buffering the entire block. This applies specifically to the
`output` field (and any field that represents captured process output,
such as `stderr`).

Producers _must_ ensure that each flushed line is a valid continuation
of the YAML block scalar — that is, it maintains the correct
indentation level established by the block scalar indicator (`|` or
`>`).

The YAML block remains structurally valid CRAP-2. The `---` marker
opens the block, the `...` marker closes it, and all content between
them is valid YAML 1.2. The pragma changes only the expected delivery
timing, not the format.

### Harness Behavior

When `streamed-output` is active, harnesses _should_ display each line
of the `output` field as it arrives rather than waiting for the `...`
marker. This enables real-time observation of test output in terminal
and CI environments.

Harnesses _must_ still parse the complete YAML block for diagnostic
data after the `...` marker is received. Incremental display is a
presentation concern and does not affect how harnesses interpret the
diagnostic data.

Harnesses that do not support incremental display _may_ continue to
buffer the entire block. The resulting output is identical — only the
timing of display differs.

### Example

A build step that compiles, then runs tests. The `output` field's
content is flushed line-by-line as the build progresses:

```tap
CRAP version 2
pragma +streamed-output
1..2
ok 1 - build
  ---
  output: |
    compiling main.rs
    compiling lib.rs
    linking binary
  ...
not ok 2 - test
  ---
  message: "test_parse assertion failed"
  severity: fail
  exitcode: 1
  output: |
    running test suite
    FAIL: test_parse expected 42 got 41
  ...
```

Without the pragma, a harness would display nothing until `...` closes
each block. With the pragma active, a harness displays each line of the
`output` value as the producer writes it — `compiling main.rs` appears
immediately, `compiling lib.rs` follows as it happens, and so on.

### Non-Output Fields

The incremental delivery guarantee applies only to fields representing
captured process output (`output`, `stderr`, and similar). Harnesses
_should not_ attempt to incrementally display structured diagnostic
fields such as `message`, `severity`, `exitcode`, `file`, or `line`,
which are typically short values written atomically.

### Backwards Compatibility

Harnesses that do not recognize the `streamed-output` pragma _must_
ignore it, per the CRAP-2 specification's pragma rules. The YAML
diagnostic block is structurally identical with or without the pragma —
it is valid CRAP-2 in both cases. The only difference is whether the
harness displays output lines as they arrive or after the block closes.

### Subtests

In a subtest, `pragma +streamed-output` applies only to that subtest's
document, consistent with CRAP-2's rule that subtest pragmas do not
affect parent document parsing.

A parent document's `streamed-output` pragma does not automatically
apply to child subtests. Subtests that want streamed output _must_ emit
their own `pragma +streamed-output`.

### Interaction with ANSI in YAML Output Blocks

When both `streamed-output` and the ANSI in YAML Output Blocks
amendment are in effect, incrementally delivered `output` lines _may_
contain ANSI SGR sequences, subject to the same rules defined by that
amendment (SGR only, TTY-gated, `NO_COLOR` respected). Harnesses that
display output lines incrementally _should_ pass through SGR sequences
when writing to a terminal and strip them when writing to a
non-terminal, exactly as they would for a fully buffered YAML block.

### Future Extensions

Future amendments _may_ define conventions for distinguishing output
streams (e.g., separate `stdout` and `stderr` fields) or for signaling
progress metadata within the output. Harnesses _should not_ assume any
semantics beyond what is defined here.

## Authors

This amendment is authored by Sasha F as an extension to the CRAP-2
specification by Isaac Z. Schlueter.
