CRAP: Command Result Accessibility Protocol
=========================================

*Don't dump garbage, flush CRAP.*

A fork of TAP focused on making trees of script output easy to visually
understand. Primarily meant for human consumers.

For the upstream TAP specification, visit the [website](https://testanything.org).

## Representations

CRAP-2's **canonical wire format** is **ndjson-crap** (newline-delimited
JSON), and its **canonical presenter** is the **viewport**. Producers emit
ndjson-crap; the viewport (the `crap-present` binary, or `:: present`)
renders it on a terminal — a live spinner, rolling output tail, progress, and
a persisted verdict line per test/node — with a plain fallback off a
terminal:

```sh
just --events-fd 1 build | crap-present --title build
:: go-test ./... | crap-present
```

ndjson-crap dries up the ecosystem's divergent ndjson-tap schemas
(tap-dancer's `tap-ndjson(7)` result model and just-us's `--events-fd`
execution model) into one superset. The line-oriented CRAP-2 text format is
now a **legacy presentation profile**.

## Specification

- [ndjson-crap schema (canonical wire format)](docs/ndjson-crap-schema.md)
- [CRAP version 2 specification](crap-version-2-specification.md) (text profile + concepts)
- [ANSI Display Hints amendment](ansi-display-amendment.md)
- [ANSI in YAML Output Blocks amendment](ansi-yaml-output-amendment.md)
- [Locale Number Formatting amendment](locale-formatting-amendment.md)
- [Status Line amendment](status-line-amendment.md)
- [Streamed Output amendment](streamed-output-amendment.md)
- [Subtest YAML Rewriting amendment](subtest-yaml-rewriting-amendment.md)
