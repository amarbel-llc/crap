package crap

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"code.linenisgreat.com/crap/go-crap/v2/ndjsoncrap"
)

// collect decodes every record the reporter wrote, asserting no write error.
func collect(t *testing.T, r *Reporter, buf *bytes.Buffer) []ndjsoncrap.Record {
	t.Helper()
	if err := r.Err(); err != nil {
		t.Fatalf("reporter write error: %v", err)
	}
	rd := ndjsoncrap.NewReader(buf)
	var recs []ndjsoncrap.Record
	for {
		rec, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode reporter stream: %v", err)
		}
		recs = append(recs, rec)
	}
	return recs
}

// The Operation API must emit conformant operation-family records (RFC 0001
// §3-6) that round-trip back through the Reader, with the Operation tallying
// done/skipped/failed itself.
func TestReporterOperationRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, ReporterOptions{Title: "sync", Source: "madder"})
	op := r.Operation("sync", OpOptions{Total: 3, BytesTotal: 1000})
	op.Item("blob-a", 100)
	op.Skip("blob-b", "exists")
	op.Fail("blob-c", errors.New("write failed"))
	op.Finish()

	recs := collect(t, r, &buf)
	if len(recs) != 6 {
		t.Fatalf("record count: got %d want 6 (meta, op_start, 3 items, op_end)", len(recs))
	}
	if m, ok := recs[0].(ndjsoncrap.Meta); !ok || m.Title != "sync" || m.Source != "madder" {
		t.Fatalf("meta header: %#v", recs[0])
	}
	os, ok := recs[1].(ndjsoncrap.OperationStart)
	if !ok || os.Name != "sync" || os.Total != 3 || os.BytesTotal != 1000 || os.TP != 1 {
		t.Fatalf("operation_start: %#v", recs[1])
	}
	if it, ok := recs[2].(ndjsoncrap.Item); !ok || it.Op != 1 || it.State != ndjsoncrap.ItemDone || it.Bytes != 100 {
		t.Fatalf("item done: %#v", recs[2])
	}
	if it, ok := recs[3].(ndjsoncrap.Item); !ok || it.State != ndjsoncrap.ItemSkipped || it.Directive == nil || it.Directive.Reason != "exists" {
		t.Fatalf("item skipped: %#v", recs[3])
	}
	if it, ok := recs[4].(ndjsoncrap.Item); !ok || it.State != ndjsoncrap.ItemFailed || it.Diagnostic["error"] != "write failed" {
		t.Fatalf("item failed: %#v", recs[4])
	}
	oe, ok := recs[5].(ndjsoncrap.OperationEnd)
	if !ok || oe.Op != 1 || oe.Done != 1 || oe.Skipped != 1 || oe.Failed != 1 || oe.Total != 3 || oe.OK {
		t.Fatalf("operation_end tallies: %#v", recs[5])
	}
}

// The TestStream API must emit a result-family stream with a tallied summary.
func TestReporterTestStreamRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, ReporterOptions{})
	ts := r.TestStream(2)
	ts.Ok("agent socket reachable")
	ts.NotOk("key loaded", map[string]any{"message": "no key"})
	ts.Finish()

	recs := collect(t, r, &buf)
	// No Meta (no Title/Source): plan, two tests, summary.
	if len(recs) != 4 {
		t.Fatalf("record count: got %d want 4", len(recs))
	}
	if p, ok := recs[0].(ndjsoncrap.Plan); !ok || p.Count != 2 {
		t.Fatalf("plan: %#v", recs[0])
	}
	if tr, ok := recs[1].(ndjsoncrap.Test); !ok || tr.N != 1 || !tr.OK {
		t.Fatalf("test 1: %#v", recs[1])
	}
	if tr, ok := recs[2].(ndjsoncrap.Test); !ok || tr.N != 2 || tr.OK || tr.Diagnostic["message"] != "no key" {
		t.Fatalf("test 2: %#v", recs[2])
	}
	s, ok := recs[3].(ndjsoncrap.Summary)
	if !ok || s.Passed != 1 || s.Failed != 1 || s.Total != 2 || s.PlanCount != 2 {
		t.Fatalf("summary: %#v", recs[3])
	}
}

// FailDiag must close the node with a nonzero node_end carrying the producer
// diagnostic (crap#22), with a non-nil err still surfacing as stderr output.
func TestReporterPhaseFailDiag(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, ReporterOptions{})
	ph := r.Phase("pre-merge hook")
	ph.FailDiag(errors.New("hook failed"), map[string]any{
		"command": "just",
		"elapsed": "48s",
	})

	recs := collect(t, r, &buf)
	// node_start, stderr output (from err), node_end.
	if len(recs) != 3 {
		t.Fatalf("record count: got %d want 3", len(recs))
	}
	if o, ok := recs[1].(ndjsoncrap.Output); !ok || o.Stream != ndjsoncrap.StreamStderr {
		t.Fatalf("FailDiag should emit the error as stderr output first: %#v", recs[1])
	}
	ne, ok := recs[2].(ndjsoncrap.NodeEnd)
	if !ok || ne.ExitCode == nil || *ne.ExitCode != 1 {
		t.Fatalf("FailDiag node_end should be nonzero: %#v", recs[2])
	}
	if ne.Diagnostic["command"] != "just" || ne.Diagnostic["elapsed"] != "48s" {
		t.Fatalf("FailDiag diagnostic not carried on node_end: %#v", ne.Diagnostic)
	}
}

// FailDiag with a nil err must emit no stderr output record — just the
// node_end with the diagnostic.
func TestReporterPhaseFailDiagNilErr(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, ReporterOptions{})
	r.Phase("pre-merge hook").FailDiag(nil, map[string]any{"error": "hook failed"})

	recs := collect(t, r, &buf)
	if len(recs) != 2 {
		t.Fatalf("nil err must not emit an output record: %#v", recs)
	}
	ne, ok := recs[1].(ndjsoncrap.NodeEnd)
	if !ok || ne.ExitCode == nil || *ne.ExitCode != 1 || ne.Diagnostic["error"] != "hook failed" {
		t.Fatalf("node_end: %#v", recs[1])
	}
}

// The Phase API must emit conformant execution-family records, including a
// nested phase under an operation (parent linkage threaded by the Reporter).
func TestReporterPhaseAndNesting(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, ReporterOptions{})

	ph := r.Phase("connect")
	ph.Command("ssh host")
	ph.Output(ndjsoncrap.StreamStdout, "connected\n")
	ph.Done()

	op := r.Operation("sync", OpOptions{Total: 1})
	nested := op.Phase("scan")
	nested.Fail(errors.New("scan blew up"))
	op.Finish()

	recs := collect(t, r, &buf)

	// Top-level phase: node_start(tp1), command, output, node_end(ok).
	ns, ok := recs[0].(ndjsoncrap.NodeStart)
	if !ok || ns.Name != "connect" || ns.TP != 1 || ns.Parent != nil {
		t.Fatalf("phase node_start: %#v", recs[0])
	}
	if ne, ok := recs[3].(ndjsoncrap.NodeEnd); !ok || ne.ExitCode == nil || *ne.ExitCode != 0 {
		t.Fatalf("phase node_end should be exit 0: %#v", recs[3])
	}
	// Operation tp2, then a nested phase tp3 whose parent is the operation.
	os, ok := recs[4].(ndjsoncrap.OperationStart)
	if !ok || os.TP != 2 {
		t.Fatalf("operation_start tp: %#v", recs[4])
	}
	nested0, ok := recs[5].(ndjsoncrap.NodeStart)
	if !ok || nested0.Name != "scan" || nested0.TP != 3 || nested0.Parent == nil || *nested0.Parent != 2 {
		t.Fatalf("nested phase should parent to the operation tp: %#v", recs[5])
	}
	// Fail emits a stderr output then a nonzero node_end.
	if o, ok := recs[6].(ndjsoncrap.Output); !ok || o.Stream != ndjsoncrap.StreamStderr {
		t.Fatalf("phase Fail should emit stderr output first: %#v", recs[6])
	}
	if ne, ok := recs[7].(ndjsoncrap.NodeEnd); !ok || ne.ExitCode == nil || *ne.ExitCode != 1 {
		t.Fatalf("phase Fail node_end should be nonzero: %#v", recs[7])
	}
}
