package crap

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/amarbel-llc/crap/go-crap/ndjsoncrap"
)

type cargoTestResult struct {
	name   string
	event  string // ok, failed, ignored
	stdout string
}

type cargoSuiteResult struct {
	name      string
	tests     []*cargoTestResult
	testCount int
	failed    bool
}

var rustFileLineRe = regexp.MustCompile(`([\w][\w_/]*\.rs):(\d+):`)

func parseRustFileLine(output string) (file string, line string) {
	m := rustFileLineRe.FindStringSubmatch(output)
	if m != nil {
		return m[1], m[2]
	}
	return "", ""
}

var (
	runningTestsRe        = regexp.MustCompile(`^running (\d+) tests?$`)
	testResultRe          = regexp.MustCompile(`^test (.+) \.\.\. (ok|FAILED|ignored)$`)
	testSummaryRe         = regexp.MustCompile(`^test result: (ok|FAILED)\. (\d+) passed; (\d+) failed; (\d+) ignored;`)
	failureStdoutHeaderRe = regexp.MustCompile(`^---- (.+) stdout ----$`)
)

// ConvertCargoTest reads `cargo test` pretty output from r and writes
// ndjson-crap to w: one top-level test record per suite, with the suite's
// tests nested as subtest records. Returns an exit code: 0 when no suite
// failed, 1 otherwise.
func ConvertCargoTest(r io.Reader, w io.Writer) int {
	scanner := bufio.NewScanner(r)

	var suites []*cargoSuiteResult
	var current *cargoSuiteResult
	failureStdout := make(map[string]string)
	var capturingFailure string

	flush := func() {
		if current == nil {
			return
		}
		for _, tr := range current.tests {
			if out, ok := failureStdout[tr.name]; ok {
				tr.stdout = out
			}
		}
		failureStdout = make(map[string]string)
		suites = append(suites, current)
		current = nil
	}

	for scanner.Scan() {
		line := scanner.Text()

		if name := parseCargoBinaryLine(line); name != "" {
			flush()
			current = &cargoSuiteResult{name: name}
			capturingFailure = ""
			continue
		}

		if m := runningTestsRe.FindStringSubmatch(line); m != nil {
			if current == nil {
				current = &cargoSuiteResult{}
			}
			fmt.Sscanf(m[1], "%d", &current.testCount)
			if current.name == "" {
				current.name = fmt.Sprintf("suite-%d", len(suites)+1)
			}
			capturingFailure = ""
			continue
		}

		if m := testResultRe.FindStringSubmatch(line); m != nil {
			if current == nil {
				continue
			}
			event := m[2]
			switch event {
			case "FAILED":
				event = "failed"
			case "ignored":
			default:
				event = "ok"
			}
			current.tests = append(current.tests, &cargoTestResult{name: m[1], event: event})
			continue
		}

		if m := failureStdoutHeaderRe.FindStringSubmatch(line); m != nil {
			capturingFailure = m[1]
			failureStdout[capturingFailure] = ""
			continue
		}

		if capturingFailure != "" {
			if line == "failures:" || testSummaryRe.MatchString(line) {
				capturingFailure = ""
				// fall through
			} else {
				if failureStdout[capturingFailure] != "" {
					failureStdout[capturingFailure] += "\n"
				}
				failureStdout[capturingFailure] += line
				continue
			}
		}

		if m := testSummaryRe.FindStringSubmatch(line); m != nil {
			if current == nil {
				continue
			}
			current.failed = m[1] == "FAILED"
			flush()
			continue
		}
	}
	flush()

	exitCode := 0
	var tops []ndjsoncrap.Test
	for _, suite := range suites {
		if len(suite.tests) == 0 {
			tops = append(tops, ndjsoncrap.Test{
				Description: suite.name,
				OK:          true,
				Directive:   skipDirective("no tests"),
			})
			continue
		}
		var subs []ndjsoncrap.Test
		for _, tr := range suite.tests {
			subs = append(subs, buildCargoTest(tr, len(subs)+1))
		}
		if suite.failed {
			exitCode = 1
		}
		tops = append(tops, ndjsoncrap.Test{
			Description: suite.name,
			OK:          !suite.failed,
			Subtest:     subs,
		})
	}

	if err := writeResultStream(w, "cargo test", "cargo-test", tops); err != nil {
		fmt.Fprintf(io.Discard, "%v", err)
	}
	return exitCode
}

func parseCargoBinaryLine(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "Running ") {
		rest := strings.TrimPrefix(line, "Running ")
		if idx := strings.Index(rest, " ("); idx > 0 {
			return rest[:idx]
		}
		return rest
	}
	if strings.HasPrefix(line, "Doc-tests ") {
		return line
	}
	return ""
}

func buildCargoTest(tr *cargoTestResult, n int) ndjsoncrap.Test {
	t := ndjsoncrap.Test{N: n, Description: tr.name}
	switch tr.event {
	case "failed":
		t.OK = false
		stdout := strings.TrimSpace(tr.stdout)
		t.Output = strPtr(stdout)
		if file, line := parseRustFileLine(stdout); file != "" {
			t.Diagnostic = map[string]any{"file": file, "line": line}
		}
	case "ignored":
		t.OK = true
		t.Directive = skipDirective("ignored")
	default:
		t.OK = true
	}
	return t
}
