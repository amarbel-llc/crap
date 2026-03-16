---
layout: default
title: "CRAP-2 Amendment: Locale Number Formatting"
---

<!-- SPDX-License-Identifier: Artistic-2.0 -->

# CRAP-2 Amendment: Locale Number Formatting

## Problem

CRAP-2 uses plain ASCII integers for test point IDs and plan counts.
When test suites grow large, numbers like `1::10000` or `ok 9999` are
hard to read at a glance. Many locales use grouping separators to
improve readability of large numbers — `1::10,000` (en-US),
`1::10.000` (de-DE), `1::10 000` (fr-FR) — but CRAP-2 has no
mechanism to signal that these formats are in use.

Without an explicit locale declaration, readers cannot distinguish
between `1.000` meaning "one thousand" (de-DE grouping) and `1.000`
meaning a malformed decimal. Producers that emit locale-formatted
numbers today will break any reader that expects plain integers.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119.

## Specification

### Activation

The pragma is activated with a BCP 47 language tag:

```crap-2
pragma +locale-formatting:de-DE
```

The tag after the colon MUST be a valid BCP 47 language tag (e.g.,
`en-US`, `de-DE`, `fr-FR`, `ja-JP`). The pragma without a tag is
invalid and readers MUST ignore it.

When active, test point IDs and plan counts MAY use the grouping
separator convention of the declared locale.

### Scope

The pragma applies from the point of declaration to the end of the
current CRAP document. Producers MUST NOT deactivate it with
`pragma -locale-formatting` after activating it.

In subtests, the pragma follows CRAP-2's scoping rules: a subtest's
locale-formatting pragma does not affect the parent document.
Subtests that want locale-formatted numbers MUST emit their own
pragma.

### Affected Positions

When active, the following positions MAY contain locale-formatted
integers:

- **Test point IDs**: `ok 1,234` (en-US), `ok 1.234` (de-DE)
- **Plan counts**: `1::10,000` (en-US), `1::10.000` (de-DE)

The formatted numbers MUST represent the same integer value as their
plain ASCII equivalent. Grouping separators are purely cosmetic and
MUST NOT change the numeric value. Decimal fractions are not valid
in these positions regardless of locale.

### Parsing Rules

Readers that recognize this pragma MUST:

1. Strip all grouping separators defined by the declared locale from
   test point IDs and plan counts before numeric comparison.
2. Use the resulting plain integer for all internal bookkeeping (plan
   validation, test point counting, ID uniqueness checks).

Readers that do not recognize the pragma will ignore it per
CRAP-2's pragma rules, and will likely fail to parse the
locale-formatted numbers. This is expected — see Backwards
Compatibility below.

### Locale Grouping Conventions

The following common grouping conventions are defined for reference.
Readers MAY support additional locales beyond this list.

| Locale | Grouping Separator | Example (ten thousand) |
|--------|--------------------|------------------------|
| en-US  | `,` (comma)        | `10,000`               |
| de-DE  | `.` (period)       | `10.000`               |
| fr-FR  | ` ` (space)        | `10 000`               |
| hi-IN  | `,` (comma, lakh grouping) | `10,000`        |
| ja-JP  | `,` (comma)        | `10,000`               |

Producers MUST use grouping separators consistently with the declared
locale throughout the document.

### Example

```crap-2
CRAP-2
pragma +locale-formatting:en-US
1::1,200
ok 1 - initial setup
ok 2 - database connection
# ...
ok 1,199 - cleanup temporary files
ok 1,200 - final teardown
```

After stripping grouping separators, a reader interprets this as
`1::1200` with test points 1 through 1200.

A German-locale example:

```crap-2
CRAP-2
pragma +locale-formatting:de-DE
1::1.200
ok 1 - Ersteinrichtung
ok 2 - Datenbankverbindung
# ...
ok 1.199 - temporäre Dateien aufräumen
ok 1.200 - abschließender Abbau
```

### Backwards Compatibility

Readers that do not recognize the `locale-formatting` pragma MUST
ignore it per CRAP-2's pragma rules. However, they will encounter
test point IDs and plan counts containing non-digit characters, which
they will likely treat as non-CRAP or parse incorrectly.

For this reason, producers SHOULD only activate locale formatting
when they know the consuming reader supports this amendment.
Producers targeting maximum compatibility SHOULD NOT use this pragma.

### Interaction with YAML Diagnostics

This amendment does not affect YAML diagnostic blocks. Numbers in
YAML blocks follow YAML 1.2 rules regardless of whether
`locale-formatting` is active.

### Interaction with Escaping

Grouping separators (`,`, `.`, ` `) are not `#` or `\` and are
therefore not subject to CRAP-2 escaping rules.

## Authors

This amendment is authored by Sasha F as an extension to the CRAP-2
specification by Isaac Z. Schlueter.
