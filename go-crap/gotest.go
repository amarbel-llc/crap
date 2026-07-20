package crap

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"code.linenisgreat.com/crap/go-crap/v2/ndjsoncrap"
)

type testEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test"`
	Elapsed float64   `json:"Elapsed"`
	Output  string    `json:"Output"`
}

type testResult struct {
	name    string
	action  string // pass, fail, skip
	elapsed float64
	output  strings.Builder
}

type packageResult struct {
	name    string
	tests   []*testResult
	testMap map[string]*testResult
	output  strings.Builder
	failed  bool
	elapsed float64
}

var fileLineRe = regexp.MustCompile(`(\w[\w_]*\.go):(\d+):`)

func parseFileLine(output string) (file string, line string) {
	m := fileLineRe.FindStringSubmatch(output)
	if m != nil {
		return m[1], m[2]
	}
	return "", ""
}

// ConvertGoTest reads `go test -json` events from r and writes ndjson-crap to
// w: one top-level test record per package, with the package's tests (and
// their subtests) nested as subtest records. Returns an exit code: 0 when no
// package failed, 1 otherwise.
func ConvertGoTest(r io.Reader, w io.Writer) int {
	dec := json.NewDecoder(r)

	packages := make(map[string]*packageResult)
	var packageOrder []string

	for {
		var ev testEvent
		if err := dec.Decode(&ev); err != nil {
			if err == io.EOF {
				break
			}
			// Skip unparseable lines; go test sometimes interleaves plain text.
			continue
		}

		pkg := packages[ev.Package]
		if pkg == nil {
			pkg = &packageResult{name: ev.Package, testMap: make(map[string]*testResult)}
			packages[ev.Package] = pkg
			packageOrder = append(packageOrder, ev.Package)
		}

		if ev.Test == "" {
			switch ev.Action {
			case "output":
				pkg.output.WriteString(ev.Output)
			case "pass", "skip":
				pkg.elapsed = ev.Elapsed
			case "fail":
				pkg.failed = true
				pkg.elapsed = ev.Elapsed
			}
			continue
		}

		tr := pkg.testMap[ev.Test]
		if tr == nil {
			tr = &testResult{name: ev.Test}
			pkg.testMap[ev.Test] = tr
			pkg.tests = append(pkg.tests, tr)
		}
		switch ev.Action {
		case "output":
			tr.output.WriteString(ev.Output)
		case "pass", "fail", "skip":
			tr.action = ev.Action
			tr.elapsed = ev.Elapsed
		}
	}

	exitCode := 0
	var tops []ndjsoncrap.Test
	for _, name := range packageOrder {
		pkg := packages[name]
		if len(pkg.tests) == 0 {
			tops = append(tops, ndjsoncrap.Test{
				Description: pkg.name,
				OK:          true,
				Directive:   skipDirective(emptyPackageReason(pkg.output.String())),
			})
			continue
		}
		var subs []ndjsoncrap.Test
		for _, tr := range pkg.tests {
			if strings.Contains(tr.name, "/") {
				continue // nested under its parent
			}
			subs = append(subs, buildGoTest(pkg, tr, len(subs)+1))
		}
		if pkg.failed {
			exitCode = 1
		}
		tops = append(tops, ndjsoncrap.Test{
			Description: pkg.name,
			OK:          !pkg.failed,
			Subtest:     subs,
		})
	}

	if err := writeResultStream(w, "go test", "go-test", tops); err != nil {
		fmt.Fprintf(io.Discard, "%v", err) // emission errors are non-fatal to the run
	}
	return exitCode
}

// buildGoTest builds the ndjson-crap test record for one go test result,
// recursing into "parent/child" subtests.
func buildGoTest(pkg *packageResult, tr *testResult, n int) ndjsoncrap.Test {
	prefix := tr.name + "/"
	var children []*testResult
	for _, child := range pkg.tests {
		if strings.HasPrefix(child.name, prefix) && !strings.Contains(child.name[len(prefix):], "/") {
			children = append(children, child)
		}
	}

	display := tr.name
	if idx := strings.LastIndex(display, "/"); idx >= 0 {
		display = display[idx+1:]
	}

	if len(children) > 0 {
		var subs []ndjsoncrap.Test
		for _, child := range children {
			subs = append(subs, buildGoTest(pkg, child, len(subs)+1))
		}
		return ndjsoncrap.Test{
			N:           n,
			Description: display,
			OK:          tr.action != "fail",
			Subtest:     subs,
		}
	}

	output := cleanTestOutput(tr.output.String())
	t := ndjsoncrap.Test{N: n, Description: display}
	switch tr.action {
	case "skip":
		t.OK = true
		t.Directive = skipDirective(extractSkipReason(output))
	case "fail":
		t.OK = false
		t.Output = strPtr(output)
		diag := map[string]any{
			"elapsed": fmt.Sprintf("%.3f", tr.elapsed),
			"package": pkg.name,
		}
		if file, line := parseFileLine(output); file != "" {
			diag["file"] = file
			diag["line"] = line
		}
		t.Diagnostic = diag
	default:
		t.OK = true
	}
	return t
}

func emptyPackageReason(output string) string {
	switch {
	case strings.Contains(output, "[no test files]"):
		return "no test files"
	case strings.Contains(output, "no tests to run"):
		return "no tests to run"
	case strings.Contains(output, "[setup failed]"):
		return "setup failed"
	default:
		return "no tests"
	}
}

func cleanTestOutput(raw string) string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "=== RUN") ||
			strings.HasPrefix(trimmed, "=== PAUSE") ||
			strings.HasPrefix(trimmed, "=== CONT") ||
			strings.HasPrefix(trimmed, "--- PASS") ||
			strings.HasPrefix(trimmed, "--- FAIL") ||
			strings.HasPrefix(trimmed, "--- SKIP") ||
			trimmed == "PASS" || trimmed == "FAIL" || trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return strings.Join(lines, "\n")
}

func extractSkipReason(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--- SKIP") ||
			strings.HasPrefix(trimmed, "=== RUN") ||
			strings.HasPrefix(trimmed, "=== PAUSE") ||
			strings.HasPrefix(trimmed, "=== CONT") {
			continue
		}
		return trimmed
	}
	return ""
}
