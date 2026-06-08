// Package crap is the producer-side reporter API for ndjson-crap (crap RFC
// 0001 §10). It lets a CLI tool emit conformant ndjson-crap without
// hand-writing ndjsoncrap.Writer records, unifying the three point-kinds:
// test points (result family), operations (operation family), and execution
// nodes (execution family). The reporter allocates stream-unique tp ids,
// threads op/parent linkage, and tallies operation/result counts itself.
//
// Whether the stream is rendered live (viewport.Present) or written as wire
// bytes (a file) is the consumer's concern, not the producer's: a Reporter
// only writes records.
package crap

import (
	"io"
	"time"

	"github.com/amarbel-llc/crap/go-crap/v2/ndjsoncrap"
)

// ReporterOptions configures a Reporter. When Title or Source is set, the
// Reporter emits a Meta header as the first record.
type ReporterOptions struct {
	Title  string
	Source string
}

// Reporter is the root producer. Construct one per stream with NewReporter.
// Write errors are sticky: the first error is retained and returned by Err,
// and subsequent writes are no-ops, so call sites stay ergonomic.
type Reporter struct {
	w      *ndjsoncrap.Writer
	nextTP int
	err    error
}

// NewReporter builds a Reporter writing ndjson-crap to w.
func NewReporter(w io.Writer, opts ReporterOptions) *Reporter {
	nw := ndjsoncrap.NewWriter(w)
	r := &Reporter{w: nw, nextTP: 1}
	if opts.Title != "" || opts.Source != "" {
		r.write(ndjsoncrap.Meta{Title: opts.Title, Source: opts.Source})
	}
	return r
}

// Err returns the first write error encountered, or nil. Check it once after
// finishing the stream.
func (r *Reporter) Err() error { return r.err }

func (r *Reporter) write(rec ndjsoncrap.Record) {
	if r.err != nil {
		return
	}
	r.err = r.w.Write(rec)
}

func (r *Reporter) allocTP() int {
	tp := r.nextTP
	r.nextTP++
	return tp
}

// --- Result family: test points -------------------------------------------

// TestStream emits result-family records: a plan, then one test point per
// Ok/NotOk/Skip, then a tallied summary at Finish.
type TestStream struct {
	r       *Reporter
	plan    int
	n       int
	summary ndjsoncrap.Summary
}

// TestStream begins a result-family stream declaring plan top-level points.
func (r *Reporter) TestStream(plan int) *TestStream {
	r.write(ndjsoncrap.Plan{Count: plan})
	return &TestStream{r: r, plan: plan}
}

// Ok records a passing test point.
func (t *TestStream) Ok(desc string) {
	t.emit(ndjsoncrap.Test{Description: desc, OK: true})
	t.summary.Passed++
}

// NotOk records a failing test point with an optional diagnostic.
func (t *TestStream) NotOk(desc string, diagnostic map[string]any) {
	t.emit(ndjsoncrap.Test{Description: desc, OK: false, Diagnostic: diagnostic})
	t.summary.Failed++
}

// Skip records a skipped test point with a reason directive.
func (t *TestStream) Skip(desc, reason string) {
	t.emit(ndjsoncrap.Test{
		Description: desc, OK: true,
		Directive: &ndjsoncrap.Directive{Kind: "skip", Reason: reason},
	})
	t.summary.Skipped++
}

func (t *TestStream) emit(test ndjsoncrap.Test) {
	t.n++
	test.N = t.n
	t.r.write(test)
}

// Finish emits the result-family summary with the accumulated tallies.
func (t *TestStream) Finish() {
	s := t.summary
	s.Total = s.Passed + s.Failed + s.Skipped + s.Todo
	s.PlanCount = t.plan
	s.Valid = true
	t.r.write(s)
}

// --- Operation family ------------------------------------------------------

// OpOptions configures an operation. Total/BytesTotal of 0 mean unknown
// (indeterminate bar). Parent of 0 means top-level.
type OpOptions struct {
	Total      int
	BytesTotal int64
	Parent     int
}

// Operation emits operation-family records: an operation_start, then one item
// per Item/Skip/Fail (and/or Progress), then a tallied operation_end at
// Finish. The Operation tallies done/skipped/failed itself.
type Operation struct {
	r       *Reporter
	tp      int
	start   time.Time
	done    int
	skipped int
	failed  int
}

// Operation begins an operation named name.
func (r *Reporter) Operation(name string, opts OpOptions) *Operation {
	tp := r.allocTP()
	r.write(ndjsoncrap.OperationStart{
		TP:         tp,
		Name:       name,
		Parent:     optionalTP(opts.Parent),
		Total:      opts.Total,
		BytesTotal: opts.BytesTotal,
	})
	return &Operation{r: r, tp: tp, start: time.Now()}
}

// Item records a completed (done) item contributing bytes.
func (o *Operation) Item(label string, bytes int64) {
	o.r.write(ndjsoncrap.Item{Op: o.tp, Label: label, State: ndjsoncrap.ItemDone, Bytes: bytes})
	o.done++
}

// Skip records a skipped item with a reason (e.g. "exists").
func (o *Operation) Skip(label, reason string) {
	o.r.write(ndjsoncrap.Item{
		Op: o.tp, Label: label, State: ndjsoncrap.ItemSkipped,
		Directive: &ndjsoncrap.Directive{Kind: "skip", Reason: reason},
	})
	o.skipped++
}

// Fail records a failed item; err becomes the item's diagnostic and the item
// persists a verdict line in the viewport.
func (o *Operation) Fail(label string, err error) {
	diag := map[string]any{}
	if err != nil {
		diag["error"] = err.Error()
	}
	o.r.write(ndjsoncrap.Item{Op: o.tp, Label: label, State: ndjsoncrap.ItemFailed, Diagnostic: diag})
	o.failed++
}

// Progress advances the operation's bars without naming an item.
func (o *Operation) Progress(current int, bytes int64) {
	o.r.write(ndjsoncrap.Progress{Op: o.tp, Current: current, Bytes: bytes})
}

// Phase opens a nested execution node (child of this operation).
func (o *Operation) Phase(name string) *Phase {
	return o.r.phase(name, o.tp)
}

// Finish emits operation_end with the tallied done/skipped/failed counts and
// the elapsed duration. ok is failed == 0.
func (o *Operation) Finish() {
	o.r.write(ndjsoncrap.OperationEnd{
		Op:         o.tp,
		Done:       o.done,
		Skipped:    o.skipped,
		Failed:     o.failed,
		Total:      o.done + o.skipped + o.failed,
		OK:         o.failed == 0,
		DurationMs: uint64(time.Since(o.start).Milliseconds()),
	})
}

// --- Execution family: phases / nodes --------------------------------------

// Phase emits execution-family records for one node: command/output lines,
// then a node_end verdict at Done/Fail.
type Phase struct {
	r     *Reporter
	tp    int
	start time.Time
}

// Phase opens a top-level execution node named name.
func (r *Reporter) Phase(name string) *Phase {
	return r.phase(name, 0)
}

func (r *Reporter) phase(name string, parent int) *Phase {
	tp := r.allocTP()
	r.write(ndjsoncrap.NodeStart{
		TP:       tp,
		Name:     name,
		Namepath: name,
		Parent:   optionalTP(parent),
	})
	return &Phase{r: r, tp: tp, start: time.Now()}
}

// Command records a command about to run under this node.
func (p *Phase) Command(cmd string) {
	p.r.write(ndjsoncrap.Command{TP: p.tp, Command: cmd})
}

// Output records a chunk of this node's child output on the given stream
// ("stdout" or "stderr").
func (p *Phase) Output(stream, data string) {
	p.r.write(ndjsoncrap.Output{TP: p.tp, Stream: stream, Data: data})
}

// Done closes the node with a success verdict (exit 0).
func (p *Phase) Done() {
	code := 0
	p.r.write(ndjsoncrap.NodeEnd{TP: p.tp, ExitCode: &code, DurationMs: p.elapsed()})
}

// Fail closes the node with a failure verdict; a non-nil err is first emitted
// as a stderr output line so it surfaces in the verdict's held tail.
func (p *Phase) Fail(err error) {
	if err != nil {
		p.r.write(ndjsoncrap.Output{TP: p.tp, Stream: ndjsoncrap.StreamStderr, Data: err.Error() + "\n"})
	}
	code := 1
	p.r.write(ndjsoncrap.NodeEnd{TP: p.tp, ExitCode: &code, DurationMs: p.elapsed()})
}

func (p *Phase) elapsed() uint64 { return uint64(time.Since(p.start).Milliseconds()) }

// optionalTP returns a pointer to tp, or nil when tp is 0 (top-level / none).
func optionalTP(tp int) *int {
	if tp == 0 {
		return nil
	}
	return &tp
}
