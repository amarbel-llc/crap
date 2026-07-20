package viewport

import (
	"encoding/json"
	"strings"
	"testing"

	"code.linenisgreat.com/crap/go-crap/v2/ndjsoncrap"
)

// wrapOutput builds the JSON encoding of an output record carrying data, as it
// appears on one line of an ndjson-crap stream. Nesting one wrapOutput's JSON
// as another's data reproduces the crap-within-crap the cascade produces.
func wrapOutput(tp int, data string) string {
	b, err := json.Marshal(ndjsoncrap.Output{
		Type: "output", TP: tp, Stream: ndjsoncrap.StreamStdout,
		Format: ndjsoncrap.FormatUTF8, Data: data,
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Plain (non-record) output text must pass through verbatim.
func TestUnnestPlainTextVerbatim(t *testing.T) {
	got := unnestOutput("direnv: using flake\nbuilding\n")
	if len(got) != 2 || got[0] != "direnv: using flake" || got[1] != "building" {
		t.Fatalf("plain text should pass through verbatim: %#v", got)
	}
}

// The exact crap#34 scenario: a single-level nested output record must render
// its innermost human text, not escaped JSON.
func TestUnnestSingleLevel(t *testing.T) {
	// Outer output's data is one inner output record + newline.
	data := wrapOutput(1, "direnv: using flake\n") + "\n"
	got := unnestOutput(data)
	if len(got) != 1 || got[0] != "direnv: using flake" {
		t.Fatalf("single-level nesting not unwrapped: %#v", got)
	}
}

// Nesting must unwrap recursively down to the innermost text.
func TestUnnestRecursive(t *testing.T) {
	lvl1 := wrapOutput(1, "compiling\n")
	data := wrapOutput(2, lvl1+"\n") + "\n"
	got := unnestOutput(data)
	if len(got) != 1 || got[0] != "compiling" {
		t.Fatalf("recursive nesting not unwrapped: %#v", got)
	}
}

// A nested command record renders "$ cmd"; a node_start renders its name;
// structural records (node_end) render nothing.
func TestUnnestExecutionRecords(t *testing.T) {
	data := strings.Join([]string{
		`{"type":"node_start","tp":1,"name":"build","namepath":"build","depth":0,"parent":null,"doc":null,"quiet":false}`,
		`{"type":"command","tp":1,"command":"go build ./...","line":1}`,
		wrapOutput(1, "ok\n"),
		`{"type":"node_end","tp":1,"exit_code":0,"signal":null,"duration_ms":1}`,
	}, "\n")
	got := unnestOutput(data)
	want := []string{"build", "$ go build ./...", "ok"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("execution records not rendered cleanly: got %#v want %#v", got, want)
	}
}

// A JSON line whose "type" is not a crap record (a log line the child tool
// merely printed) must be kept verbatim, not swallowed.
func TestUnnestUnknownJSONVerbatim(t *testing.T) {
	line := `{"type":"info","msg":"hello"}`
	got := unnestOutput(line)
	if len(got) != 1 || got[0] != line {
		t.Fatalf("unknown JSON should pass through verbatim: %#v", got)
	}
}

// The recursion is depth-bounded: a pathologically deep chain does not recurse
// past the guard, and the record at the limit is kept verbatim rather than
// dropped.
func TestUnnestDepthGuard(t *testing.T) {
	data := "leaf\n"
	for i := 0; i < maxUnnestDepth+3; i++ {
		data = wrapOutput(i, data) + "\n"
	}
	got := unnestOutput(data)
	// Whatever the guard leaves, it must be a single line and must not be lost.
	if len(got) != 1 {
		t.Fatalf("depth guard should collapse to one retained line: %#v", got)
	}
}

// End to end through the driver: a node whose output record nests an inner
// crap stream must feed the tail the innermost text, never escaped JSON
// (crap#34).
func TestDriverUnnestsNestedOutput(t *testing.T) {
	inner := wrapOutput(1, "the real error\n")
	outer := wrapOutput(9, inner+"\n")
	stream := strings.Join([]string{
		`{"type":"node_start","tp":9,"name":"repo","namepath":"repo","depth":0,"parent":null,"doc":null,"quiet":false}`,
		outer,
		`{"type":"node_end","tp":9,"exit_code":1,"signal":null,"duration_ms":1}`,
	}, "\n")

	var logs []string
	for _, m := range drive(t, stream) {
		if ll, ok := m.(LogLine); ok {
			logs = append(logs, ll.Text)
		}
	}
	if len(logs) != 1 || logs[0] != "the real error" {
		t.Fatalf("driver did not unwrap nested output: %#v", logs)
	}
	for _, l := range logs {
		if strings.Contains(l, `"type"`) || strings.Contains(l, `\"`) {
			t.Fatalf("tail still carries escaped JSON soup: %q", l)
		}
	}
}
