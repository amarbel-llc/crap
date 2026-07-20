package crap

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"code.linenisgreat.com/crap/go-crap/v2/ndjsoncrap"
)

// collect decodes every record from an ndjson-crap stream.
func collect(t *testing.T, s string) []ndjsoncrap.Record {
	t.Helper()
	r := ndjsoncrap.NewReader(strings.NewReader(s))
	var recs []ndjsoncrap.Record
	for {
		rec, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode emitted stream: %v", err)
		}
		recs = append(recs, rec)
	}
	return recs
}

func TestConvertGoTestEmitsNdjsonCrap(t *testing.T) {
	// A passing package with one test and a failing package with one test.
	in := strings.Join([]string{
		`{"Action":"run","Package":"pkg/a","Test":"TestOne"}`,
		`{"Action":"pass","Package":"pkg/a","Test":"TestOne","Elapsed":0.01}`,
		`{"Action":"pass","Package":"pkg/a","Elapsed":0.02}`,
		`{"Action":"run","Package":"pkg/b","Test":"TestTwo"}`,
		`{"Action":"output","Package":"pkg/b","Test":"TestTwo","Output":"    foo_test.go:12: boom\n"}`,
		`{"Action":"fail","Package":"pkg/b","Test":"TestTwo","Elapsed":0.03}`,
		`{"Action":"fail","Package":"pkg/b","Elapsed":0.04}`,
	}, "\n")

	var buf bytes.Buffer
	code := ConvertGoTest(strings.NewReader(in), &buf)
	if code != 1 {
		t.Fatalf("exit code: got %d want 1 (a package failed)", code)
	}

	recs := collect(t, buf.String())
	// Meta, plan, 2 tests, summary.
	if len(recs) != 5 {
		t.Fatalf("record count: got %d want 5\n%s", len(recs), buf.String())
	}
	if _, ok := recs[0].(ndjsoncrap.Meta); !ok {
		t.Fatalf("first record should be Meta, got %T", recs[0])
	}
	if p, ok := recs[1].(ndjsoncrap.Plan); !ok || p.Count != 2 {
		t.Fatalf("plan should be count 2, got %#v", recs[1])
	}

	a := recs[2].(ndjsoncrap.Test)
	if a.Description != "pkg/a" || !a.OK || len(a.Subtest) != 1 || a.Subtest[0].Description != "TestOne" {
		t.Fatalf("package a wrong: %#v", a)
	}
	b := recs[3].(ndjsoncrap.Test)
	if b.Description != "pkg/b" || b.OK || len(b.Subtest) != 1 {
		t.Fatalf("package b wrong: %#v", b)
	}
	sub := b.Subtest[0]
	if sub.OK || sub.Output == nil || !strings.Contains(*sub.Output, "boom") {
		t.Fatalf("failing subtest should carry output: %#v", sub)
	}
	if sub.Diagnostic["file"] != "foo_test.go" || sub.Diagnostic["line"] != "12" {
		t.Fatalf("file:line diagnostic missing: %#v", sub.Diagnostic)
	}

	s := recs[4].(ndjsoncrap.Summary)
	if s.Passed != 1 || s.Failed != 1 || s.Total != 2 || !s.Valid {
		t.Fatalf("summary wrong: %#v", s)
	}
}

func TestConvertGoTestEmptyPackageSkips(t *testing.T) {
	in := strings.Join([]string{
		`{"Action":"output","Package":"pkg/empty","Output":"?   \tpkg/empty\t[no test files]\n"}`,
		`{"Action":"pass","Package":"pkg/empty","Elapsed":0}`,
	}, "\n")
	var buf bytes.Buffer
	code := ConvertGoTest(strings.NewReader(in), &buf)
	if code != 0 {
		t.Fatalf("empty package should not fail the run, got %d", code)
	}
	recs := collect(t, buf.String())
	tp := recs[2].(ndjsoncrap.Test)
	if tp.Directive == nil || tp.Directive.Kind != "skip" || tp.Directive.Reason != "no test files" {
		t.Fatalf("empty package should skip: %#v", tp)
	}
}

func TestConvertCargoTestEmitsNdjsonCrap(t *testing.T) {
	in := strings.Join([]string{
		"Running unittests src/lib.rs (target/debug/deps/foo-abc)",
		"running 2 tests",
		"test tests::passes ... ok",
		"test tests::fails ... FAILED",
		"failures:",
		"---- tests::fails stdout ----",
		"thread 'tests::fails' panicked at src/lib.rs:42:9:",
		"assertion failed",
		"test result: FAILED. 1 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s",
	}, "\n")

	var buf bytes.Buffer
	code := ConvertCargoTest(strings.NewReader(in), &buf)
	if code != 1 {
		t.Fatalf("exit code: got %d want 1", code)
	}
	recs := collect(t, buf.String())
	suite := recs[2].(ndjsoncrap.Test)
	if suite.OK || len(suite.Subtest) != 2 {
		t.Fatalf("suite should fail with 2 subtests: %#v", suite)
	}
	fail := suite.Subtest[1]
	if fail.OK || fail.Diagnostic["file"] != "src/lib.rs" || fail.Diagnostic["line"] != "42" {
		t.Fatalf("failing cargo test should carry file:line: %#v", fail)
	}
}

func TestConvertExecEmitsExecutionFamily(t *testing.T) {
	var buf bytes.Buffer
	code := ConvertExec(context.Background(), "sh", []string{"-c", "echo hello; echo oops 1>&2; exit 3"}, &buf)
	if code != 3 {
		t.Fatalf("exec should surface exit code 3, got %d", code)
	}
	recs := collect(t, buf.String())
	start, ok := recs[0].(ndjsoncrap.NodeStart)
	if !ok {
		t.Fatalf("first record should be node_start, got %T", recs[0])
	}
	if start.TP != 1 {
		t.Fatalf("default node id should be 1, got %d", start.TP)
	}
	if start.Name != "sh -c echo hello; echo oops 1>&2; exit 3" {
		t.Fatalf("default label should be the joined command line, got %q", start.Name)
	}
	var sawHello, sawOops bool
	var end *ndjsoncrap.NodeEnd
	for _, r := range recs {
		switch v := r.(type) {
		case ndjsoncrap.Output:
			if v.Stream == "stdout" && strings.TrimSpace(v.Data) == "hello" {
				sawHello = true
			}
			if v.Stream == "stderr" && strings.TrimSpace(v.Data) == "oops" {
				sawOops = true
			}
		case ndjsoncrap.NodeEnd:
			end = &v
		}
	}
	if !sawHello || !sawOops {
		t.Fatalf("missing stdout/stderr output records: hello=%v oops=%v", sawHello, sawOops)
	}
	if end == nil || end.ExitCode == nil || *end.ExitCode != 3 {
		t.Fatalf("node_end exit code wrong: %#v", end)
	}
	if end.Diagnostic == nil || end.Diagnostic["error"] == nil || end.Diagnostic["command"] == nil {
		t.Fatalf("failing node_end should carry an error/command diagnostic: %#v", end.Diagnostic)
	}
}

// ConvertExecOpts stamps the caller's node id and label on every record so
// concatenated invocations compose into one multi-node stream; a successful
// node_end carries no diagnostic.
func TestConvertExecOptsStampsNodeIDAndLabel(t *testing.T) {
	var buf bytes.Buffer
	code := ConvertExecOpts(context.Background(), "sh", []string{"-c", "echo hi"}, &buf,
		ExecOptions{TP: 7, Name: "dodder"})
	if code != 0 {
		t.Fatalf("exit code: got %d want 0", code)
	}
	recs := collect(t, buf.String())
	start, ok := recs[0].(ndjsoncrap.NodeStart)
	if !ok || start.TP != 7 || start.Name != "dodder" {
		t.Fatalf("node_start should carry tp=7 name=dodder: %#v", recs[0])
	}
	var sawOutput bool
	var end *ndjsoncrap.NodeEnd
	for _, r := range recs {
		switch v := r.(type) {
		case ndjsoncrap.Output:
			sawOutput = true
			if v.TP != 7 {
				t.Fatalf("output should carry tp=7: %#v", v)
			}
		case ndjsoncrap.NodeEnd:
			end = &v
		}
	}
	if !sawOutput {
		t.Fatal("missing output record")
	}
	if end == nil || end.TP != 7 || end.ExitCode == nil || *end.ExitCode != 0 {
		t.Fatalf("node_end should carry tp=7 exit_code=0: %#v", end)
	}
	if end.Diagnostic != nil {
		t.Fatalf("successful node_end must not carry a diagnostic: %#v", end.Diagnostic)
	}
}
