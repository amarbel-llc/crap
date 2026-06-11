---
status: draft
date: 2026-06-11
---

<!-- crap RFC 0002 (crap-local RFC series). Design sketch — the protocol
     core is implemented as a prototype in just-us (see Conformance
     Testing); the go-crap `attach` package and `:: attach` are future
     work. Home: docs/rfcs/0002-attach-protocol.md.
     Note: "crap RFC 000N" = this repo's series; "eng RFC 000N" = the
     eng-wide protocol series (0002 = just-us --events-fd). -->

# CRAP Attach Protocol: Harness Detection, Content Negotiation, and Recursive Structured-Output Trees

## Abstract

A program that can emit `ndjson-crap` has no way to learn whether the thing
running it wants that — so today every producer needs an explicit opt-in flag
(`just --events-fd 3`, `:: go-test`), and a tree of programs cannot compose
into one nested stream without each layer being hand-wired. This RFC
specifies the **CRAP attach protocol**. Its core is a single environment
variable: a harness that accepts CRAP-2 structured output exports `CRAP=2`,
and a capable program detects it, announces itself with a one-line JSON
*hello*, and emits its stream — by default on stdout, so
`CRAP=2 just build | crap-present` is the whole deployment. Optional
variables refine the offer: `CRAP_FD` moves the channel off stdout,
`CRAP_ACCEPT` negotiates among formats (enabling future alternatives such as
a compact binary encoding), and `CRAP_PARENT`/`CRAP_DEPTH` make the protocol
**recursive** — an attached producer re-offers to its own children, so a
process tree (subprocesses, or shell-managed pipes deep inside it)
materializes as a nested node tree in a single stream, and check marks nest
to mirror the processes that produced them. Unaware programs ignore the
environment and degrade silently, in the spirit of the kitty graphics
protocol's capability model; the offer/announce negotiation follows
HashiCorp's go-plugin handshake, whose out-of-band transport rendezvous is
reserved here for future formats.

## Introduction

The CRAP-2 ecosystem already has the *wire format* (ndjson-crap, [ndjson-crap
v1]), the *presenter* (the viewport / `crap-present`), and several
*producers* (just-us `--events-fd` per [eng RFC 0002], the `::` converters,
the `crap.Reporter` consumers). What it lacks is the *introduction*: a
producer must be told, per invocation, that a structured consumer exists and
where to write. Three consequences:

1. **No ambient detection.** `just` run by spinclass's merge viewport emits
   human text unless the caller remembered `--events-fd`. A program cannot
   ask "does my harness speak ndjson-crap?" the way a terminal program asks
   "does my terminal render graphics?" ([kitty-graphics] solves this for
   terminals with an in-band query/response; pipes are unidirectional, so
   this protocol uses the environment — the same out-of-band channel `TERM`
   itself uses, and `CRAP=2` is deliberately `TERM`-shaped).
2. **No recursion.** When a crap-aware tool runs another crap-aware tool
   (spinclass → just → `:: go-test`; or a recipe whose body is a shell
   pipeline ending in a producer), the inner tool's structure is flattened
   into captured `output` bytes. The process tree is lost exactly where it
   is most interesting.
3. **No negotiation.** ndjson-crap is the only format, by fiat. A future
   compact binary encoding, or a consumer that only renders some record
   families, has no way to be introduced incrementally.

[go-plugin] solved the same problems for plugin processes: the host exports
a magic-cookie environment variable (detection), exports the protocol
versions it supports (negotiation), and the plugin prints a single handshake
line announcing its chosen version and transport rendezvous (announcement).
This RFC adapts that shape to streaming telemetry: the offer is the
environment, the announcement is the first line on the offered channel, and
— because the v1 transport is simply "the channel you were handed" — the
handshake line and the data share it. The go-plugin-style out-of-band
rendezvous (plugin opens a socket and names it in the handshake) is reserved
as the negotiated escape hatch for formats that need a private or
bidirectional channel (Section 8).

Scope: this RFC defines (1) the offer environment variables, (2) producer
detection and degradation rules, (3) format negotiation and the hello
record, (4) the recursive re-offer rules (passthrough and mux) including the
shared-channel write discipline, and (5) small additive amendments to
[ndjson-crap v1] (hello header fields; optional result-family lineage). A Go
helper package (`go-crap/attach`) and an `:: attach` CLI affordance are
sketched informatively. The prototype implementation lives in just-us.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119.

## Specification

### 1. Roles

- **Harness** — a process that consumes structured output: spinclass's merge
  viewport, `crap-present` at the end of a pipeline, a CI annotator, an
  agent harness. The harness *offers*.
- **Producer** — a program that can emit a supported format. A producer
  *detects, negotiates, announces, emits*.
- **Intermediary** — anything between them: shells, `sh -c`, wrappers,
  other producers. An **unaware** intermediary participates by doing
  nothing: it inherits and propagates the environment (and any offered
  descriptor), so the offer tunnels through it. An **attached** intermediary
  (a producer that itself runs children) is a harness to its children
  (Section 6).

One process may hold all three roles at once.

A child's output is exhaustively one of two things:

- **Crap** — records in a negotiated format, on the channel, from a child
  that attached.
- **Garbage** — everything else: the plain stdout/stderr of a child that
  never attached (canonically, *garbage*), plus whatever an attached child
  still writes to its captured stdio.

Every node that executes subprocesses MUST handle both: crap by
re-offering (Section 6) so it lands on the channel already structured, and
garbage by capturing it and wrapping it as `output` records under the
child's execution node. The tree is therefore complete either way — an
attached child appears as nested nodes, an unattached child as a node with
captured garbage — and a presenter never has to care which kind of child
produced what it renders.

### 2. The offer

A harness offers by exporting **one REQUIRED variable**:

- **`CRAP=<major>`** — the CRAP major version the harness accepts.
  This document is about CRAP-2, so a conforming harness exports `CRAP=2`.

That alone is a complete offer. Four OPTIONAL variables refine it:

- **`CRAP_FD=<n>`** — the decimal number of an open, writable file
  descriptor the producer inherits and writes the stream to.
  **Default: `1` (stdout).** The default is what makes the zero-setup
  pipeline work — the harness is whatever is reading the producer's
  stdout (`CRAP=2 just build | crap-present`). When `CRAP_FD` names a
  descriptor other than stdout, stdout stays human-oriented and the
  structured stream is a pure side channel, as with `--events-fd`.
- **`CRAP_ACCEPT=<tokens>`** — the formats the harness accepts, in
  preference order, as a comma-separated list of format tokens
  (Section 4). **Default: `ndjson-crap/1`** — the canonical format for
  CRAP-2.
- **`CRAP_PARENT=<id>`** — a non-negative integer: the node id (`tp`) in
  the harness's stream under which the producer's root records nest.
  Absent means the producer's roots are top-level (`parent: null`).
- **`CRAP_DEPTH=<d>`** — a non-negative integer: the `depth` the producer
  SHOULD assign its root nodes. Defaults to `0`. Advisory — consumers
  SHOULD derive depth from `parent` lineage and treat `depth` as a hint.

An offer is **well-formed** iff `CRAP` parses as an integer major version.
The presence of a well-formed offer is the detection signal — the moral
equivalent of go-plugin's magic cookie: it asserts "a CRAP-aware consumer
is on the other end of your channel", nothing more. It is not a secret and
conveys no authority (see Security Considerations).

The harness MUST tolerate, on the read side: zero structured bytes before
EOF (nothing attached — and in the stdout-default case, arbitrary plain
human output instead), lines that do not parse (decode to `Unknown`, per
the tolerant reader), and more than one hello (Section 6). When the channel
is a dedicated descriptor, EOF arrives when the last process holding the
write side exits.

### 3. Detection and degradation (producer)

A producer, at startup:

1. Reads `CRAP`. If absent, malformed, or naming a major version the
   producer does not implement, it is **unattached**: it MUST behave
   exactly as if the protocol did not exist.
2. Resolves the channel: `CRAP_FD` if set (validated — on Unix,
   `fcntl(fd, F_GETFL)` succeeds and the access mode is writable),
   otherwise stdout. A set-but-invalid `CRAP_FD` — e.g. the variables
   outlived the descriptor through some intermediary — MUST degrade
   silently to unattached, never error. (Contrast with an *explicit*
   opt-in flag like `--events-fd`, where a dead descriptor is a usage
   error; explicit flags keep their own semantics and take precedence
   over an ambient offer.)
3. Selects the **first token in `CRAP_ACCEPT` order that it supports**
   (harness preference wins; the absent-variable default is
   `ndjson-crap/1`). If it supports none, it is unattached.
4. **Announces** by writing the hello (Section 5) to the channel, then
   emits its stream in the chosen format.

Silent degradation is the kitty-derived property: capability detection
costs the harness one `setenv`, costs an unaware program nothing, and a
capable program never has to guess.

When the channel is a dedicated descriptor, an attached producer SHOULD
keep stdout/stderr human-oriented (its existing behavior when unobserved).
When the channel is defaulted stdout, the structured stream *replaces* the
producer's human stdout — human diagnostics belong on stderr — which is
precisely the existing `just --events-fd 1 | crap-present` contract made
ambient.

### 4. Format tokens and negotiation

A format token is:

```
<name>/<major>[;<param>=<value>]*
```

- `name` — lowercase `[a-z0-9-]+`. `major` — decimal major version of that
  format. The pair identifies wire compatibility: a consumer offering
  `ndjson-crap/1` MUST accept any stream valid under [ndjson-crap v1]
  including its additive evolution.
- Parameters refine the offer; unknown parameters MUST be ignored. Defined
  here:
  - `families=<f>[+<f>]*` (ndjson-crap) — which record families the
    harness renders (`result`, `execution`, `operation`). Advisory; a
    producer MAY use it to choose a richer or cheaper emission strategy.
  - `transport=<t>[+<t>]*` — transports the harness supports
    (Section 8). Default: `inline`.

Example:

```
CRAP=2
CRAP_ACCEPT=crap-pack/1,ndjson-crap/1;families=execution+operation
```

offers a (hypothetical) binary format preferentially, falling back to
ndjson-crap. Negotiation is one-shot and declarative: there is no
counter-offer round trip, because the channel is unidirectional in v1. The
producer's choice is communicated by the hello.

**Registry.** This RFC defines one format: `ndjson-crap/1`, the canonical
wire format, which IS shared-channel-capable (Section 7). The name
`crap-pack` is reserved for a future compact binary encoding of the same
record model (length-prefixed, presumably CBOR); any new format's
specification MUST state its name/major, whether it supports shared
channels, and its mapping to the CRAP record model.

### 5. The hello

The first bytes an attached producer writes to the channel MUST be a single
JSON object on one line — the **hello** — regardless of the negotiated
format. The hello is the [ndjson-crap v1] `crap` header record with additive
fields:

```json
{"type":"crap","version":2,"ndjson":1,"format":"ndjson-crap/1","producer":"just-us/0.4.2","parent":7,"sid":"9f31c2"}
```

- `type` (REQUIRED) — `"crap"`.
- `version` (REQUIRED) — the CRAP major version attached to (`2`),
  i.e. the producer's answer to the offer's `CRAP=<major>`.
- `format` (REQUIRED) — the selected format token, without parameters.
- `producer` (REQUIRED) — `<name>/<version>` of the emitting program, for
  diagnostics.
- `ndjson` (REQUIRED when `format` is `ndjson-crap/1`) — the ndjson-crap
  schema version, for byte-compatibility with the v1 header.
- `parent` (OPTIONAL) — echo of `CRAP_PARENT`, so a consumer can correlate
  a hello with its position in the tree.
- `sid` (OPTIONAL) — a random per-producer-instance id, distinguishing
  producers on a shared channel.
- `transport` (OPTIONAL) — absent or `null` means **inline**: the
  negotiated stream follows on this channel. Section 8 reserves the
  non-null form.

After the hello's terminating `\n`, the channel speaks the announced format.
For `ndjson-crap/1` that means the hello doubles as the stream's `crap`
header ("MUST be first when present" is satisfied per producer; see
Section 7 for multi-producer channels). For a future binary format the
hello is a self-describing JSON preamble before binary payload — the
HTTP-Upgrade shape, which is what lets the format set iterate without
touching detection.

The hello serves the same purpose as go-plugin's `CORE-PROTOCOL-VERSION |
APP-PROTOCOL-VERSION | NETWORK-TYPE | NETWORK-ADDR | PROTOCOL` line: the
host learns *that* the child attached, *what* it chose, and *where* the data
flows — except that "where" defaults to "right here".

A producer whose invocation emits nothing (a `--list`-style subcommand)
SHOULD defer the hello until its first record, so silent invocations stay
silent on the channel too.

### 6. Recursion: re-offer rules

An attached producer that runs children is a harness to them. The received
offer is **per-edge** — its `CRAP_PARENT` and negotiated format describe the
parent edge, not the grandchild edge — and its channel may not even be
reachable by the child under the same name (a producer that captures child
stdout has repointed the child's fd 1 away from the channel). So:

> An attached producer MUST NOT propagate its received offer verbatim to
> its children. For each child it MUST either **re-offer** (below) or
> **withdraw** (remove `CRAP` and the `CRAP_*` variables from the child's
> environment).

(An *unattached* program makes no such promise — by definition it doesn't
know the variables exist. That asymmetry is load-bearing: it is what lets
an offer tunnel through `sh -c`, login shells, and pipelines to reach a
producer several layers down.)

One withdraw is REQUIRED rather than optional: a child whose stdout the
parent consumes **as data** — command substitution, backtick evaluation,
`shell()`-style functions — MUST have the offer withdrawn by any aware
parent (attached or not). Stdout is the default channel, so a producer
attaching there would leak records into the computed value. (The same
hazard exists under *unaware* intermediaries — `x=$(just build)` in a
plain shell under an exported `CRAP=2` captures records, exactly as
`x=$(ls --color=always)` captures escape codes under a forced-color
environment. Harnesses that export the offer broadly — e.g. session-wide
rather than around one pipeline — SHOULD set an explicit non-stdout
`CRAP_FD` to remove the hazard at the source.)

Two re-offer topologies:

**6.1 Passthrough (RECOMMENDED default).** The producer hands the child the
*same* channel and scopes it into its own node tree, exporting all of:

- `CRAP` — unchanged.
- `CRAP_FD` — a descriptor that reaches the producer's channel from the
  child. Because the producer typically captures the child's stdout (so
  the child's fd 1 is a capture pipe, not the channel), the producer
  SHOULD `dup(2)` its channel to a dedicated inheritable descriptor once
  and name that number here. The descriptor MUST remain inheritable (no
  `FD_CLOEXEC`).
- `CRAP_ACCEPT` — narrowed to **exactly the negotiated format**, and only
  if that format is shared-channel-capable. (A grandchild that cannot
  speak it simply stays unattached and its plain output is captured as
  `output` records, as today.)
- `CRAP_PARENT` — the `tp` of the execution node the producer allocated
  for this child (the `node_start` it emits when it runs the command).
- `CRAP_DEPTH` — that node's depth + 1.

The grandchild's records then flow on the shared channel, parented under
the producer's node — nesting falls out of the existing
`tp`/`parent`/`depth` lineage with no muxing, no re-parsing, and no
per-edge pipes. The discipline that makes sharing safe is Section 7.

**6.2 Mux.** The producer creates a **fresh pipe per child** and re-offers
anything it wants on it (any format list, any transport set). It reads the
child's stream itself and translates: re-allocating node ids into its own
stream's namespace, rewriting `parent` linkage, transcoding formats if it
negotiated something different downstream than upstream. Mux is REQUIRED
when the producer wants to transcode, filter, or rate-limit a child; it is
the only topology available when the negotiated upstream format is not
shared-channel-capable.

A harness at the root is just the degenerate case: it "muxes" zero levels
and renders.

**6.3 The process tree on the wire.** With passthrough recursion, the tree

```
spinclass merge            (harness: CRAP=2, viewport on the read side)
└── just …                 (attached: hello, node per recipe)
    └── recipe `test`      (node_start tp=K)
        └── :: go-test …   (attached via re-offer: CRAP_PARENT=K)
            └── package P  (test record, parent K lineage)
```

is one stream whose `parent` links reproduce the process tree, and the
viewport's check marks nest accordingly. This is the protocol's reason to
exist.

### 7. Shared-channel discipline

A producer attached via an **ambient** offer cannot assume it is the
channel's only writer: a passthrough ancestor, or parallel siblings under
an unaware intermediary, may share the channel. Therefore an
ambient-attached producer:

- **MUST randomize its node-id base.** Node ids (`tp`, and the `op` ids of
  the operation family) MUST be drawn from `base + n` where `base` is a
  uniformly random integer in `[2^40, 2^48)` chosen per process, and `n`
  increments from 1. This keeps ids inside JSON's exact-integer range
  (< 2^53), makes cross-producer collision negligible without
  coordination, and never collides with an explicit-flag producer's small
  monotonic ids. ([ndjson-crap v1] requires only stream-uniqueness of
  `tp`; monotonic-from-1 is an [eng RFC 0002] detail that applies to the
  explicit `--events-fd` contract, not here.)
- **MUST write each record with a single `write(2)` of at most `PIPE_BUF`
  (4096) bytes** so concurrent records never interleave mid-line. Bulky
  payloads — in practice `output` `data` — MUST be chunked across multiple
  records (consumers already concatenate `output` data by contract).
  Producers SHOULD bound the pre-escaping chunk size such that the
  serialized line cannot exceed the limit (1024 bytes of UTF-8 text is a
  safe choice).
- **MUST scope every record to its lineage.** Root execution/operation
  records carry `parent` = `CRAP_PARENT`. Records that are global in a
  single-producer stream — `plan`, `summary`, and result-family records,
  which have no lineage fields in v1 — MUST either carry the additive
  OPTIONAL `parent` field this RFC introduces on `plan` / `test` /
  `bailout` / `summary` (lineage for result streams nested under an
  execution node), or be omitted. A v1 presenter ignores the additive
  field and renders such records flat — acceptable degradation; a
  conforming presenter nests them.
- MAY include a `sid` in its hello so consumers can attribute hellos.

Multiple hellos on one channel are therefore legal and expected under
passthrough: each attached producer announces once, before any of its own
records. Consumers MUST treat a hello as per-producer, not per-stream. (For
`ndjson-crap/1` this is compatible: extra `crap` records mid-stream decode
as headers and are ignorable.)

A producer attached via an **explicit flag** (e.g. `--events-fd`) owns its
channel by contract and is exempt from the random-base and chunking rules
on that channel — but the moment it re-offers passthrough, its *children*
are ambient and the discipline applies to them.

### 8. Transports (reserved)

v1 defines and requires exactly one transport, **`inline`**: the negotiated
stream flows on the offered channel itself, after the hello. This is the
whole prototype surface — an offer that is one environment variable, per
the design constraint that the simplest deployment must stay trivial.

The go-plugin rendezvous shape is reserved for formats or consumers that
need a private, bidirectional, or higher-throughput channel: a harness MAY
offer `transport=inline+unix`; a producer MAY then announce

```json
{"type":"crap","version":2,"format":"crap-pack/1","producer":"…","transport":{"scheme":"unix","addr":"/run/user/1000/crap-9f31c2.sock"}}
```

meaning: the producer listens at `addr`, the harness connects, and the
negotiated stream flows over the connection while the offered channel
carries only hellos. A producer MUST NOT announce a transport the offer did
not include. Defining the rendezvous lifecycle (listening, timeouts,
cleanup) is left to the RFC that first needs it; this RFC only shapes the
negotiation so that adding it later is additive. Out-of-band transports
also dissolve the shared-channel constraints of Section 7 per-connection,
which is the expected migration path if chunked-inline throughput ever
becomes the bottleneck.

### 9. Relationship to existing opt-in flags

`just --events-fd N` / `JUST_EVENTS_FD` ([eng RFC 0002]) and similar
explicit flags remain fully specified and unchanged: explicit flags take
precedence over an ambient offer, keep their error-on-invalid-fd semantics,
and their streams keep their documented shape (plan-first, monotonic
`tp`s). The attach protocol is the ambient generalization: detection
without per-invocation wiring, plus the recursive and negotiated parts that
a single flag cannot express.

### 10. Library and CLI affordances (informative)

- **`go-crap/attach`** (future work): `attach.Detect()` returning a
  negotiated `io.Writer` + metadata (format, parent, depth) or nil;
  `attach.Offer(cmd *exec.Cmd, opts)` for harnesses; integration with
  `crap.Reporter` so `NewReporter(attach.Writer(), …)` is the whole
  producer story, including automatic re-offer env for `Phase`-scoped
  children.
- **`:: attach -- <cmd…>`** (future work): the pipeline adapter. Runs a
  command under a fresh offer, splices the channel to stdout, and — when
  the child never attaches — degrades to today's `:: exec` conversion
  (node + captured output). Makes `:: attach -- just build | crap-present`
  the universal form of "give me structure if you have it".
- **`crap-present`** (future work): grow a wrapper mode that execs its
  argv with `CRAP=2` exported and renders the pipe — making
  `crap-present -- just build` self-offering, no shell export needed.

## Security Considerations

- **The offer conveys no authority and no authenticity.** Any parent can
  set `CRAP`; attaching reveals to that parent exactly the telemetry the
  protocol is designed to reveal. This is the same trust relationship as
  stdout/stderr — a parent that can run you can already read your output.
  Producers MUST NOT treat attachment as a sign of a *trusted* consumer,
  and the [eng RFC 0002] warning carries over: quiet suppresses echo, not
  capture; command text and child output reach the channel regardless of
  quiet settings, so secrets that must not leave the process must not be
  written at all.
- **Descriptor trust.** As with `--events-fd`, the producer writes to the
  channel it is handed and cannot verify the destination. Validation
  establishes only "open and writable".
- **Spoofed or hostile producers.** A harness's read side is attacker-input
  by construction (any descendant can write). Consumers MUST parse with the
  tolerant reader, MUST bound memory (per-line caps, capped tails), MUST
  NOT interpret record text as terminal control sequences, and SHOULD
  strip/escape ANSI and control bytes before display — unchanged from
  [ndjson-crap v1] and crap RFC 0001, but the ambient offer widens who can
  write, so it bears repeating.
- **Id squatting on shared channels.** A malicious sibling could
  deliberately collide node ids or claim a foreign `parent`. The
  consequence is garbled *display*, not privilege; harnesses that need
  attribution should mux (private pipe per child) instead of passthrough.

## Conformance Testing

The prototype implementation is **just-us** (the `--events-fd` producer
grows ambient attach, the hello, random tp bases, output chunking, and
passthrough re-offer; see just-us `docs/features/0002-crap-attach.md`).
Conformance lives there as unit tests plus bats once the suite grows
attach coverage (`zz-tests_bats/`, tag `crap_attach`); the table below is
the requirement map. bats/nix were unavailable in the authoring container,
so the bats lane is specified but not yet written. The prototype was
verified end to end against the canonical consumers: a nested
two-justfile run under `CRAP=2` piped into `crap-present` renders one
verdict per node, and the stream passes `:: validate`.

| Requirement | Test | Description |
| --- | --- | --- |
| §2/§3 `CRAP=2` alone attaches to stdout | unit + smoke | `CRAP=2` ⇒ active sink on fd 1, hello first |
| §3 malformed/unsupported offer degrades silently | unit | `CRAP=banana`, `CRAP=3`, invalid `CRAP_FD` ⇒ unattached, behavior unchanged |
| §3 explicit flag precedence | bats | `--events-fd` + ambient offer ⇒ RFC 0002 stream shape on the flag's fd |
| §4 first-supported-token selection | unit: `negotiate` | `crap-pack/1,ndjson-crap/1;families=…` ⇒ picks `ndjson-crap/1`; unknown-only list ⇒ unattached |
| §5 hello shape and position | unit | `crap` record with `version`/`ndjson`/`format`/`producer`/`parent` precedes all records |
| §6.1 passthrough re-offer | bats | nested `just` sees `CRAP` unchanged, `CRAP_FD` = dup'd channel, `CRAP_ACCEPT` narrowed, `CRAP_PARENT` = recipe `tp` |
| §7 random tp base | unit | ambient `tp`s ≥ 2^40; explicit-flag `tp`s remain 1..n |
| §7 single-write ≤ PIPE_BUF, output chunking | unit | large child output splits across `output` records, each serialized line ≤ 4096 bytes |
| §7 nested global-record scoping | unit | producer attached with `CRAP_PARENT` omits `plan` (or scopes it) |

## Compatibility

- **Additive to [ndjson-crap v1]** — the `ndjson` version stays `1`. The
  hello reuses the existing `crap` header record with new OPTIONAL fields
  (`format`, `producer`, `parent`, `sid`, `transport`); the OPTIONAL
  `parent` on result-family records is a new field on existing records,
  permitted by v1's forward-compatibility rules. v1 consumers render
  attached streams flat but correctly.
- **[eng RFC 0002] unchanged.** The explicit `--events-fd` contract (plan
  first, monotonic tps, error on invalid fd) is untouched; ambient attach
  is a parallel activation path with its own rules. New records appearing
  on an `--events-fd` stream via passthrough grandchildren are covered by
  RFC 0002's "consumers MUST ignore unknown record types".
- **crap RFC 0001 composes.** Operation-family records from an attached
  producer scope via the same `parent` lineage; nothing in the operation
  family changes.
- **Viewport.** No new `Model` message is required for v1 rendering
  (flat-with-lineage is already the execution family's shape). Rendering
  parent-scoped *result-family* records as nested check marks is presenter
  work tracked alongside the schema amendment.
- **The `CRAP` name.** No known tool reads a bare `CRAP` environment
  variable; the `CRAP_*` namespace is claimed by this RFC.

## References

### Normative

- [RFC 2119] — Key words for use in RFCs to Indicate Requirement Levels.
- [ndjson-crap v1] — `docs/ndjson-crap-schema.md`, the wire format this
  protocol negotiates and amends.
- [eng RFC 0002] — just-us `--events-fd` stream, the explicit-activation
  precursor whose execution family carries the tree.

### Informative

- [kitty-graphics] — the kitty graphics protocol: capability detection
  with silent degradation by unaware participants, the design stance this
  protocol borrows. <https://sw.kovidgoyal.net/kitty/graphics-protocol/>
- [go-plugin] — HashiCorp's plugin handshake: magic-cookie env detection,
  host-advertised protocol versions, one-line announce naming the chosen
  protocol and transport rendezvous.
  <https://github.com/hashicorp/go-plugin/blob/main/docs/internals.md>
- crap RFC 0001 — operation family + producer reporter API.
- just-us FDR 0002 — `docs/features/0002-crap-attach.md`, the prototype's
  design record and divergences.

[RFC 2119]: https://www.rfc-editor.org/rfc/rfc2119
[ndjson-crap v1]: ../ndjson-crap-schema.md
[eng RFC 0002]: https://github.com/amarbel-llc/just-us/blob/master/docs/rfcs/0002-just-events-fd-stream.md
[kitty-graphics]: https://sw.kovidgoyal.net/kitty/graphics-protocol/
[go-plugin]: https://github.com/hashicorp/go-plugin/blob/main/docs/internals.md
