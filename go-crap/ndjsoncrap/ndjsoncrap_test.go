package ndjsoncrap

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// A tap-ndjson(7) test record must decode to a Test with all fields,
// proving result-family wire compatibility with tap-dancer.
func TestDecodeTAPNdjsonTestRecord(t *testing.T) {
	line := `{"type":"test","n":2,"description":"parses negative numbers","ok":false,"directive":null,"diagnostic":{"message":"expected 42 got 41","severity":"fail"},"output":null,"subtest":null,"line":7}`
	rec, err := Decode([]byte(line))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	tr, ok := rec.(Test)
	if !ok {
		t.Fatalf("got %T, want Test", rec)
	}
	if tr.N != 2 || tr.OK || tr.Description != "parses negative numbers" || tr.Line != 7 {
		t.Fatalf("unexpected test: %+v", tr)
	}
	if tr.Diagnostic["message"] != "expected 42 got 41" {
		t.Fatalf("diagnostic not parsed: %+v", tr.Diagnostic)
	}
}

// Writing a Test with nullable fields unset must emit explicit nulls, so
// the output is byte-shaped like tap-ndjson(7).
func TestWriteTestEmitsNulls(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.Write(Test{N: 1, Description: "loads config", OK: true, Line: 3}); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(buf.String())
	want := `{"type":"test","n":1,"description":"loads config","ok":true,"directive":null,"diagnostic":null,"output":null,"subtest":null,"line":3}`
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

// Nested subtests must each carry "type":"test", and an empty summary
// diagnostics list must serialize as [] (not null) to match tap-ndjson(7).
func TestWriteStampsSubtestsAndSummaryArray(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.Write(Test{N: 1, Description: "parent", OK: true, Subtest: []Test{
		{N: 1, Description: "child", OK: true, Subtest: []Test{{N: 1, Description: "grandchild", OK: true}}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(Summary{Passed: 1, Total: 1, Valid: true}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, `"type":""`) {
		t.Fatalf("subtest left an empty type:\n%s", out)
	}
	if c := strings.Count(out, `"type":"test"`); c != 3 {
		t.Fatalf("want 3 test records stamped (parent+child+grandchild), got %d:\n%s", c, out)
	}
	if !strings.Contains(out, `"diagnostics":[]`) {
		t.Fatalf("empty summary diagnostics must be [] not null:\n%s", out)
	}
}

// just-us discriminators must normalize to the execution family.
func TestDecodeJustEventsAliases(t *testing.T) {
	cases := []struct {
		line     string
		wantType string
	}{
		{`{"type":"recipe_start","tp":2,"name":"bar","namepath":"bar","depth":1,"parent":1,"doc":"the bar recipe","quiet":false}`, "node_start"},
		{`{"type":"recipe_command","tp":1,"command":"echo hi","line":4}`, "command"},
		{`{"type":"recipe_complete","tp":1,"exit_code":null,"signal":"SIGINT","duration_ms":312}`, "node_end"},
		{`{"type":"output","tp":1,"stream":"stdout","format":"utf8","data":"hello\n"}`, "output"},
	}
	for _, c := range cases {
		rec, err := Decode([]byte(c.line))
		if err != nil {
			t.Fatalf("decode %s: %v", c.line, err)
		}
		if rec.RecordType() != c.wantType {
			t.Fatalf("decode %s: got type %q want %q", c.line, rec.RecordType(), c.wantType)
		}
	}
}

// just-us's plan carries recipe_count; it must populate Count.
func TestDecodeJustEventsPlan(t *testing.T) {
	rec, err := Decode([]byte(`{"type":"plan","version":1,"recipe_count":3}`))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := rec.(Plan)
	if !ok {
		t.Fatalf("got %T want Plan", rec)
	}
	if p.Count != 3 {
		t.Fatalf("recipe_count not mapped: %+v", p)
	}
}

// A recipe_complete with a SIGINT signal must surface the signal pointer.
func TestDecodeNodeEndSignal(t *testing.T) {
	rec, err := Decode([]byte(`{"type":"recipe_complete","tp":1,"exit_code":null,"signal":"SIGINT","duration_ms":312}`))
	if err != nil {
		t.Fatal(err)
	}
	n := rec.(NodeEnd)
	if n.ExitCode != nil {
		t.Fatalf("exit_code should be nil, got %v", *n.ExitCode)
	}
	if n.Signal == nil || *n.Signal != "SIGINT" {
		t.Fatalf("signal not decoded: %+v", n)
	}
}

// Unknown record types must decode to Unknown, not error (forward compat).
func TestDecodeUnknownType(t *testing.T) {
	rec, err := Decode([]byte(`{"type":"future_thing","whatever":42}`))
	if err != nil {
		t.Fatalf("unknown type must not error: %v", err)
	}
	u, ok := rec.(Unknown)
	if !ok {
		t.Fatalf("got %T want Unknown", rec)
	}
	if u.RecordType() != "future_thing" {
		t.Fatalf("unknown type lost: %q", u.RecordType())
	}
}

// Output with no format defaults to utf8 on both decode and encode.
func TestOutputFormatDefault(t *testing.T) {
	rec, err := Decode([]byte(`{"type":"output","tp":1,"stream":"stdout","data":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if rec.(Output).Format != FormatUTF8 {
		t.Fatalf("decode default format: %+v", rec)
	}
	var buf bytes.Buffer
	_ = NewWriter(&buf).Write(Output{TP: 1, Stream: StreamStdout, Data: "x"})
	if !strings.Contains(buf.String(), `"format":"utf8"`) {
		t.Fatalf("write default format missing: %s", buf.String())
	}
}

// Reader skips blank lines and reports io.EOF at the end.
func TestReaderSkipsBlanksAndEOF(t *testing.T) {
	in := "\n" +
		`{"type":"plan","count":1}` + "\n\n" +
		`{"type":"summary","passed":1,"failed":0,"skipped":0,"todo":0,"total":1,"plan_count":1,"bailed":false,"valid":true,"diagnostics":[]}` + "\n"
	r := NewReader(strings.NewReader(in))
	var got []string
	for {
		rec, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, rec.RecordType())
	}
	if len(got) != 2 || got[0] != "plan" || got[1] != "summary" {
		t.Fatalf("unexpected records: %v", got)
	}
}

// The Meta header stamps default schema versions when unset.
func TestWriteHeaderDefaults(t *testing.T) {
	var buf bytes.Buffer
	if err := NewWriter(&buf).WriteHeader("my run", "go-test"); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(buf.String())
	want := `{"type":"crap","version":2,"ndjson":1,"title":"my run","source":"go-test"}`
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}
