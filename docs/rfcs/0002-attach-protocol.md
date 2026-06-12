---
status: draft
date: 2026-06-11
---

<!-- crap RFC 0002 (crap-local RFC series). Second draft, same day:
     supersedes the fd-passthrough first draft after design review (see
     Design History). The first draft's just-us prototype validated the
     semantics (lineage nesting, garbage capture, degradation, viewport
     compatibility) and is superseded on transport; rework tracked in
     Conformance Testing. Home: docs/rfcs/0002-attach-protocol.md.
     Note: "crap RFC 000N" = this repo's series; "eng RFC 000N" = the
     eng-wide protocol series (0002 = just-us --events-fd). -->

# CRAP Attach Protocol: Recursive Structured-Output Trees over a Birthed Sink Server

## Abstract

A program that can emit `ndjson-crap` has no way to learn whether the thing
running it wants that — so today every producer needs an explicit opt-in
flag (`just --events-fd 3`, `:: go-test`), and a tree of programs cannot
compose into one nested stream without each layer being hand-wired. This
RFC specifies the **CRAP attach protocol** around two invariants:

- **Composable** — any aware node drops under any other (including through
  unaware shells, wrappers, and parallel siblings) and the combined run
  yields one coherent tree.
- **Severable** — cut the tree anywhere; the severed subtree, run on its
  own, still produces a complete, viewport-renderable crap stream.

The model is a recursive function with a strict boundary. A harness offers
with one environment variable, `CRAP=2`. Every attached node is only ever a
**client**: it connects to the **sink** — a unix-domain-socket server — and
writes its records there, rooted under the `CRAP_PARENT` node id it
inherited. A node with no sink above it is a **root**: it *births* the
server (forking and becoming it, re-exec'ing its own binary into it, or
exec'ing a shipped one), which owns the socket, grants each connection a
disjoint deterministic id base, splices record lines to one merged output
in arrival order, and cleans itself up when its birth lease drops.
Attachment is scoped to the process tree: the server admits only peers
descended from its spawner, and everything outside — leaked offers,
detached daemons — is denied and reverts to dumb output. Roots
presenting to a terminal share one tty-keyed server, elected by atomic
bind, so each terminal has zero or one server — never more. Children
that never attach emit **garbage** — plain stdio — which their parent
captures and wraps as `output` records, so the tree is complete either way.
No randomness, no shared-descriptor write discipline, no per-hop pumping
chain: connections carry identity, and the server is small enough to embed
in every node's client library.

## Introduction

The CRAP-2 ecosystem has the *wire format* (ndjson-crap, [ndjson-crap v1]),
the *presenter* (the viewport / `crap-present`), and several *producers*
(just-us `--events-fd` per [eng RFC 0002], the `::` converters, the
`crap.Reporter` consumers). What it lacks is the *introduction*: a producer
must be told, per invocation, that a structured consumer exists and where
to write. Consequently there is no ambient detection (a program cannot ask
"does my harness speak ndjson-crap?" the way a terminal program asks "does
my terminal render graphics?" — [kitty-graphics] answers that for
terminals; this protocol uses the environment, the same out-of-band channel
`TERM` itself uses), no recursion (a crap-aware tool running another
crap-aware tool flattens the inner tool's structure into captured bytes),
and no evolution path for the wire format.

This RFC's stance on degradation is kitty's: unaware programs ignore the
environment and behave normally, and the offer tunnels through them
untouched, so capability detection costs the harness one `setenv` and
costs everyone else nothing.

### Design history (informative)

Three models were worked through; the residue of each survives here.

1. **Shared-fd passthrough** (first draft of this RFC; prototyped in
   just-us). The harness offered a writable descriptor; producers attached
   and re-offered the same descriptor to children. Nesting worked and was
   validated end to end, but the *anonymous* shared channel forced three
   producer burdens: random id bases (uncoordinated global uniqueness),
   single-`write(2)` records ≤ `PIPE_BUF`, and output chunking. A
   go-plugin-style out-of-band transport rendezvous was reserved for
   later.
2. **Pure recursive mux** (stdout only; every node merges its children's
   stdout). Strict boundaries and natural severability, but two flaws:
   liveness depends on every intermediary pumping promptly, and unaware
   intermediaries reintroduce the multi-writer channel anyway (two aware
   tools run in parallel by an unaware script share its inherited stdout).
3. **Client/birthed-server** (this draft). The sink is a socket server
   spawned by the root. Connections give writers identity, which dissolves
   the randomness and the write discipline; clients write point-to-point,
   which dissolves the pumping chain; root-election gives severability.
   The go-plugin rendezvous machinery is dropped — once the sink is a
   server, the only thing the environment must carry is its address.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119.

## Specification

### 1. Model and roles

Every node in a process tree is treated as a recursive function with a
strict API boundary:

```
f(parent_id, sink) → ndjson-crap records, written to sink
```

- **Input** (via environment): the sink address and the node id under
  which this node's root records nest.
- **Output**: records on the node's own connection to the sink. A node's
  descendants are recursive calls; their records flow to the *same* sink
  on their *own* connections — never through the node.

Roles:

- **Client** — every attached node, without exception. Connects, receives
  a grant (Section 4), emits records.
- **Server (the sink)** — never a long-lived service, never a node
  "becoming" one: it is *birthed* by a root (Section 6), owns one
  unix-domain socket for one tree, and dies with it.
- **Root** — a node holding a well-formed offer but no sink address. It
  births the server, connects as an ordinary client, and exports the sink
  address to its children.
- **Unaware intermediary** — any program in between that has never heard
  of this protocol. It participates by doing nothing: the environment
  tunnels through it.

A child's output is exhaustively one of two things:

- **Crap** — records on its own connection to the sink, from a child that
  attached.
- **Garbage** — everything else: the plain stdout/stderr of a child that
  never attached (canonically, *garbage*), plus whatever an attached child
  still writes to its stdio.

Every node that executes subprocesses MUST handle both: crap by scoping
(Section 5 — set the child's `CRAP_PARENT` so its records nest), and
garbage by capturing it and wrapping it as `output` records under the
child's execution node, sent over the parent's own connection. The tree is
complete either way, and a presenter never has to care which kind of child
produced what it renders.

### 2. The offer

A harness offers by exporting **one variable**:

- **`CRAP=<major>`** — the CRAP major version the harness accepts. This
  document is about CRAP-2, so a conforming harness exports `CRAP=2`.
  This is the entire user-facing surface; the variables below are
  machinery managed by attached nodes and the client library.

Two further variables are written by the protocol itself:

- **`CRAP_SINK=<path>`** — absolute filesystem path of the sink's
  unix-domain socket. Exported by the root after birthing; inherited by
  everything below it.
- **`CRAP_PARENT=<id>`** — non-negative integer: the node id under which
  the receiving node's root records nest. Set per-child by attached
  parents. Absent means the node's roots are top-level (`parent: null`).

An offer is **well-formed** iff `CRAP` parses as an integer major version.
Its presence is the detection signal; it is not a secret and conveys no
authority (see Security Considerations). Version negotiation is the
integer itself plus the per-connection grant (Section 4): an incompatible
future revision is a different major.

### 3. Attachment (the `attach()` contract)

The crap framework ships this procedure as a native library function
(go-crap and rust-crap) and, for other languages, as a C-ABI export or a
spec-conformant reimplementation — the client side is deliberately small
enough that the wire description below is a complete implementation guide.
A node, at startup:

1. Reads `CRAP`. Absent, malformed, or an unsupported major → the node is
   **unattached**: it MUST behave exactly as if the protocol did not
   exist, and MUST NOT modify any `CRAP*` variable for its children.
2. If `CRAP_SINK` is set: connect to it and perform the grant exchange
   (Section 4). Failures divide by what they evidence:
   - **No server there** — missing socket, connection refused: the node
     MUST proceed as if `CRAP_SINK` were absent (step 3). A dead
     ancestor sink causes the subtree to re-root rather than go dark:
     severability doubles as the failure mode.
   - **A server said no** — a deny grant, EOF before any grant, a
     malformed grant, or an unsupported granted format: the node MUST
     degrade to **unattached** (dumb output). A live server's refusal
     means this node is not part of that tree (Section 4, scoped
     attachment); it must not respond by birthing a surprise server of
     its own.
3. If `CRAP_SINK` is not set (or step 2 failed with "no server there"):
   the node is root-capable. Where it lands depends on where its output
   goes:
   - **Presenting to a terminal** (stdout is a tty): the node MUST use
     the terminal's shared server (Section 6.3) — connecting to the
     tty-keyed socket if one is live, electing itself by atomic bind if
     not — and export the resulting `CRAP_SINK` to its children. Two
     root-capable nodes racing rooflessly on one terminal thus converge
     on one server, as sibling top-level trees.
   - **Otherwise** (stdout is a pipe, file, or other destination): the
     node is a root in the Section 6 sense. It MUST birth a private tree
     server, connect to it as a client, and export the new `CRAP_SINK`
     to its children.

   A node that can do neither (resource failure, lost election *and*
   failed connect) degrades to unattached.
4. After a successful grant, the node emits records (Section 5). A write
   failure after attachment MUST NOT abort the node's real work; it drops
   records from that point (mirroring [eng RFC 0002]'s write-failure
   policy).

Explicit opt-in flags (e.g. `just --events-fd`) take precedence over an
ambient offer and keep their own documented semantics, including
error-on-invalid-descriptor.

Silent degradation at every step is the kitty-derived property: a capable
node never has to guess, an incapable or unlucky one never breaks the run.

### 4. The connection: grant, then records

Connections are point-to-point and framed by the transport, which is what
makes writer identity free. The protocol on each connection:

**Scoped attachment.** Attachment is scoped to the server's process
tree: a node MUST NOT attach unless the server's spawner is among its
ancestors at attach time. The offer travels only by parent→child
inheritance — nodes MUST NOT persist `CRAP_SINK` to files, session
managers, or anything else that outlives the run — and a process that
has detached from the tree (daemonized, reparented out) has no license
to attach. Outside the tree there are exactly two legitimate behaviors:
dumb output (garbage, captured and wrapped by whatever runs you) or
root-electing a tree of your own. Servers enforce this at accept
(Section 6.1) and answer trespassers with a deny grant.

**Grant (server → client, exactly one line, immediately on accept):**

```json
{"type":"crap","version":2,"base":0,"format":"ndjson-crap/1"}
```

or, when the peer fails the scope check (or any other admission rule):

```json
{"type":"crap","version":2,"deny":"scope"}
```

A denied client MUST close the connection and degrade to unattached
(Section 3). Denial is distinct from absence by construction: a missing
or refused socket means "no tree here — re-root", an answered denial
means "a tree is here and you are not in it — dumb output".

- `version` (REQUIRED) — the CRAP major the server speaks.
- `base` (REQUIRED) — the id base granted to this connection. The client
  MUST derive every node id it emits (`tp`, operation `op`) as
  `base + n`, `n` counting from 1, `n < 2^32`.
- `format` (REQUIRED) — the format token the server accepts on this
  connection. This document defines `ndjson-crap/1`. A client that cannot
  produce the granted format MUST disconnect and degrade per Section 3
  step 2.

The server MUST grant bases whose intervals `[base, base + 2^32)` are
disjoint across the socket's lifetime and lie below 2^53 (JSON-exact
integers). RECOMMENDED: `base = k·2^32` for the k-th accepted connection,
counting from 0 — the first connection (normally the root itself) then
emits plain `1, 2, 3, …`, so a single-producer stream is byte-identical
to today's, and ids are deterministic given accept order.

**Records (client → server, the rest of the connection):** ndjson-crap,
one record per line, flushed per record. The client's root nodes carry
`parent` = its inherited `CRAP_PARENT` (or `null`). A client MAY send an
[ndjson-crap v1] `crap` header record first (e.g. carrying a `producer`
string for diagnostics); it needs no special handling — it is a record
like any other.

The grant subsumes both the first draft's *hello* (the connection itself
is the announcement; the grant is the acknowledgment) and its
content-negotiation surface (`CRAP_ACCEPT` token lists): a future format
arrives by servers granting it, clients understanding it, and `CRAP`
majoring on incompatibility.

### 5. Node duties (client side)

An attached node:

- **Connects before it spawns.** A node MUST NOT start any child that
  inherits the offer until its own attachment has completed — connection
  established, grant received (and, for a root, `CRAP_SINK` exported).
  This single rule carries three guarantees: a root's children always see
  a live sink; a parent can flush its `node_start` before the child
  exists, so parent-before-child holds at the server by arrival order
  with no reordering logic; and the server's accept order follows spawn
  order wherever execution is serial, so granted bases are
  **deterministic for serial trees** — the common case — not just for
  single producers.
- **Scopes its children.** For each child it executes whose output should
  nest, it MUST set `CRAP_PARENT` to the id of the execution node it
  allocated for that child (the `node_start` it emitted when launching
  it). `CRAP` and `CRAP_SINK` are left as inherited. This is the entire
  re-offer — no descriptors, no narrowing.
- **Captures garbage.** Per Section 1, an unattached child's stdio is
  wrapped as `output` records under that child's node, on the parent's
  own connection. Producers SHOULD keep any single record line under
  64 KiB (chunking `output` data; consumers concatenate by contract) as
  buffer hygiene — this is a hygiene bound, not a `PIPE_BUF` atomicity
  requirement; the server's framing makes interleaving impossible.
- **Withholds the offer from data-consuming children** (SHOULD): children
  whose stdout the node consumes *as a value* — command substitution,
  backtick evaluation, `shell()`-style functions — should run with the
  `CRAP*` variables removed. With a socket sink the old leak (records in
  the captured value) cannot happen; this rule is now tree hygiene:
  evaluation subprocesses are not execution and would clutter the tree
  with mis-parented nodes. (Demoted from the first draft's MUST
  accordingly.)
- **Keeps stdio human.** Records go to the sink; stdout/stderr keep their
  unobserved behavior. (Producers like just-us that capture child stdio
  while attached continue to do so — that is the garbage duty, not a
  contradiction.)

Ordering needs no further rules: connect-before-spawn means a node
flushes its `node_start` before the child exists, and the child cannot
connect before it is spawned, so parent-before-child reaches the server
in arrival order; everything else is demux-by-id, which [ndjson-crap v1]
consumers already perform.

Note that parent-assigned monotonic child identity is already present in
the design rather than needing a new mechanism: the `CRAP_PARENT` value a
node hands each child invocation *is* a deterministic monotonic
identifier — `base + n` with `n` advancing in spawn order within the
parent's granted base. Every record's parent chain is therefore fully
deterministic; the granted base values are the only labels that can vary,
and only where execution is genuinely parallel (Section 7).

### 6. The server: birthed, small, self-cleaning

#### 6.1 What it does

The server is deliberately *not* a structurer. It MUST:

1. Own one listening unix-domain socket (bound by itself, or received
   pre-bound from its spawner — see 6.2).
2. On each accept, check scope, then grant. The server SHOULD verify via
   peer credentials (`SO_PEERCRED` on Linux, `LOCAL_PEERPID` on macOS,
   platform equivalents elsewhere) that the connecting process descends
   from the server's spawner — walking the peer's parent chain to the
   spawner's pid — and answer a deny grant (Section 4) on failure.
   Checking at *accept time* gives the right temporal semantics for
   free: a process must be in the tree when it joins (a daemonized
   stray that connects after detaching is refused), while a legitimate
   node whose intermediate parent later exits keeps the connection it
   was already granted. The check is admission hygiene, not a security
   boundary (Security Considerations); platforms without peer
   credentials MAY grant unchecked. Admitted connections get the grant
   with a fresh disjoint base.
3. Splice complete lines from all connections to its single merged output
   in arrival order — atomically per line, which is trivial since it is
   the only writer. It MUST NOT reorder, MUST NOT rewrite, and need not
   parse record contents. It MUST tolerate lines that are not valid
   records (forwarding or dropping them; the downstream reader is
   tolerant) and MUST bound per-connection line buffers (it MUST support
   lines to at least 64 KiB and MAY drop longer ones with a diagnostic
   record).
4. Exit by **lease** (tree servers): the spawner holds the write end of
   a pipe the server was born with. On lease EOF the server stops
   accepting, drains open connections, flushes, unlinks its socket, and
   exits. The kernel closes descriptors on any process death, so cleanup
   survives even `SIGKILL` of the spawner. The server owns its socket
   file: it unlinks it on every exit path. Draining SHOULD be bounded by
   a grace timeout so a stray connection held by a process that outlived
   the tree delays exit, not prevents it. (Terminal servers substitute a
   reference-counted lifetime — Section 6.3.)

The merged output goes wherever the spawner pointed the server's stdout at
birth: a pipe (`CRAP=2 just build | crap-present` — the pipe carries the
single-writer merged stream), a file (`CRAP=2 just build > log.ndjson`
records the run), or a presenter (the server MAY itself be the viewport,
e.g. `crap-present --serve`).

This minimalism is what makes the server embeddable: a base counter plus a
line-splicing multiplexer over `poll`/`accept`/`read`/`write` is a few
hundred lines, implementable with preallocated buffers and no runtime
services — small enough to ship inside every node's client library rather
than as infrastructure.

#### 6.2 How it is born

The spawning root SHOULD bind the socket and create the lease pipe
*before* birthing, passing both as inherited descriptors — then there is
no readiness race in any embodiment, and the root can spawn children
immediately. Three conformant embodiments:

- **fork-and-become** — `fork()`; the child calls the embedded serve
  function on the inherited descriptors and `_exit`s when it returns.
  Available to single-threaded clients, or to any client whose serve core
  is fork-safe: after `fork()` in a threaded process only the calling
  thread survives, so the serve core MUST NOT take locks or allocate from
  a possibly-locked allocator post-fork — the RECOMMENDED implementation
  style (static arenas, raw syscalls) satisfies this by construction.
- **re-exec self** — spawn the node's own executable (`/proc/self/exe` on
  Linux, platform equivalents elsewhere) with a marker variable; the
  client library's init hook recognizes the marker and routes into the
  embedded serve function before application logic runs. REQUIRED style
  for runtimes where fork-without-exec is unsupported (Go). No external
  binary, clean address space.
- **external binary** — exec a shipped server (`crap-present --serve`).
  OPTIONAL convenience; never a hard dependency of the protocol.

All three implement the same server contract (grant, splice, lease), so
mixed trees interop. Sockets MUST be filesystem sockets (not Linux
abstract-namespace ones) in a directory with `0700` permissions —
`$XDG_RUNTIME_DIR/crap/` RECOMMENDED, `$TMPDIR` fallback — so connection
rights follow file permissions.

#### 6.3 Terminal servers and leader election

**Property: each terminal has zero or one server — never more.** The
terminal is a shared presentation surface; two servers merging two
streams onto one tty is the multi-writer collision reappearing at the
top of the stack, and is forbidden.

The property is enforced by making the terminal itself the rendezvous.
A terminal server's socket path is **derived deterministically from the
tty** — RECOMMENDED `<runtime dir>/crap/tty-<major>.<minor>.sock` from
the controlling terminal's device number — and election is the atomic
bind:

1. A root-capable node presenting to a tty first tries to **connect** to
   the tty-keyed path. Success + grant ⇒ a server already exists; the
   node is a client like any other.
2. Otherwise it tries to **bind** the path. Success ⇒ it won the
   election: it births the terminal server on the pre-bound descriptor.
   `EADDRINUSE` ⇒ it lost a race it didn't see; go to 1 (bounded
   retries).
3. A path that refuses connections *and* refuses binds is stale debris
   from a crashed server: claim it under a sidecar lock file (`flock`),
   re-verify deadness, unlink, and bind. Implementations MUST ensure
   mutual exclusion here; the invariant is worth a lock file.

No messages are exchanged to elect — the kernel's bind atomicity *is*
the election. Losers become clients; their roots (no `CRAP_PARENT`)
render as sibling top-level trees, which is the correct presentation for
parallel commands sharing a terminal.

Terminal servers differ from tree servers in three ways:

- **Admission**: the scope check is the terminal, not descent — admit
  peers whose controlling terminal is the server's tty (peer pid via
  credentials, then the platform's ctty lookup), deny others. The tty is
  the tree's roof.
- **Lifetime**: reference-counted, not leased — the server exits (and
  unlinks) when its last client disconnects, after a short grace period
  for back-to-back commands. It serves the terminal, not one spawner.
- **Output**: its merged stream lands on a human's tty, so it SHOULD
  present rather than splice raw records — the viewport when available
  (`crap-present` as the output stage or the server embodiment itself),
  a plain verdict-per-line renderer as fallback. Raw splice to a tty is
  permitted only as a last resort. This is the one server flavor where
  the embedded-tiny embodiment is *not* the natural fit; terminal
  presentation legitimately wants the shipped presenter, which the
  ecosystem provides anyway.

Pipes and files need no election: distinct destinations are distinct by
construction, and one destination is written by one tree server. (A
future revision MAY generalize the rendezvous key from "tty device" to
"output destination identity" — e.g. `st_dev:st_ino` of a shared pipe —
if parallel roofless roots writing one non-tty destination prove to
matter; for now that case is documented residue, and redirecting to
distinct files is the workaround.)

### 7. The invariants, demonstrated

**Composable.** The tree

```
spinclass merge            (harness: exports CRAP=2, reads merged stream)
└── just …                 (root: births server, exports CRAP_SINK;
    │                       client: nodes per recipe)
    └── recipe `test`      (node_start tp=K; just sets CRAP_PARENT=K
        │                   for the child)
        └── sh -c …        (unaware: env tunnels through)
            └── :: go-test (client: connects itself, granted base
                            2^32·k, roots carry parent:K)
```

is one merged stream whose `parent` links reproduce the process tree;
check marks nest accordingly. With connect-before-spawn (Section 5),
accept order follows spawn order through any serially-executing chain, so
the whole tree's ids are deterministic run to run wherever execution is
serial. The residue is confined to genuine parallelism under an *unaware*
intermediary: two aware tools launched in parallel by an unaware script
inherit identical context — neither can deterministically pick a distinct
index, and no handed-down path or parent-assigned counter can fix that (a
coordination-free pigeonhole; the parent's monotonic identifiers stop at
the invocation it actually made, and both racers live inside one
invocation). Each still gets its own connection, hence its own granted
base: uniqueness comes from connection identity, lineage from the shared
`CRAP_PARENT`, and the only nondeterminism left is which racer gets which
base — a label, not a structure, and exactly as racy as the siblings'
real execution order already was.

**Severable.** Severing is **root-election**: run any subtree without an
inherited `CRAP_SINK` — a debugging shell, a CI job, a cron entry — and it
births its own server, yielding a complete stream that any crap-compliant
presenter renders. A node whose ancestor sink died re-roots automatically
(Section 3 step 2). The deliberate trade: a *running* tree's interior
edges carry nothing (records flow point-to-point), so severability is
"cut and re-run/re-root", not "wiretap a live edge".

**Scoped.** The two invariants are bounded by a third property: the
unit of attachment is the process tree, roofed at worst by the terminal.
A tree server admits descendants of its spawner; a terminal server
admits holders of its tty (Section 6.3); everything outside — leaked
environments in other terminals, daemons that detached mid-run,
processes sourcing a stale env snapshot — is denied and reverts to dumb
output, or root-elects a tree of its own. Composition never crosses
scope boundaries by accident, and severed pieces cannot half-rejoin a
tree they no longer belong to.

**One server per terminal.** Roofless parallelism is the residual
collision case — two root-capable nodes under an unaware launcher would
otherwise birth two servers granting overlapping bases onto one shared
destination. For terminals, Section 6.3 closes it: the tty-keyed
rendezvous and bind-atomicity election guarantee zero or one server per
terminal, with losers joining as clients and rendering as sibling
top-level trees in one viewport.

### 8. Relationship to existing opt-in flags

`just --events-fd N` / `JUST_EVENTS_FD` ([eng RFC 0002]) is unchanged:
plan-first, monotonic tps, error on invalid descriptor, stream shape
byte-identical. The attach protocol is the ambient generalization. A
producer supporting both MUST prefer the explicit flag when given.

## Security Considerations

- **The offer conveys no authority.** Any parent can export `CRAP=2`;
  attaching reveals exactly the telemetry the protocol is designed to
  reveal, to whoever can read the sink's output — the same trust
  relationship as stdout. The [eng RFC 0002] warning carries over: quiet
  suppresses echo, not capture; secrets that must not leave the process
  must not be written at all.
- **Socket trust.** Anyone who can connect can inject records; anyone who
  can read the socket path's directory can find it. The `0700`-directory
  requirement (Section 6.2) scopes both to the same user, which is the
  same boundary the user's processes already share. Servers MUST NOT
  grant to, and SHOULD NOT accept from, peers they cannot restrict by
  filesystem permission; abstract-namespace sockets are excluded for
  exactly this reason. The scope check (Sections 4, 6.1) tightens this
  from "same user" to "same tree" against *accidents* — leaked offers,
  stale env snapshots, detached daemons — but it is hygiene, not a
  security boundary: peer-pid ancestry walks are subject to pid-reuse
  races and parent-chain mutation, and a same-user attacker can defeat
  them (as they can defeat anything, via ptrace). Display integrity
  against same-user accidents is the claim; isolation against same-user
  malice is not.
- **Hostile or buggy clients.** The server's inputs are attacker-grade by
  construction. It MUST bound per-connection buffers and connection
  counts; the splice design means a malicious client can at worst pollute
  the stream with records, which downstream tolerant readers and the
  display-safety rules of [ndjson-crap v1] (no terminal control
  interpretation, ANSI stripping) already contain. Id squatting (emitting
  ids outside the granted base) garbles display, not privilege; a server
  MAY police ranges but is not required to parse.
- **No orphan daemons.** The lease (Section 6.1) ties every server's
  lifetime to its spawner's; there is no registry, no pidfile, and
  nothing to leak but a socket file the server unlinks on exit.

## Conformance Testing

The prototype lives in **just-us**. The first-draft prototype (pushed on
this branch) validated the protocol *semantics* end to end over the
superseded fd transport: ambient `CRAP=2` detection with silent
degradation, hello-then-records, lineage nesting across a two-justfile
tree, garbage capture, backtick withdrawal, explicit-flag precedence, and
acceptance by the canonical consumers (`crap-present` rendering,
`:: validate` 16/16 records). Reworking it to this draft — `attach()`
connect-or-birth, the granted-base client, and an embedded re-exec/fork
server (plus `crap-present --serve` and the `go-crap`/`rust-crap` attach
libraries on the crap side) — is the tracked next step; the requirement
map below is written against this draft.

| Requirement | Test | Description |
| --- | --- | --- |
| §3 `CRAP=2` alone roots and serves | bats | `CRAP=2 just build > f` yields one merged stream; first connection's ids are `1..n` |
| §3 degradation | unit | `CRAP` absent / `CRAP=3` / birth failure ⇒ behavior unchanged |
| §3 dead sink re-roots | bats | stale `CRAP_SINK` (socket gone) ⇒ subtree births its own server, stream complete |
| §3/§4 denial means dumb output | unit | deny grant / EOF-before-grant ⇒ unattached, no surprise server |
| §4 grant shape and base discipline | unit | grant precedes records; bases disjoint; client ids = base+n |
| §6.1 scope check at accept | unit | non-descendant peer gets `deny:"scope"`; in-tree peer whose parent later exits keeps its connection |
| §6.3 terminal singleton | bats | two roofless root-capable nodes on one pty ⇒ exactly one server, both render as sibling top-level trees |
| §6.3 cross-terminal denial | unit | a peer on another tty gets `deny:"scope"` from a terminal server |
| §6.3 stale-path claim | unit | crashed terminal server's socket is reclaimed under the lock; never two binders |
| §5 connect-before-spawn | unit | no child env export / spawn before the grant is read; serial runs grant identical bases run-to-run |
| §5 child scoping | bats | nested `just` records carry `parent` = outer recipe id via its own connection |
| §5 garbage capture | bats | unattached child stdio arrives as `output` records under its node |
| §6.1 splice atomicity and arrival order | unit | concurrent clients' lines never interleave mid-line; parent `node_start` precedes child records |
| §6.1 lease cleanup | unit | spawner exit (incl. SIGKILL) ⇒ server drains, unlinks socket, exits |
| §7 unaware-parallel composition | bats | two aware tools under an unaware script get distinct bases, same parent |

## Compatibility

- **Additive to [ndjson-crap v1]**; the `ndjson` version stays `1`. The
  grant line exists only on the server→client leg of connections — it
  never appears in the merged stream, so consumers need nothing new. Two
  additive amendments stand: the OPTIONAL `parent` field on result-family
  records (`plan`/`test`/`bailout`/`summary`) so attached result-stream
  producers can nest under an execution node, and the observation that
  [ndjson-crap v1] requires only stream-uniqueness of ids (granted bases
  satisfy it; monotonic-from-1 is an [eng RFC 0002] detail of the
  explicit flag).
- **[eng RFC 0002] unchanged** (Section 8). Records from other clients
  appearing alongside a producer's own in the merged stream are covered
  by its consumers-ignore-unknown rule and by demux-by-id.
- **First draft of this RFC**: superseded. `CRAP_FD`, `CRAP_ACCEPT`,
  `CRAP_DEPTH`, the hello header fields, the `sid`, the format-token
  grammar, the transport rendezvous, and the shared-channel discipline
  (random bases, `PIPE_BUF` single-writes, mandatory chunking) are all
  withdrawn; `CRAP` and `CRAP_PARENT` retain their meaning, and
  `CRAP_SINK` replaces `CRAP_FD`. The just-us fd prototype implements the
  withdrawn transport and is marked superseded in its FDR.
- **Viewport.** No new `Model` message; flat-with-lineage already renders
  (validated). Nested *indentation* for parent-scoped records remains
  presenter work tracked alongside the result-family amendment.
- **The `CRAP` name.** No known tool reads a bare `CRAP` environment
  variable; the `CRAP*` namespace is claimed by this RFC.

## References

### Normative

- [RFC 2119] — Key words for use in RFCs to Indicate Requirement Levels.
- [ndjson-crap v1] — `docs/ndjson-crap-schema.md`, the wire format whose
  records flow over this protocol.
- [eng RFC 0002] — just-us `--events-fd` stream, the explicit-activation
  precursor whose execution family carries the tree.

### Informative

- [kitty-graphics] — capability detection with silent degradation by
  unaware participants, the design stance this protocol borrows.
  <https://sw.kovidgoyal.net/kitty/graphics-protocol/>
- [go-plugin] — HashiCorp's plugin handshake; its host-advertised
  versions and transport rendezvous informed the first draft and were
  dissolved by the birthed-server model (Design History).
  <https://github.com/hashicorp/go-plugin/blob/main/docs/internals.md>
- crap RFC 0001 — operation family + producer reporter API; operation
  ids participate in granted bases like `tp`s.
- just-us FDR 0002 — `docs/features/0002-crap-attach.md`, the
  first-draft prototype's design record.

[RFC 2119]: https://www.rfc-editor.org/rfc/rfc2119
[ndjson-crap v1]: ../ndjson-crap-schema.md
[eng RFC 0002]: https://github.com/amarbel-llc/just-us/blob/master/docs/rfcs/0002-just-events-fd-stream.md
[kitty-graphics]: https://sw.kovidgoyal.net/kitty/graphics-protocol/
[go-plugin]: https://github.com/hashicorp/go-plugin/blob/main/docs/internals.md
