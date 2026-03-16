---
layout: default
title: "CRAP-2 Amendment: Subtest YAML Rewriting"
---

<!-- SPDX-License-Identifier: Artistic-2.0 -->

# CRAP-2 Amendment: Subtest YAML Rewriting

## Problem

CRAP-2 subtests are indented CRAP documents nested inside a parent document.
A subtest's YAML diagnostic block may itself contain a CRAP document — for
example, when a test point captures the output of a child process that
produces CRAP. If that YAML block's value begins with a CRAP version line
(e.g., `CRAP-2`), the embedded document is a complete CRAP stream,
but the harness has no mechanism to interpret its test points as part of
the subtest hierarchy.

Without special handling, the embedded CRAP document is treated as an
opaque string. Its test point results are invisible to the harness — they
do not contribute to pass/fail counts, are not displayed in hierarchical
reporters, and cannot be individually addressed for re-running or
filtering.

## Solution

Allow harnesses to detect when a YAML diagnostic block's value begins
with a CRAP document line and rewrite that block so the embedded test
points are output as a properly indented subtest. This gives harnesses
the option to promote embedded CRAP documents into the subtest tree
without requiring producers to change their output format.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119.

## Specification

### Detection

A YAML diagnostic block is eligible for subtest rewriting when ALL of the
following are true:

1. The block is attached to a test point within a subtest (i.e., at any
   indentation level greater than 0).
2. The block contains a field whose block scalar value begins with a
   valid CRAP version line (`CRAP-2`).

Harnesses MAY check any field name, but the `output` field is the most
common location for captured CRAP output. Harnesses SHOULD document which
fields they inspect.

### Rewriting

When a harness detects an eligible YAML block, it MAY rewrite the
enclosing subtest's document so that the test points from the embedded
CRAP document are emitted as a nested subtest at the appropriate
indentation level.

The rewriting process:

1. The harness parses the embedded CRAP document from the YAML field
   value according to the CRAP-2 specification.
2. The harness emits the embedded document's test points, plan, and
   other CRAP lines as a subtest indented one level deeper than the
   test point to which the YAML block is attached.
3. The original YAML diagnostic block MAY be retained (with the CRAP
   content removed or replaced by a reference), or it MAY be omitted
   entirely. If retained, the harness MUST ensure the resulting YAML
   remains valid.
4. The correlated test point for the rewritten subtest MUST preserve
   the original test point's status (`ok` or `not ok`), ID, and
   description.

### Harness Behavior

Harnesses that implement subtest rewriting MUST:

- Parse the embedded CRAP document fully before emitting the rewritten
  subtest, to ensure the document is valid.
- Preserve the pass/fail semantics of the original test point. The
  rewritten subtest is a presentation expansion — it MUST NOT change
  whether the parent test point is `ok` or `not ok`.

Harnesses that do not implement subtest rewriting MUST continue to treat
the YAML block as opaque diagnostic data, per standard CRAP-2 behavior.

### Example

A test runner executes a child process that produces CRAP. The producer
captures the child's output in a YAML diagnostic block:

```crap-2
CRAP-2
1::1
# Subtest: integration suite
    1::1
    ok 1 - child process
      ---
      output: |
        CRAP-2
        1::2
        ok 1 - database connects
        not ok 2 - query returns results
      ...
ok 1 - integration suite
```

A harness implementing this amendment MAY rewrite the document so the
embedded CRAP output appears as a nested subtest:

```crap-2
CRAP-2
1::1
# Subtest: integration suite
    1::1
    # Subtest: child process
        1::2
        ok 1 - database connects
        not ok 2 - query returns results
    ok 1 - child process
ok 1 - integration suite
```

The harness may now report `database connects` and `query returns results`
as individual test points within the hierarchy.

### Subtests

Subtest rewriting is recursive. If a rewritten subtest itself contains
YAML blocks with embedded CRAP documents, the harness MAY apply the same
rewriting rules at each level of nesting.

Harnesses MUST guard against unbounded recursion. A reasonable
implementation limit (e.g., matching the harness's maximum subtest
nesting depth) is sufficient.

### Backwards Compatibility

This amendment defines optional harness behavior — it does not change the
CRAP-2 wire format. Producers are not required to change their output.
Harnesses that do not implement rewriting continue to function correctly,
treating embedded CRAP in YAML blocks as opaque text.

The rewritten output is itself valid CRAP-2. Downstream consumers that
receive the rewritten stream see standard subtests and require no special
handling.

## Authors

This amendment is authored by Sasha F as an extension to the CRAP-2
specification by Isaac Z. Schlueter.
