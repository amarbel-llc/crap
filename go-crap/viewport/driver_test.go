package viewport

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amarbel-llc/crap/go-crap/v2/ndjsoncrap"
)

// fakeSender records the messages a Driver emits.
type fakeSender struct{ msgs []tea.Msg }

func (f *fakeSender) Send(m tea.Msg) { f.msgs = append(f.msgs, m) }

func drive(t *testing.T, stream string) []tea.Msg {
	t.Helper()
	fs := &fakeSender{}
	if err := NewDriver(fs).Run(ndjsoncrap.NewReader(strings.NewReader(stream))); err != nil {
		t.Fatalf("driver run: %v", err)
	}
	return fs.msgs
}

// A result-family stream: plan arms the bar, each test persists a verdict,
// the summary ends the run with an error reflecting failures.
func TestDriverResultFamily(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"plan","count":2}`,
		`{"type":"test","n":1,"description":"loads config","ok":true,"directive":null,"diagnostic":null,"output":null,"subtest":null,"line":1}`,
		`{"type":"test","n":2,"description":"parses","ok":false,"directive":null,"diagnostic":{"message":"boom"},"output":"line a\nline b\n","subtest":null,"line":2}`,
		`{"type":"summary","passed":1,"failed":1,"skipped":0,"todo":0,"total":2,"plan_count":2,"bailed":false,"valid":true,"diagnostics":[]}`,
	}, "\n")
	msgs := drive(t, stream)

	if os, ok := msgs[0].(OperationStarted); !ok || os.Total != 2 {
		t.Fatalf("first msg should arm bar with total 2, got %#v", msgs[0])
	}

	var ended []PhaseEnded
	var logs []LogLine
	var done *BatchDone
	for _, m := range msgs {
		switch v := m.(type) {
		case PhaseEnded:
			ended = append(ended, v)
		case LogLine:
			logs = append(logs, v)
		case BatchDone:
			done = &v
		}
	}
	if len(ended) != 2 {
		t.Fatalf("want 2 PhaseEnded, got %d", len(ended))
	}
	if !ended[0].Verdict.OK || ended[1].Verdict.OK {
		t.Fatalf("verdicts wrong: %#v", ended)
	}
	// The failing test's output must precede its verdict in the tail.
	if len(logs) != 2 || logs[0].Text != "line a" || logs[1].Text != "line b" {
		t.Fatalf("output lines not fed to tail: %#v", logs)
	}
	if done == nil || done.Err == nil {
		t.Fatalf("summary with a failure must finalize with an error: %#v", done)
	}
}

// A skip directive must render as a directive verdict, not a pass/fail.
func TestDriverSkipDirective(t *testing.T) {
	stream := `{"type":"test","n":1,"description":"net","ok":true,"directive":{"kind":"skip","reason":"offline"},"diagnostic":null,"output":null,"subtest":null,"line":1}`
	msgs := drive(t, stream)
	for _, m := range msgs {
		if pe, ok := m.(PhaseEnded); ok {
			if pe.Verdict.Directive == nil || pe.Verdict.Directive.Kind != "skip" {
				t.Fatalf("expected skip directive verdict: %#v", pe.Verdict)
			}
			return
		}
	}
	t.Fatal("no PhaseEnded emitted")
}

// An execution-family (just-us) stream: node_start opens a phase, command
// and output feed the tail, node_end closes it with a verdict derived from
// the exit code/signal.
func TestDriverExecutionFamily(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"recipe_start","tp":1,"name":"build","namepath":"build","depth":0,"parent":null,"doc":null,"quiet":false}`,
		`{"type":"recipe_command","tp":1,"command":"go build ./...","line":3}`,
		`{"type":"output","tp":1,"stream":"stdout","format":"utf8","data":"compiling\n"}`,
		`{"type":"recipe_complete","tp":1,"exit_code":0,"signal":null,"duration_ms":120}`,
		`{"type":"recipe_start","tp":2,"name":"test","namepath":"test","depth":0,"parent":null,"doc":null,"quiet":false}`,
		`{"type":"recipe_complete","tp":2,"exit_code":null,"signal":"SIGINT","duration_ms":5}`,
	}, "\n")
	msgs := drive(t, stream)

	var started []PhaseStarted
	var ended []PhaseEnded
	var logs []LogLine
	sawDone := false
	for _, m := range msgs {
		switch v := m.(type) {
		case PhaseStarted:
			started = append(started, v)
		case PhaseEnded:
			ended = append(ended, v)
		case LogLine:
			logs = append(logs, v)
		case BatchDone:
			sawDone = true
		}
	}
	if len(started) != 2 || started[0].Description != "build" || started[1].Description != "test" {
		t.Fatalf("phase starts wrong: %#v", started)
	}
	if len(ended) != 2 {
		t.Fatalf("want 2 phase ends, got %d", len(ended))
	}
	if ended[0].Description != "build" || !ended[0].Verdict.OK {
		t.Fatalf("build phase should pass and be named: %#v", ended[0])
	}
	if ended[1].Description != "test" || ended[1].Verdict.OK {
		t.Fatalf("test phase should fail (signal): %#v", ended[1])
	}
	if ended[1].Verdict.Diagnostic["signal"] != "SIGINT" {
		t.Fatalf("signal diagnostic missing: %#v", ended[1].Verdict.Diagnostic)
	}
	if len(logs) != 2 || logs[0].Text != "$ go build ./..." || logs[1].Text != "compiling" {
		t.Fatalf("command/output not fed to tail: %#v", logs)
	}
	// No summary record, so the driver must synthesize a finalization.
	if !sawDone {
		t.Fatal("driver must finalize a summary-less stream")
	}
}

// base64 output must be decoded before feeding the tail.
func TestDriverBase64Output(t *testing.T) {
	// "hi there\n" base64 == "aGkgdGhlcmUK"
	stream := strings.Join([]string{
		`{"type":"recipe_start","tp":1,"name":"x","namepath":"x","depth":0,"parent":null,"doc":null,"quiet":false}`,
		`{"type":"output","tp":1,"stream":"stdout","format":"base64","data":"aGkgdGhlcmUK"}`,
		`{"type":"recipe_complete","tp":1,"exit_code":0,"signal":null,"duration_ms":1}`,
	}, "\n")
	msgs := drive(t, stream)
	for _, m := range msgs {
		if ll, ok := m.(LogLine); ok {
			if ll.Text != "hi there" {
				t.Fatalf("base64 not decoded: %q", ll.Text)
			}
			return
		}
	}
	t.Fatal("no LogLine emitted for base64 output")
}

// presentPlain renders one verdict line per test on a non-TTY sink.
func TestPresentPlain(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"test","n":1,"description":"ok one","ok":true,"directive":null,"diagnostic":null,"output":null,"subtest":null,"line":1}`,
		`{"type":"test","n":2,"description":"bad two","ok":false,"directive":null,"diagnostic":{"message":"nope"},"output":null,"subtest":null,"line":2}`,
	}, "\n")
	var buf strings.Builder
	if err := Present(strings.NewReader(stream), Options{Out: &buf, IsTTY: false}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "✓ ok one") || !strings.Contains(got, "✗ bad two") {
		t.Fatalf("plain output missing verdicts:\n%s", got)
	}
	if !strings.Contains(got, "message: nope") {
		t.Fatalf("plain output missing diagnostic:\n%s", got)
	}
}
