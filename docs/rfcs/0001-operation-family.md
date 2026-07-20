---
status: accepted
date: 2026-06-08
---

<!-- crap RFC 0001 (crap-local RFC series). Commit-ready.
     Author: madder/sunny-willow (Clown). Implementer: crap/vivid-olive.
     Home: docs/rfcs/0001-operation-family.md.
     Note: "crap RFC 000N" = this repo's series; "eng RFC 000N" = the eng-wide
     protocol series (0001 flake-input-go_mod, 0002 just-us --events-fd). -->

# CRAP-2 Operation Family and Unified Producer Reporter API

## Abstract

CRAP-2's `ndjson-crap` wire format has two record families: the *result*
family (test points — `plan`/`test`/`bailout`/`summary`) and the *execution*
family (command nodes — `node_start`/`command`/`output`/`node_end`). Both
persist one scrollback line per terminal unit. Neither expresses an
*operation*: a unit of work over many items that should render as a capped
rolling activity tail plus a progress bar and collapse to a single final
verdict, not N persisted lines. This RFC adds a third *operation* family
(`operation_start`/`progress`/`item`/`operation_end`) whose routine items
advance the viewport's progress bar and rolling tail without persisting a
line, while noteworthy items (failures) persist exactly as test points do. It
also specifies a producer-side Go reporter API that unifies all three
point-kinds — test points, operations, and execution nodes — so CLI tools
emit conformant `ndjson-crap` without hand-writing records.

## Introduction

`ndjson-crap` (the canonical CRAP-2 wire format; see [ndjson-crap v1]) is
rendered by one presenter, the CRAP-2 viewport. The viewport `Model` already
supports a rich operation UX — a capped rolling log tail, an item progress
bar and a byte progress bar (driven by an internal `OperationProgress`
message carrying `current`/`total`/`bytes`/`bytes_total`), phase boundaries,
and dimmed skip/todo directives. **No wire record currently reaches that
capability.** The result family's `test` record and the execution family's
`node_end` record each cause the driver to persist a verdict line
(`tea.Println`) above the live region. A producer doing an operation over
many items therefore has only two options today, both wrong:

1. Emit one `test` per item — every item persists a line, producing an
   unbounded scrollback wall (the live viewport regions stay empty).
2. Emit `output` records — these feed the capped tail, but cannot advance
   the progress bar (only `plan` + `test` arm and advance it) and provide no
   per-item verdict or counts.

The motivating case is `madder sync`: mapping each transferred blob to a
`test` record produced a growing, uncapped list of per-blob lines under the
spinner instead of the intended capped tail + progress bar + single verdict.

This specification is **additive** to `ndjson-crap` v1. The result and
execution families are unchanged. The execution family (just-us's
`--events-fd` model, [eng RFC 0002]) remains first-class and MUST NOT
regress; the operation family extends the same lineage rather than replacing
it.

Scope: this RFC defines (1) the operation-family wire records, (2) the
driver mapping from those records to viewport messages, including the
transient-vs-persisted rendering distinction, and (3) a producer-side Go
reporter API unifying the three point-kinds. It adds **one** new message to
the viewport `Model` — a persist-without-reset message for failed items
mid-operation (Section 7) — but changes no existing `Model` message and no
result/execution record schema.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD",
"SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be
interpreted as described in RFC 2119.

## Specification

### 1. Point-kinds and families

CRAP-2 streams describe three kinds of *points*. Each is a record family; a
single stream MAY mix all three.

| Point-kind | Family | Records | Persists a line? |
| --- | --- | --- | --- |
| Test point | result (existing) | `plan`, `test`, `bailout`, `summary` | always (one per `test`) |
| Execution node | execution (existing) | `node_start`, `command`, `output`, `node_end` | always (one per `node_end`) |
| Operation | operation (**new**) | `operation_start`, `progress`, `item`, `operation_end` | once per operation, plus one per *failed* item |

The operation family is the subject of this RFC. All operation-family records
carry `type` discriminators not present in v1 and are therefore invisible to
v1 consumers (which decode unknown `type`s to `Unknown` and ignore them, per
[ndjson-crap v1] forward-compatibility).

### 2. The `tp` namespace

Operation-family records reuse the existing stream-unique node id, `tp`, and
the `parent` linkage from the execution family, so operations, execution
nodes, and phases share one lineage tree. An `operation_start` MUST allocate a
fresh `tp`. `progress`, `item`, and `operation_end` records MUST reference
their operation's `tp` via an `op` field. An operation MAY be a child of
another node/operation via `parent`. Note that `tp`/`parent`/`depth` is
wire-level lineage for machine consumers and sequential nesting; the viewport
renders flat (Section 7), not as a live tree.

### 3. `operation_start`

Begins an operation.

```json
{"type":"operation_start","tp":3,"name":"sync","parent":null,"depth":0,"total":1280,"bytes_total":536870912}
```

- `tp` (integer, REQUIRED) — stream-unique id for this operation.
- `name` (string, REQUIRED) — human label shown as the live header.
- `parent` (integer or null, REQUIRED) — enclosing node/operation `tp`, or
  null at top level.
- `depth` (integer, REQUIRED) — nesting depth; 0 at top level.
- `total` (integer, REQUIRED) — expected item count. `0` means unknown
  (indeterminate bar). When `> 0` it arms the determinate item progress bar.
- `bytes_total` (integer, REQUIRED) — expected total bytes. `0` means
  unknown. When `> 0` it arms the determinate byte progress bar.

A producer that does not know `total`/`bytes_total` until a scan completes
MUST emit `operation_start` with `0` and MAY refine the totals via a later
`progress` record (Section 5) — this is the two-stage pattern (Section 9).

### 4. `item`

Reports the outcome of one work item within an operation.

```json
{"type":"item","op":3,"label":"blake2b256-q0vh… (1.2 MB)","state":"done","bytes":1258291,"diagnostic":null}
```

- `op` (integer, REQUIRED) — the `operation_start` `tp` this item belongs to.
- `label` (string, REQUIRED) — item display text (e.g. the blob id + size).
- `state` (string, REQUIRED) — one of `"done"`, `"skipped"`, `"failed"`.
- `bytes` (integer, REQUIRED) — bytes this item contributed; `0` if N/A.
- `diagnostic` (object or null, REQUIRED) — null for `done`/`skipped`; for
  `failed`, a JSON object carrying the failure detail (e.g.
  `{"error":"write failed: …"}`). ANSI SGR SHOULD be stripped for
  programmatic streams.
- `directive` (object or null, OPTIONAL, default null) — for `skipped`, MAY
  carry `{"kind":"skip","reason":<string>}` to drive the dimmed skip
  rendering; null otherwise.

**Rendering contract (the central decision of this RFC):**

- An `item` with `state":"done"` MUST be rendered as a *transient* rolling-tail
  entry (capped, deduped) and MUST advance the operation's progress, and MUST
  NOT persist a scrollback line.
- An `item` with `state":"skipped"` MUST be rendered transiently as above,
  visually marked as a skip (e.g. a dimmed `↷` prefix), and MUST advance
  progress; it MUST NOT persist a scrollback line.
- An `item` with `state":"failed"` MUST persist a scrollback verdict line
  (red `✗ <label>` plus the `diagnostic`), exactly as a failed `test` point
  does, and MUST advance progress. The driver maps this to the new `ItemFailed`
  message, NOT `PhaseEnded` (Section 7).

This reconciles two consumers: machine consumers parsing the stream receive
one record per item (the wire volume is identical to emitting one `test` per
item), while the viewport renders routine items transiently and surfaces only
failures as durable lines.

> **Design fork (resolved; alternative recorded).** An earlier option extended
> the existing `test` record with a `transient` boolean rather than adding an
> `item` record. This RFC instead introduces `item` so the result family's
> `test` keeps its fixed `tap-ndjson(7)` semantics (always a persisted test
> point with `n`/`subtest`/`line`) and the operation family owns the
> transient-rendering contract. A "test point" is thus precisely *an item that
> always persists*; a "progress item" persists only on failure. Implementers
> who prefer the flag-on-`test` approach SHOULD note it breaks the
> result-family/tap-ndjson invariant that every `test` is a counted, persisted
> point.

### 5. `progress`

Advances an operation's progress bars without naming an item and without
persisting a line. Use for byte-level progress between items, for refining
totals after a scan, or for high-volume operations that report aggregate
progress instead of one `item` per unit.

```json
{"type":"progress","op":3,"current":640,"total":1280,"bytes":268435456,"bytes_total":536870912,"label":"blake2b256-2mwp…"}
```

- `op` (integer, REQUIRED) — the operation `tp`.
- `current` (integer, REQUIRED) — items completed so far.
- `total` (integer, REQUIRED) — item denominator; `0` leaves it unchanged
  from the last known value (indeterminate if never set).
- `bytes` (integer, REQUIRED) — bytes completed so far.
- `bytes_total` (integer, REQUIRED) — byte denominator; `0` leaves it
  unchanged.
- `label` (string, OPTIONAL, default ""): when non-empty, MAY feed the rolling
  tail (transient); when empty, only the bars advance.

A `progress` record MUST NOT persist a scrollback line. A consumer MUST treat
`progress` as monotonic hints for display only; it carries no verdict.

### 6. `operation_end`

Terminates an operation with a tallied verdict. Exactly one per
`operation_start`.

```json
{"type":"operation_end","op":3,"done":1180,"skipped":99,"failed":1,"total":1280,"ok":false,"duration_ms":48213}
```

- `op` (integer, REQUIRED) — the operation `tp`.
- `done`, `skipped`, `failed` (integers, REQUIRED) — item tallies.
- `total` (integer, REQUIRED) — `done + skipped + failed`.
- `ok` (boolean, REQUIRED) — `failed == 0` AND the operation did not abort.
- `duration_ms` (integer, REQUIRED) — wall-clock duration.

`operation_end` MUST persist exactly one verdict line — a green `✓ <name> …`
summarizing the tallies on success, or a red `✗ <name> …` on failure — and
MUST reset the operation's live region (tail, bars). The tally text rides in
the verdict's `Description` (Section 7).

### 7. Driver mapping (records → viewport messages)

A conformant driver MUST map operation-family records to the viewport `Model`
messages as follows. Rows with multiple messages emit them **in the listed
left-to-right order**.

| Record | Viewport message(s), in order | Persist? |
| --- | --- | --- |
| `operation_start` | `OperationStarted{Name, Total}` — **always**, never `PhaseStarted`; then `OperationProgress{BytesTotal}` when `bytes_total > 0` | no |
| `progress` | `OperationProgress{Current, Total, Bytes, BytesTotal}`; then `LogLine{label}` when `label != ""` | no |
| `item` done | `LogLine{label}`; then `OperationProgress{Current: ++}` | no |
| `item` skipped | `LogLine{dimmed ↷ label}`; then `OperationProgress{Current: ++}` | no |
| `item` failed | `ItemFailed{Label, Diagnostic}` (**new** message); then `OperationProgress{Current: ++}` | **yes** |
| `operation_end` | `PhaseEnded{Description: "<name> — <done> done, <skipped> skipped, <failed> failed"}` then phase reset | **yes** |

The driver MUST continue to map the result and execution families exactly as
in v1 (unchanged). The driver MUST finalize the run (`BatchDone`) on stream
EOF even if no `operation_end`/`summary` arrived, so the program always quits.

**Required `Model` addition — `ItemFailed`.** A failed `item` MUST NOT reuse
`PhaseEnded`: that message calls `resetPhase()` (zeroing the progress bars and
clearing the rolling tail) and its failure render holds the *entire* current
tail above the verdict — so a single mid-operation failure would destroy the
operation's accumulated progress and dump every unrelated done-item label above
the `✗` line, contradicting Section 4 ("failed … MUST advance progress" while
the operation continues). Implementations MUST therefore add one new `Model`
message — RECOMMENDED `ItemFailed{Label string, Diagnostic map[string]any}`
(a general `PersistLine{Text string}` is an acceptable alternative) — whose
handler renders the `✗ <label>` verdict plus its diagnostic via `tea.Println`
and performs **no** phase reset and **no** tail-hold. For a failed item the
driver emits **two** messages in order: `ItemFailed` first (persisting the
verdict), then `OperationProgress{Current: ++}` (advancing the bar past the
failure). This is the only `Model` vocabulary change required by this RFC; all
existing messages are unchanged. (Validated against the real `Model`: after
`ItemFailed` the live region — `current`/`total`, `bytes`, and the tail — is
untouched and a following `OperationProgress` advances normally; the contrast
`PhaseEnded` leg resets the bar to 0/0, confirming the defect above.)

**`operation_start` always arms the bar.** It MUST map to `OperationStarted`
(which sets the item `total` and arms the determinate bar), never to
`PhaseStarted` (which resets the live region and does not arm a bar).
`OperationStarted` carries no byte total, so a non-zero `bytes_total` is armed
via a follow-on `OperationProgress{BytesTotal}` — keeping `ItemFailed` the only
new `Model` message. A nested `operation_start` (`parent != null`) still maps
to `OperationStarted` so child operations get a progress bar.

**`operation_end` tallies ride in `Description`.** The verdict's success
render path reads only `PhaseEnded.Description`, not its `Verdict.Diagnostic`;
therefore the human tally string ("… — 1180 done, 99 skipped, 1 failed") MUST
be carried in `Description`. `operation_end` maps to `PhaseEnded` (not the
`Model`'s non-persisting `OperationDone` message): `OperationDone` clears the
tail without persisting and suits only a single-operation, no-phase run,
whereas a multi-operation stream needs each operation to persist its own
verdict above the next operation's live region.

**Flat rendering; `tp`/`parent`/`depth` is wire lineage.** The viewport
`Model` is flat — one active phase, one rolling tail, one progress bar. The
`tp`/`parent`/`depth` fields (Section 2) are wire-level lineage for machine
consumers and for sequential nesting; the viewport renders operations flat and
sequentially, with the innermost currently-active operation owning the live
region. Genuine *simultaneous* nested live bars are out of scope for this RFC
and would require separate `Model` growth. The two-stage pattern (Section 9)
is sequential — the scan phase ends before the transfer operation begins — and
renders correctly on the flat `Model`.

### 8. Skip/exists rendering

`item` `state":"skipped"` is the operation-family equivalent of a `test`
`directive` of kind `skip`. Producers SHOULD set `directive` to
`{"kind":"skip","reason":<why>}` (e.g. `"exists"`) so the viewport renders the
dimmed `↷ <label> # SKIP <reason>` form and so machine consumers can
distinguish a skip from a transfer. Skipped items MUST NOT be counted as
`done` in `operation_end`.

### 9. Two-stage operations

An operation whose totals are unknown until a scan SHOULD be expressed in two
stages so the viewport shows a determinate bar:

1. **Scan** — emitted as a child phase (`node_start`/`node_end`, or a nested
   `operation_*` with indeterminate totals) that walks the work set and
   computes `total` and `bytes_total`.
2. **Transfer** — an `operation_start` carrying the scanned `total` and
   `bytes_total`, followed by per-item `item` records (and/or `progress`),
   ending in `operation_end`.

The two stages render sequentially on the flat `Model` (Section 7): the scan
phase persists its verdict and resets the live region, then the transfer
`operation_start` arms a fresh determinate bar. A producer that cannot afford
a pre-scan MAY emit a single-stage operation with `total: 0` (indeterminate
spinner-bar) and refine via `progress`.

### 10. Producer reporter API (Go)

`code.linenisgreat.com/crap/go-crap/v2/crap` (new package) MUST provide an
ergonomic reporter so producers do not hand-write `ndjsoncrap.Writer`
records. The API unifies the three point-kinds. The following is the
normative surface (names illustrative; signatures are the contract):

```go
// Reporter is the root. It wraps an io.Writer (ndjson-crap sink) and a tp
// allocator. Construct one per stream.
type Reporter struct { /* … */ }
func NewReporter(w io.Writer, opts ReporterOptions) *Reporter

// Test points (result family) — unchanged semantics, ergonomic surface.
type TestStream struct { /* … */ }
func (r *Reporter) TestStream(plan int) *TestStream
func (t *TestStream) Ok(desc string)
func (t *TestStream) NotOk(desc string, diagnostic map[string]any)
func (t *TestStream) Skip(desc, reason string)
func (t *TestStream) Finish() // emits summary

// Operations (operation family) — the new vocabulary.
type Operation struct { /* … */ }
func (r *Reporter) Operation(name string, opts OpOptions) *Operation // operation_start
type OpOptions struct { Total int; BytesTotal int64; Parent int }
func (o *Operation) Item(label string, bytes int64)          // item state=done
func (o *Operation) Skip(label, reason string)               // item state=skipped
func (o *Operation) Fail(label string, err error)            // item state=failed (persists)
func (o *Operation) Progress(current int, bytes int64)       // progress record
func (o *Operation) Phase(name string) *Phase                // nested node_start
func (o *Operation) Finish()                                 // operation_end (counts tallied by the Operation)

// Phases / execution nodes (execution family) — first-class, not regressed.
type Phase struct { /* … */ }
func (p *Phase) Command(cmd string)            // command
func (p *Phase) Output(stream, data string)    // output
func (p *Phase) Done()                         // node_end exit 0
func (p *Phase) Fail(err error)                // node_end nonzero / verdict
func (p *Phase) FailDiag(err error, diagnostic map[string]any) // Fail + node_end diagnostic
```

> **Amendment (2026-06-11, crap#22).** `node_end` gained an OPTIONAL
> `diagnostic` field ([ndjson-crap v1] schema) so an execution node is a
> self-sufficient verdict unit: producers attach the failure detail (summary,
> command, elapsed, resource link, …) to the node instead of pairing it with a
> duplicate result-family `test` record. `FailDiag` is the reporter surface
> for it; conformant presenters merge the producer diagnostic with their
> exit-code/signal synthesis (producer keys win) on the failure verdict.

- The `Operation` MUST tally `done`/`skipped`/`failed` from `Item`/`Skip`/`Fail`
  calls and emit them in `operation_end` at `Finish`; callers MUST NOT compute
  counts themselves.
- `Reporter` MUST allocate `tp`s and thread `op`/`parent` automatically.
- All emitters MUST produce records conformant to Sections 3–6.
- The API MUST NOT require the caller to know whether output is a TTY; whether
  the stream is rendered live (viewport) or written as wire bytes is the
  consumer's concern (e.g. `viewport.Present` vs a file), not the producer's.

### 11. Worked consumer mappings

**madder sync (operation over items, two-stage, skips).** Phase 1 scans the
source (and probes the destination) to compute blob count + total bytes;
emitted as a `scan` phase. Phase 2 is an `operation_start{name:"sync",
total:N, bytes_total:B}`; each blob is `Item` (transferred), `Skip`
(already-present, reason `"exists"`), or `Fail` (transfer error); `Finish`
emits the tallied `operation_end`. A remote (SFTP) store bootstrap is a
preceding `Phase("connect sftp://…")` with `Output` lines and `Done`/`Fail`.

**piggy / ssh-agent-mux health (pure test points).** A `TestStream`: one
`Ok`/`NotOk` per check (`"agent socket reachable"`, `"key loaded"`), then
`Finish` → `summary`. No operation-family records; the result family is
unchanged, proving the new family is purely additive.

**just-us recipes (execution family, first-class).** Unchanged from
[eng RFC 0002]: `node_start`/`command`/`output`/`node_end` per recipe. just-us
MAY adopt the `Phase` reporter surface but its existing `--events-fd` emitter
remains conformant. The operation family does not alter execution-family
rendering.

Further consumers the API generalizes to (informative): dodder
(init/fsck/import/export — fsck is an operation over blobs; cf. dodder#243),
cutting-garden (capture/restore/diff — operations over filesystem entries),
spinclass (merge/clean — operations with phases).

## Security Considerations

The operation family is a display/telemetry wire format; it conveys no
authority and triggers no privileged action. Three considerations apply:

- **Untrusted labels and diagnostics.** `label`, `name`, and `diagnostic`
  values MAY contain attacker-influenced data (e.g. a path or remote error).
  Producers MUST emit UTF-8 with invalid sequences replaced by U+FFFD and MUST
  NOT disable the consumer's terminal safety; the viewport renders text and
  MUST NOT interpret label/diagnostic content as terminal control sequences.
  Consumers SHOULD strip or escape ANSI SGR/control bytes from these fields
  before display (the schema already RECOMMENDS SGR stripping for programmatic
  streams).
- **Information disclosure.** `output`, `diagnostic`, and `label` may carry
  sensitive material (paths, hostnames, error bodies). This is a property of
  the producer, not the format; producers SHOULD redact secrets before
  emission. The format adds no new disclosure channel beyond the existing
  `output`/`diagnostic` fields.
- **Resource use.** A malicious or buggy producer could emit unbounded
  `item`/`progress` records. Consumers MUST bound memory: the rolling tail is
  already capped, and a conformant driver MUST NOT retain per-item history for
  transient (`done`/`skipped`) items. The existing 64 MiB per-record line cap
  applies unchanged.

## Conformance Testing

Conformance tests for the wire format and driver mapping live in the crap
repo at `go-crap/zz-tests_bats/` (operation-family suite) alongside the
existing ndjson-crap tests.

Tests use binary injection via `bats-emo`:

    require_bin CRAP_PRESENT crap-present

### Covered Requirements

| Requirement | Test File | Description |
| --- | --- | --- |
| §4, `item` done/skipped MUST NOT persist | `operation_render.bats` | Pipe a stream of done/skipped items to `crap-present` (plain fallback); assert no per-item verdict lines, only the `operation_end` verdict. |
| §4, `item` failed MUST persist | `operation_render.bats` | A failed item produces a `✗ <label>` line with its diagnostic. |
| §5, `progress` MUST NOT persist | `operation_render.bats` | A stream of `progress` records yields no scrollback lines. |
| §6, `operation_end` MUST persist one verdict with tallies | `operation_render.bats` | Assert exactly one `✓/✗ <name>` line reflecting done/skipped/failed. |
| §7, `ItemFailed` persists without resetting the bar | `operation_render.bats` | A failed item mid-stream keeps the subsequent progress determinate (bar not reset to 0/0). |
| §7, unknown→Unknown forward-compat | `operation_compat.bats` | A v1 consumer fed operation-family records ignores them without error. |
| §10, reporter API emits conformant records | Go test `crap_reporter_test.go` | Round-trip: `Reporter` calls decode back via `ndjsoncrap.Reader` to the records in §3–6. |

## Compatibility

- **Additive to `ndjson-crap` v1.** No existing record schema changes; the
  result and execution families are byte-compatible. The `ndjson` schema
  version remains `1` (new record `type`s are the v1-specified extension
  point; no existing record changed).
- **Forward compatibility.** v1 consumers decode `operation_start`/`progress`/
  `item`/`operation_end` to `Unknown` and ignore them, per [ndjson-crap v1].
  Producers targeting mixed-version consumers MAY additionally emit a trailing
  result-family `summary` mirroring the `operation_end` tally so v1 presenters
  still show a terminal aggregate.
- **Viewport `Model`.** Adds one message (`ItemFailed`); no existing message
  changes. A presenter built before this RFC simply lacks operation rendering
  (it would ignore the unknown wire records); presenters MUST add the
  `ItemFailed` handler plus the operation-family driver mappings to conform.
- **Module path.** The reporter API ships under
  `code.linenisgreat.com/crap/go-crap/v2` (the `/v2` module). No v0/v1
  consumers exist for the new `crap` package.
- **Producer migration.** Tools currently hand-writing `ndjsoncrap.Writer`
  records (e.g. madder sync's `syncCrapSink`) SHOULD migrate to the reporter
  API; the raw `Writer` remains supported for the result/execution families.

## References

### Normative

- [RFC 2119] Key words for use in RFCs to Indicate Requirement Levels.
- [ndjson-crap v1] `docs/ndjson-crap-schema.md`, CRAP-2 — the wire format this
  RFC extends.
- [eng RFC 0002] just-us `--events-fd` execution model — the execution family
  this RFC composes with.

### Informative

- [eng RFC 0001] flake-input-go_mod — eng-wide protocol RFC (numbering-series
  lineage; the eng-wide series is distinct from this crap-local RFC series).
- CRAP-2 specification (`crap-version-2-specification.md`).
- purse-first FDR 0010 operation_viewport / cutting-garden `capture_viewport`
  — the viewport `Model` lineage whose `OperationProgress` message this RFC's
  `progress` record targets.
- madder#234 (adopt crap output across madder's streaming commands);
  dodder#243 (fsck progress UI).
