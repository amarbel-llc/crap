---
layout: default
title: "CRAP-2 Amendment: Streamed Output"
---

<!-- SPDX-License-Identifier: Artistic-2.0 -->

# CRAP-2 Amendment: Streamed Output

## Problem

CRAP-2 captures process output in YAML diagnostic blocks, which are emitted
after a test point completes. Readers traditionally buffer the entire YAML block
--- waiting for the closing `...` marker --- before displaying any content. For
long-running tests, build steps, or interactive CI environments, this means
output is invisible until the test finishes.

Common workarounds include sending output to stderr (losing association with
test points) or emitting ad-hoc `#` comment lines (which readers treat as
unstructured noise). Neither approach preserves the structured association
between output and test points that YAML diagnostics provide.

## Solution

Define a new pragma, `streamed-output`, that tells readers to display lines of a
YAML diagnostic block's `output` field incrementally as they are written, rather
than buffering the entire block until the closing `...` marker.

## Specification

### Default State

The streamed output feature is enabled by default in CRAP-2. Producers do not
need to emit `pragma +streamed-output` to use it. Producers that do not want
this feature MAY disable it with:

``` crap-2
pragma -streamed-output
```

### Output Blocks

Output Blocks allow producers to stream process output *before* the test point
status is known. An Output Block consists of three parts:

1.  **Header**: A comment line identifying the test point:
    `# Output: <id> - <description>`
2.  **Body**: Zero or more lines, each indented by exactly 4 spaces. These lines
    are plain text representing captured process output --- they are not parsed
    as YAML or CRAP.
3.  **Closing test point**: A standard `ok` or `not ok` line at the parent
    indentation level with a matching ID and description.

Producers *should* emit the header before starting the associated process, so
readers can display a "running" indicator. Output lines *may* be flushed
individually as the process produces them, enabling real-time visibility in
terminal and CI environments.

The closing test point *must* be emitted after the process completes and the
pass/fail status is determined. An empty body (header followed immediately by
the test point) is valid.

### Output Block Example

A build step that compiles source files:

``` crap-2
CRAP-2
1::2
# Output: 1 - build
    compiling main.rs
    compiling lib.rs
    linking binary
ok 1 - build
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

### Reader Behavior

Readers *should* recognize the `# Output:` header and display body lines
incrementally as they arrive. When the closing test point is received, readers
*should* validate that its ID and description match the header.

Readers that do not recognize Output Blocks degrade gracefully: the header is
treated as a plain comment, body lines are treated as non-TAP content, and the
test point parses normally.

### Blank Lines in Output Blocks

Blank lines within the body *must* remain indented (4 spaces) to stay within the
block. An unindented blank line terminates the Output Block.

### YAML Incremental Delivery

When `streamed-output` is active (the default), producers *may* also flush
individual lines of a YAML block scalar value as they become available, rather
than buffering the entire block. This applies specifically to the `output` field
(and any field that represents captured process output, such as `stderr`).

Producers *must* ensure that each flushed line is a valid continuation of the
YAML block scalar --- that is, it maintains the correct indentation level
established by the block scalar indicator (`|` or `>`).

The YAML block remains structurally valid CRAP-2. The `---` marker opens the
block, the `...` marker closes it, and all content between them is valid YAML
1.2. The pragma changes only the expected delivery timing, not the format.

### Non-Output Fields

The incremental delivery guarantee applies only to fields representing captured
process output (`output`, `stderr`, and similar). Readers *should not* attempt
to incrementally display structured diagnostic fields such as `message`,
`severity`, `exitcode`, `file`, or `line`, which are typically short values
written atomically.

### Backwards Compatibility

Output Blocks and YAML diagnostic blocks are both structurally valid CRAP-2
whether streamed output is active or disabled. The only difference is whether
the reader displays output lines as they arrive or after the block/test point
closes. Readers that do not support incremental display will naturally buffer
with no ill effect.

### Subtests

In a subtest, the streamed output feature applies only to that subtest's
document, consistent with CRAP-2's rule that subtest pragmas do not affect
parent document parsing. A subtest may disable the feature with
`pragma -streamed-output`.

The parent document's streamed output state does not automatically apply to
child subtests --- each subtest inherits the default (enabled) state
independently.

### Interaction with ANSI in YAML Output Blocks

When both `streamed-output` and the ANSI in YAML Output Blocks amendment are in
effect, incrementally delivered `output` lines *may* contain ANSI SGR sequences,
subject to the same rules defined by that amendment (SGR only, TTY-gated,
`NO_COLOR` respected). Readers that display output lines incrementally *should*
pass through SGR sequences when writing to a terminal and strip them when
writing to a non-terminal, exactly as they would for a fully buffered YAML
block.

### Future Extensions

Future amendments *may* define conventions for distinguishing output streams
(e.g., separate `stdout` and `stderr` fields) or for signaling progress metadata
within the output. Readers *should not* assume any semantics beyond what is
defined here.

## Authors

This amendment is authored by Sasha F as an extension to the CRAP-2
specification by Isaac Z. Schlueter.
