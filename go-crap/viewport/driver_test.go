package viewport

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"code.linenisgreat.com/crap/go-crap/v2/ndjsoncrap"
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
	var done *BatchDone
	for _, m := range msgs {
		switch v := m.(type) {
		case PhaseStarted:
			started = append(started, v)
		case PhaseEnded:
			ended = append(ended, v)
		case LogLine:
			logs = append(logs, v)
		case BatchDone:
			done = &v
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
	// No summary record, so the driver must synthesize a finalization — and
	// the SIGINT node failure must fail the run verdict (#24).
	if done == nil {
		t.Fatal("driver must finalize a summary-less stream")
	}
	if done.Err == nil {
		t.Fatal("signal-failed node must fail the run verdict")
	}
}

// batchErr returns the Err of the single BatchDone in msgs, failing if none.
func batchErr(t *testing.T, msgs []tea.Msg) error {
	t.Helper()
	for _, m := range msgs {
		if bd, ok := m.(BatchDone); ok {
			return bd.Err
		}
	}
	t.Fatal("no BatchDone emitted")
	return nil
}

// A failing execution node must fail the run verdict even when a clean
// result-family summary follows (#24): producers that moved a unit from Test
// to Phase+FailDiag per #22 must not render under a green final frame.
func TestDriverFailedNodeFailsRunDespiteCleanSummary(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"node_start","tp":1,"name":"hook","namepath":"hook","depth":0,"parent":null,"doc":null,"quiet":false}`,
		`{"type":"node_end","tp":1,"exit_code":1,"signal":null,"duration_ms":5,"diagnostic":{"error":"hook failed"}}`,
		`{"type":"summary","passed":0,"failed":0,"skipped":0,"todo":0,"total":0,"plan_count":0,"bailed":false,"valid":true,"diagnostics":[]}`,
	}, "\n")
	if err := batchErr(t, drive(t, stream)); err == nil {
		t.Fatal("failing node_end must fail the run despite a clean summary")
	}
}

// On a summary-less stream (EOF path), failing nodes and failed operation
// items must fail the run verdict (#24).
func TestDriverFailuresFailRunOnEOF(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"node_start","tp":1,"name":"hook","namepath":"hook","depth":0,"parent":null,"doc":null,"quiet":false}`,
		`{"type":"node_end","tp":1,"exit_code":1,"signal":null,"duration_ms":5}`,
		`{"type":"operation_start","tp":2,"name":"sync","parent":null,"depth":0,"total":1,"bytes_total":0}`,
		`{"type":"item","op":2,"label":"blob-a","state":"failed","bytes":0,"diagnostic":{"error":"boom"}}`,
	}, "\n")
	if err := batchErr(t, drive(t, stream)); err == nil {
		t.Fatal("failures on a summary-less stream must fail the run")
	}
}

// A not-ok test under a todo directive is an expected failure (TAP
// semantics): it must NOT fail the run on the EOF path.
func TestDriverTodoFailureDoesNotFailRun(t *testing.T) {
	stream := `{"type":"test","n":1,"description":"flaky thing","ok":false,"directive":{"kind":"todo","reason":"wip"},"diagnostic":null,"output":null,"subtest":null,"line":1}`
	if err := batchErr(t, drive(t, stream)); err != nil {
		t.Fatalf("todo failure must not fail the run: %v", err)
	}
}

// A node_end carrying a producer diagnostic (crap#22) must surface it in the
// verdict, merged with the exit_code/signal synthesis.
func TestDriverNodeEndDiagnostic(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"node_start","tp":1,"name":"pre-merge hook","namepath":"pre-merge hook","depth":0,"parent":null,"doc":null,"quiet":false}`,
		`{"type":"node_end","tp":1,"exit_code":1,"signal":null,"duration_ms":480,"diagnostic":{"command":"just","error":"hook failed"}}`,
	}, "\n")
	msgs := drive(t, stream)
	for _, m := range msgs {
		if pe, ok := m.(PhaseEnded); ok {
			if pe.Verdict.OK {
				t.Fatalf("exit 1 must fail: %#v", pe.Verdict)
			}
			d := pe.Verdict.Diagnostic
			if d["command"] != "just" || d["error"] != "hook failed" {
				t.Fatalf("producer diagnostic not merged into verdict: %#v", d)
			}
			if d["exit_code"] != 1 {
				t.Fatalf("exit_code synthesis lost in merge: %#v", d)
			}
			return
		}
	}
	t.Fatal("no PhaseEnded emitted")
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

// An operation-family stream (crap RFC 0001): operation_start arms the bars,
// items feed the tail / persist failures and advance progress, operation_end
// persists a tallied verdict. Asserts the §7 ordered mapping and that a failed
// item does NOT reset the determinate bar.
func TestDriverOperationFamily(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"operation_start","tp":3,"name":"sync","parent":null,"depth":0,"total":3,"bytes_total":1000}`,
		`{"type":"item","op":3,"label":"blob-a","state":"done","bytes":100,"diagnostic":null}`,
		`{"type":"item","op":3,"label":"blob-b","state":"skipped","bytes":0,"diagnostic":null,"directive":{"kind":"skip","reason":"exists"}}`,
		`{"type":"item","op":3,"label":"blob-c","state":"failed","bytes":50,"diagnostic":{"error":"write failed"}}`,
		`{"type":"operation_end","op":3,"done":1,"skipped":1,"failed":1,"total":3,"ok":false,"duration_ms":42}`,
	}, "\n")
	msgs := drive(t, stream)

	// operation_start always arms the item bar via OperationStarted.
	if os, ok := msgs[0].(OperationStarted); !ok || os.Name != "sync" || os.Total != 3 {
		t.Fatalf("first msg should arm bar (name=sync total=3), got %#v", msgs[0])
	}

	var logs []LogLine
	var failed []ItemFailed
	var progresses []OperationProgress
	var ended []PhaseEnded
	sawDone := false
	for _, m := range msgs {
		switch v := m.(type) {
		case LogLine:
			logs = append(logs, v)
		case ItemFailed:
			failed = append(failed, v)
		case OperationProgress:
			progresses = append(progresses, v)
		case PhaseEnded:
			ended = append(ended, v)
		case BatchDone:
			sawDone = true
		}
	}

	// The skipped item feeds the tail dimmed (↷); the done item plainly.
	if len(logs) != 2 || logs[0].Text != "blob-a" || logs[1].Text != "↷ blob-b" {
		t.Fatalf("item tail lines wrong: %#v", logs)
	}
	// The failed item persists via ItemFailed (not PhaseEnded).
	if len(failed) != 1 || failed[0].Label != "blob-c" || failed[0].Diagnostic["error"] != "write failed" {
		t.Fatalf("failed item should persist via ItemFailed: %#v", failed)
	}
	// Progress advanced 1->2->3 across the items (the byte-bar arm is a 4th).
	var currents []int
	for _, p := range progresses {
		if p.Current > 0 {
			currents = append(currents, p.Current)
		}
	}
	if len(currents) != 3 || currents[0] != 1 || currents[1] != 2 || currents[2] != 3 {
		t.Fatalf("progress should advance 1,2,3 (bar not reset by the failure): %v", currents)
	}
	// operation_end persists one tallied verdict.
	if len(ended) != 1 || ended[0].Verdict.OK {
		t.Fatalf("operation_end should persist a failing verdict: %#v", ended)
	}
	if !strings.Contains(ended[0].Description, "1 done, 1 skipped, 1 failed") || !strings.HasPrefix(ended[0].Description, "sync — ") {
		t.Fatalf("operation_end tally description wrong: %q", ended[0].Description)
	}
	if !sawDone {
		t.Fatal("driver must finalize a summary-less operation stream")
	}
	// The failed item is counted once: its failing operation_end must not
	// re-count it (#24 review followup).
	if err := batchErr(t, msgs); err == nil || err.Error() != "1 failed" {
		t.Fatalf("run verdict should count the failed item exactly once: %v", err)
	}
}

// presentPlain must print a node_end's producer diagnostic under the verdict
// (crap#22), alongside the synthesized exit_code.
func TestPresentPlainNodeEndDiagnostic(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"node_start","tp":1,"name":"pre-merge hook","namepath":"pre-merge hook","depth":0,"parent":null,"doc":null,"quiet":false}`,
		`{"type":"node_end","tp":1,"exit_code":1,"signal":null,"duration_ms":480,"diagnostic":{"error":"hook failed"}}`,
	}, "\n")
	var buf strings.Builder
	if err := Present(strings.NewReader(stream), Options{Out: &buf, IsTTY: false}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "✗ pre-merge hook") {
		t.Fatalf("plain output missing verdict:\n%s", got)
	}
	if !strings.Contains(got, "error: hook failed") || !strings.Contains(got, "exit_code: 1") {
		t.Fatalf("plain output missing merged diagnostic:\n%s", got)
	}
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
