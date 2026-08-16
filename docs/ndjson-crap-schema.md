# ndjson-crap schema (v1)

`ndjson-crap` is the canonical newline-delimited-JSON wire format for CRAP-2
streams. It is a deliberate union ("drying") of the divergent
newline-delimited-JSON schemas that grew up across the ecosystem, so a single
presenter — the CRAP-2 viewport (`crap-present` / `:: present`) — can render
any of them.

> Status: **Phase 1 foundation.** This document specifies the wire format and
> the compatibility mappings. The CRAP-2 specification rewrite that adopts
> ndjson-crap as the canonical format (demoting the line-oriented CRAP-2 text
> format) is Phase 2 and is intentionally **not** done here.

## Encoding

- UTF-8, one JSON object per line, newline-delimited (`\n`).
- Producers MUST replace invalid UTF-8 with U+FFFD.
- HTML escaping MUST be disabled (`<`, `>`, `&` emitted literally).
- Field order is a SHOULD, not a MUST; consumers MUST NOT depend on it
  (RFC 8259).

## Record families

Every record carries a `type` discriminator. Records fall into three
families; a single stream MAY mix them.

### Result family (from `tap-ndjson(7)`)

Field-for-field compatible with tap-dancer's `tap-ndjson(7)`, so any
tap-ndjson stream is a valid ndjson-crap stream.

| `type`      | purpose                                       |
| ----------- | --------------------------------------------- |
| `plan`      | declared count of top-level test points       |
| `test`      | one test point (recurses via `subtest`)        |
| `bailout`   | abnormal termination                           |
| `summary`   | terminal aggregate (exactly one per result stream) |

### Execution family (from just-us `--events-fd`, RFC 0002)

A streaming, command-execution model. just-us's discriminators
(`recipe_start`, `recipe_command`, `recipe_complete`, and a `plan` carrying
`recipe_count`) are accepted directly and normalized to the canonical names
below.

| canonical `type` | just-us alias      | purpose                                  |
| ---------------- | ------------------ | ---------------------------------------- |
| `node_start`     | `recipe_start`     | a unit of execution begins (recipe/phase/step) |
| `command`        | `recipe_command`   | a command about to run under a node       |
| `output`         | `output`           | a chunk of a node's child output          |
| `node_end`       | `recipe_complete`  | a node finishes (exit code / signal)      |

### Operation family (crap RFC 0001)

A unit of work over many items that renders as a capped rolling tail + a
progress bar collapsing to one verdict, rather than one persisted line per
item. Routine items advance the bar transiently (no persist); only failures
persist. Additive to ndjson-crap v1 — `ndjson` stays `1`. See
[`docs/rfcs/0001-operation-family.md`](rfcs/0001-operation-family.md) for the
normative spec (records, driver mapping, and the producer reporter API).

| `type`            | purpose                                          |
| ----------------- | ------------------------------------------------ |
| `operation_start` | an operation begins (arms the bar)               |
| `progress`        | advance the bars without naming an item (no persist) |
| `item`            | one item's outcome (`done`/`skipped`/`failed`)   |
| `operation_end`   | terminal tallied verdict (one per `operation_start`) |

### Header

| `type`  | purpose                                   |
| ------- | ----------------------------------------- |
| `crap`  | optional stream header; MUST be first when present |

### Attachment (crap RFC 0002, draft)

The CRAP attach protocol
([`docs/rfcs/0002-attach-protocol.md`](rfcs/0002-attach-protocol.md))
lets a harness advertise — via a single `CRAP=2` environment variable —
that it consumes this format. Attached nodes are clients of a sink
server (a unix socket birthed by the tree's root) that grants each
connection a disjoint node-id base and splices record lines into one
merged stream, so a process tree nests via the existing
`tp`/`parent` lineage. It amends this schema additively:
result-family records gain an OPTIONAL `parent` for lineage under an
execution node, and node ids may start from a granted base (this
schema requires only stream-uniqueness).

## Record schemas

### `crap` (header)

```json
{"type":"crap","version":2,"ndjson":1,"title":"go test ./...","source":"go-test"}
```

- `version` — CRAP major version (`2`).
- `ndjson` — ndjson-crap schema version (`1`).
- `title`, `source` — optional presentation hints (`omitempty`).

### `plan`

```json
{"type":"plan","count":2}
```

- `count` — number of top-level test points. just-us emits `recipe_count`
  (plus `version`); the reader maps `recipe_count` → `count`.

### `test`

All fields are always present; nullable fields use `null`.

```json
{"type":"test","n":2,"description":"parses negative numbers","ok":false,"directive":null,"diagnostic":{"message":"expected 42 got 41","severity":"fail"},"output":null,"subtest":null,"line":7}
```

- `directive` — `null` or `{"kind":"skip"|"todo","reason":<string>}`.
- `diagnostic` — `null` or a JSON object (parsed YAML diagnostic; ANSI SGR
  SHOULD be stripped for programmatic streams).
- `output` — `null` or the captured output block as a string.
- `subtest` — `null` or an array of nested `test` records.
- `line` — 1-indexed source line, or `0` for direct producers.

### `bailout`

```json
{"type":"bailout","message":"database unreachable","line":42}
```

### `summary`

```json
{"type":"summary","passed":7,"failed":3,"skipped":0,"todo":0,"total":10,"plan_count":10,"bailed":false,"valid":true,"diagnostics":[]}
```

- `total` = `passed + failed + skipped + todo`; subtests are NOT counted.
- `valid` is independent of `bailed`.
- `diagnostics` — array of `{line, severity, rule, message}`; empty when valid.

### `node_start`

```json
{"type":"node_start","tp":2,"name":"build","namepath":"build","depth":0,"parent":null,"doc":null,"quiet":false}
```

- `tp` — stream-unique node id.
- `parent` — `null` or the `tp` of the enclosing node.

### `command`

```json
{"type":"command","tp":1,"command":"go build ./...","line":3}
```

### `output`

```json
{"type":"output","tp":1,"stream":"stdout","format":"utf8","data":"compiling\n"}
```

- `stream` — `"stdout"` | `"stderr"`.
- `format` — `"utf8"` | `"base64"` (defaults to `"utf8"` when absent).

### `node_end`

```json
{"type":"node_end","tp":1,"exit_code":1,"signal":null,"duration_ms":120,"diagnostic":{"error":"hook failed","command":"just"}}
```

- Exactly one of `exit_code` / `signal` is non-null for a process-backed node.
- Success is `exit_code == 0` with `signal == null`.
- `diagnostic` — OPTIONAL (default `null`): a JSON object carrying producer
  verdict detail (failure summary, command, elapsed, resource link, …), shaped
  like `test`'s `diagnostic`. SHOULD be `null`/absent on success. Presenters
  merge it with their own exit-code/signal synthesis (producer keys win), so an
  execution node is a self-sufficient verdict unit — producers MUST NOT pair a
  node with a duplicate result-family `test` record just to carry a diagnostic.
  Absent in just-us streams (the field is additive; absent decodes to null).

## Forward compatibility

- New fields MAY be added; existing fields MUST NOT be removed and their types
  MUST NOT change.
- New record `type`s MAY be added. Consumers MUST ignore unrecognized record
  types (the Go reader decodes them to an `Unknown` record rather than
  erroring). Producers MUST NOT emit record types outside this schema.

## Go API

`code.linenisgreat.com/crap/go-crap/v2/ndjsoncrap`:

- `Reader` / `Writer` — tolerant decode (accepts just-us aliases, skips blank
  lines, unknown types → `Unknown`) and canonical encode.
- record types: `Meta`, `Plan`, `Test`, `Directive`, `Bailout`, `Summary`,
  `NodeStart`, `Command`, `Output`, `NodeEnd`, `OperationStart`, `Progress`,
  `Item`, `OperationEnd`, `Unknown`.

`code.linenisgreat.com/crap/go-crap/v2/viewport`:

- `Present(in, Options)` — render an ndjson-crap stream via the bubbletea
  viewport on a TTY, or a plain verdict-per-line fallback off a TTY.
- `Model` / message types / `Driver` — the presenter internals (extracted from
  cutting-garden's `capture_viewport`, itself a WET copy of purse-first
  FDR 0010's operation_viewport).

`code.linenisgreat.com/crap/go-crap/v2/crap`:

- `Reporter` — the producer-side API (crap RFC 0001 §10): `TestStream`
  (result family), `Operation` (operation family), and `Phase` (execution
  family), so tools emit conformant ndjson-crap without hand-writing records.

## CLI

- `crap-present [--title <s>] [--tail <n>]` — standalone presenter; reads
  ndjson-crap on stdin, renders to the terminal (stderr). For subprocess-pipe
  consumers that cannot import the Go library:

  ```sh
  just --events-fd 1 build | crap-present --title build
  ```

- `:: present [flags]` — the same surface from the `large-colon` toolkit. It
  delegates to `crap-present` (resolved on PATH or alongside the `::`
  executable) rather than importing it: the presenter pulls in bubbletea,
  whose `init()` probes the terminal for its background color, and that probe
  must not run for the general-purpose `::` subcommands.
