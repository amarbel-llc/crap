# just-us Parity Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring go-crap/large-colon to feature parity with just-us's usage of rust-crap, using a two-phase testing loop: (1) make large-colon's `validate` accept just-us output, (2) make go-crap's writing methods produce output that passes its own `validate`.

**Architecture:** Add `NewBareWriter` (no version/pragma emission), `ExecTestResult` struct with `EmitTestResult()` method, and `PlanSkip()` to go-crap. Update Reader to handle YAML block literal scalars. Validate against real just-us output captured as golden files.

**Tech Stack:** Go, go-crap library, large-colon CLI, just-us (Rust reference impl)

**Design note — YAML value quoting:** rust-crap quotes single-line YAML values (`key: "value"`), while go-crap uses unquoted (`key: value`), consistent with go-crap's existing `NotOk()` method. This is an intentional divergence — both are valid YAML. The Reader accepts both forms.

**Design note — naming:** go-crap already has a `TestPoint` struct (used by `WriteAll`). The new just-us-style struct is named `ExecTestResult` and the method `EmitTestResult()` to avoid collision.

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `go-crap/crap.go` | Modify | Add `NewBareWriter`, `PlanSkip`, `ExecTestResult` struct, `EmitTestResult()` method |
| `go-crap/crap_test.go` | Modify | Tests for new writer methods |
| `go-crap/reader.go` | Modify | YAML block literal parsing |
| `go-crap/reader_test.go` | Modify | Validation tests against just-us golden output |
| `go-crap/parse.go` | Modify | (if needed) Parse `1::0 # SKIP reason` plan lines |

---

## Chunk 1: Phase 1 — Reader accepts just-us output

### Task 1: Capture just-us golden output

**Files:**
- Create: `go-crap/testdata/justus-basic.crap`
- Create: `go-crap/testdata/justus-with-output.crap`

- [ ] **Step 1: Create testdata directory if needed**

Run: `mkdir -p go-crap/testdata`

- [ ] **Step 2: Create minimal just-us golden file**

Create `go-crap/testdata/justus-basic.crap` with a minimal just-us-style CRAP-2 stream:

```
CRAP-2
1::3
ok 1 - build
ok 2 - test # runs the test suite
not ok 3 - lint
  ---
  message: clippy found 2 warnings
  severity: fail
  exitcode: 1
  ...
```

Note: just-us uses `# directive` (e.g. `# runs the test suite`) as a generic comment/doc annotation on passing test points — NOT a SKIP/TODO/WARN directive. The reader must accept these without error.

- [ ] **Step 3: Create just-us golden file with output and suppress_yaml**

Create `go-crap/testdata/justus-with-output.crap` with streamed output and block literal YAML:

```
CRAP-2
1::2
# build output line 1
# build output line 2
ok 1 - build
not ok 2 - test
  ---
  message: assertion failed
  severity: fail
  exitcode: 1
  output: |
    running tests...
    FAILED test_foo
  ...
```

- [ ] **Step 4: Commit golden files**

```bash
git add go-crap/testdata/justus-basic.crap go-crap/testdata/justus-with-output.crap
git commit -m "test: add just-us golden CRAP-2 test fixtures"
```

### Task 2: Reader accepts generic `# directive` comments on test points

just-us emits lines like `ok 2 - test # runs the test suite` where `# runs the test suite` is the recipe's doc comment — NOT a SKIP/TODO/WARN directive. The current `splitDirective` function only recognizes `# TODO`, `# SKIP`, and `# WARN`. An unknown `# comment` is left as part of the description, which is correct behavior. Verify this works and doesn't produce validation errors.

**Files:**
- Modify: `go-crap/reader_test.go`

- [ ] **Step 1: Write test for generic directive comment parsing**

```go
func TestReaderGenericDirectiveComment(t *testing.T) {
	input := "CRAP-2\nok 1 - test # runs the test suite\n1::1\n"
	reader := NewReader(strings.NewReader(input))
	summary := reader.Summary()
	if !summary.Valid {
		t.Errorf("expected valid stream, got invalid")
	}
	if summary.Passed != 1 {
		t.Errorf("expected 1 passed, got %d", summary.Passed)
	}
}
```

- [ ] **Step 2: Run test to verify behavior**

Run: `cd go-crap && go test -run TestReaderGenericDirectiveComment -v`

If the test passes, the reader already handles this correctly. If it fails, investigate `splitDirective`.

- [ ] **Step 3: Commit**

```bash
git add go-crap/reader_test.go
git commit -m "test: verify reader accepts generic directive comments"
```

### Task 3: Reader accepts plan-skip lines (`1::0 # SKIP reason`)

rust-crap's `plan_skip` emits `1::0 # SKIP reason`. The current plan regex `^1::([\d,.\x{00a0}\x{202f} ]+)(\s+#\s+(.*))?$` should already match this since `0` is a digit. Verify and add tests.

**Files:**
- Modify: `go-crap/reader_test.go`
- Modify: `go-crap/parse.go` (if regex needs updating)

- [ ] **Step 1: Write test for plan-skip parsing**

```go
func TestReaderPlanSkip(t *testing.T) {
	input := "CRAP-2\n1::0 # SKIP no tests to run\n"
	reader := NewReader(strings.NewReader(input))
	summary := reader.Summary()
	if !summary.Valid {
		t.Errorf("expected valid stream, got invalid; diags: %v", reader.Diagnostics())
	}
	if summary.TotalTests != 0 {
		t.Errorf("expected 0 tests, got %d", summary.TotalTests)
	}
	if summary.PlanCount != 0 {
		t.Errorf("expected plan count 0, got %d", summary.PlanCount)
	}
}
```

- [ ] **Step 2: Run test**

Run: `cd go-crap && go test -run TestReaderPlanSkip -v`

- [ ] **Step 3: Fix if needed**

If the plan regex doesn't match, update it. The `# SKIP reason` part is a generic comment in group 3 — no special handling needed.

- [ ] **Step 4: Commit**

```bash
git add go-crap/reader_test.go go-crap/parse.go
git commit -m "test: verify reader accepts plan-skip lines"
```

### Task 4: Golden file validation tests

**Files:**
- Modify: `go-crap/reader_test.go`

- [ ] **Step 1: Write golden file validation tests**

```go
func TestReaderJustUsGoldenBasic(t *testing.T) {
	f, err := os.Open("testdata/justus-basic.crap")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	reader := NewReader(f)
	summary := reader.Summary()
	diags := reader.Diagnostics()

	var errors []Diagnostic
	for _, d := range diags {
		if d.Severity == SeverityError {
			errors = append(errors, d)
		}
	}

	if len(errors) > 0 {
		for _, d := range errors {
			t.Errorf("line %d: %s: [%s] %s", d.Line, d.Severity, d.Rule, d.Message)
		}
	}

	if summary.TotalTests != 3 {
		t.Errorf("expected 3 total tests, got %d", summary.TotalTests)
	}
	if summary.Passed != 2 {
		t.Errorf("expected 2 passed, got %d", summary.Passed)
	}
	if summary.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", summary.Failed)
	}
}

func TestReaderJustUsGoldenWithOutput(t *testing.T) {
	f, err := os.Open("testdata/justus-with-output.crap")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	reader := NewReader(f)
	summary := reader.Summary()
	diags := reader.Diagnostics()

	var errors []Diagnostic
	for _, d := range diags {
		if d.Severity == SeverityError {
			errors = append(errors, d)
		}
	}

	if len(errors) > 0 {
		for _, d := range errors {
			t.Errorf("line %d: %s: [%s] %s", d.Line, d.Severity, d.Rule, d.Message)
		}
	}

	if !summary.Valid {
		t.Errorf("expected valid stream")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd go-crap && go test -run TestReaderJustUsGolden -v`

- [ ] **Step 3: Fix any validation errors in the Reader**

The `justus-with-output.crap` golden file will likely fail because the Reader doesn't handle YAML block literal scalars (`output: |`). This is addressed in Task 5.

- [ ] **Step 4: Commit**

```bash
git add go-crap/reader_test.go
git commit -m "test: golden file validation for just-us CRAP-2 output"
```

### Task 5: YAML block literal support in Reader

just-us/rust-crap emits YAML block literal scalars (`output: |`) for multi-line values. The current reader only parses simple `key: value` pairs. It needs to handle `key: |` followed by indented continuation lines.

**Files:**
- Modify: `go-crap/reader.go`
- Modify: `go-crap/reader_test.go`

- [ ] **Step 1: Write failing test for YAML block literal**

```go
func TestReaderYAMLBlockLiteral(t *testing.T) {
	input := `CRAP-2
not ok 1 - test
  ---
  message: |
    line one
    line two
  severity: fail
  ...
1::1
`
	reader := NewReader(strings.NewReader(input))
	var yamlEvent Event
	for {
		ev, err := reader.Next()
		if err != nil {
			break
		}
		if ev.Type == EventYAMLDiagnostic {
			yamlEvent = ev
		}
	}

	if yamlEvent.YAML == nil {
		t.Fatal("expected YAML event")
	}
	if yamlEvent.YAML["message"] != "line one\nline two" {
		t.Errorf("expected multi-line message, got %q", yamlEvent.YAML["message"])
	}
	if yamlEvent.YAML["severity"] != "fail" {
		t.Errorf("expected severity=fail, got %q", yamlEvent.YAML["severity"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-crap && go test -run TestReaderYAMLBlockLiteral -v`
Expected: FAIL — current YAML parser doesn't handle block literal `|`

- [ ] **Step 3: Implement block literal parsing in Reader**

Add fields to `Reader` struct in `reader.go`:

```go
yamlBlockKey    string  // current block literal key, empty = not in block
yamlBlockIndent int     // expected indent for block literal continuation lines
```

In the `stateYAML` handler, replace the simple key:value parsing with logic that:
1. Detects `key: |` and enters block literal mode
2. Accumulates indented continuation lines into the value
3. Exits block literal mode when a line at YAML base indent is reached, then processes that line as a new key

```go
// Before the YAML end marker check, handle block literal continuation:
if r.yamlBlockKey != "" {
    yamlBaseIndent := (r.currentFrame().depth * 4) + 2
    lineIndent := len(raw) - len(strings.TrimLeft(raw, " "))
    if lineIndent <= yamlBaseIndent && strings.TrimSpace(raw) != "" {
        // Not a continuation — end block, fall through to normal parsing
        r.yamlBlockKey = ""
    } else {
        // Continuation line
        stripped := raw
        if len(stripped) >= r.yamlBlockIndent {
            stripped = stripped[r.yamlBlockIndent:]
        }
        if r.yamlBuf[r.yamlBlockKey] != "" {
            r.yamlBuf[r.yamlBlockKey] += "\n"
        }
        r.yamlBuf[r.yamlBlockKey] += strings.TrimRight(stripped, " ")
        continue
    }
}

// Normal key:value parsing (using original line for ANSI preservation)
parts := strings.SplitN(content, ":", 2)
if len(parts) == 2 {
    key := strings.TrimSpace(parts[0])
    val := strings.TrimSpace(parts[1])
    if val == "|" {
        r.yamlBlockKey = key
        r.yamlBlockIndent = (r.currentFrame().depth * 4) + 4
        r.yamlBuf[key] = ""
    } else {
        r.yamlBuf[key] = val
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-crap && go test -run TestReaderYAMLBlockLiteral -v`
Expected: PASS

- [ ] **Step 5: Run all tests including golden files**

Run: `cd go-crap && go test ./...`
Expected: All pass (including `TestReaderJustUsGoldenWithOutput`)

- [ ] **Step 6: Commit**

```bash
git add go-crap/reader.go go-crap/reader_test.go
git commit -m "feat: reader supports YAML block literal scalars"
```

## Chunk 2: Phase 2 — Writer parity with rust-crap

### Task 6: Add `NewBareWriter` (no version/pragma emission)

rust-crap's `build_without_printing()` creates a writer that doesn't emit the version line or pragma lines. just-us uses this for per-test-point emission (one writer per recipe, after the initial plan is emitted by a normal writer).

**Files:**
- Modify: `go-crap/crap.go`
- Modify: `go-crap/crap_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestNewBareWriter(t *testing.T) {
	var buf bytes.Buffer
	tw := NewBareWriter(&buf)
	tw.Ok("hello")
	tw.Plan()

	got := buf.String()
	want := "ok 1 - hello\n1::1\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-crap && go test -run TestNewBareWriter -v`
Expected: FAIL — `NewBareWriter` doesn't exist yet

- [ ] **Step 3: Implement `NewBareWriter`**

Add to `crap.go`:

```go
// NewBareWriter creates a Writer that does not emit the CRAP-2 version line
// or any pragma lines. Use this for incremental emission (e.g. one test point
// at a time) after a normal Writer has already emitted the header and plan.
func NewBareWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// NewBareColorWriter creates a bare Writer with ANSI color support.
func NewBareColorWriter(w io.Writer, color bool) *Writer {
	return &Writer{w: w, color: color}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-crap && go test -run TestNewBareWriter -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go-crap/crap.go go-crap/crap_test.go
git commit -m "feat: add NewBareWriter for per-test-point emission"
```

### Task 7: Add `ExecTestResult` struct and `EmitTestResult()` method

Mirror rust-crap's `TestResult` struct and `test_point()` method. Named `ExecTestResult` and `EmitTestResult()` to avoid collision with the existing `TestPoint` struct used by `WriteAll()`.

**Files:**
- Modify: `go-crap/crap.go`
- Modify: `go-crap/crap_test.go`

- [ ] **Step 1: Write failing test for EmitTestResult with YAML**

```go
func TestWriterEmitTestResultFail(t *testing.T) {
	var buf bytes.Buffer
	tw := NewBareWriter(&buf)
	tw.EmitTestResult(&ExecTestResult{
		Name:         "build",
		OK:           false,
		ErrorMessage: "exit code 1",
		ExitCode:     intPtr(1),
	})
	tw.Plan()

	got := buf.String()
	if !strings.Contains(got, "not ok 1 - build") {
		t.Errorf("missing test point line in:\n%s", got)
	}
	if !strings.Contains(got, "  ---") {
		t.Errorf("missing YAML start in:\n%s", got)
	}
	if !strings.Contains(got, "  message: exit code 1") {
		t.Errorf("missing message in:\n%s", got)
	}
	if !strings.Contains(got, "  severity: fail") {
		t.Errorf("missing severity in:\n%s", got)
	}
	if !strings.Contains(got, "  exitcode: 1") {
		t.Errorf("missing exitcode in:\n%s", got)
	}
	if !strings.Contains(got, "  ...") {
		t.Errorf("missing YAML end in:\n%s", got)
	}
}

func TestWriterEmitTestResultBareFail(t *testing.T) {
	// A bare "not ok" with no message/exitcode/output still gets YAML with severity
	var buf bytes.Buffer
	tw := NewBareWriter(&buf)
	tw.EmitTestResult(&ExecTestResult{
		Name: "bare-fail",
		OK:   false,
	})
	tw.Plan()

	got := buf.String()
	if !strings.Contains(got, "  severity: fail") {
		t.Errorf("bare failing test should still get severity YAML:\n%s", got)
	}
}

func intPtr(n int) *int { return &n }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-crap && go test -run TestWriterEmitTestResult -v`
Expected: FAIL — `ExecTestResult` struct and `EmitTestResult()` don't exist

- [ ] **Step 3: Implement `ExecTestResult` and `EmitTestResult()`**

Add to `crap.go`:

```go
// ExecTestResult represents the outcome of a single command/recipe execution.
// Mirrors rust-crap's TestResult struct used by just-us.
type ExecTestResult struct {
	Number       int    // explicit test number; 0 means auto-increment
	Name         string
	OK           bool
	Directive    string // generic comment (e.g. recipe doc), not a SKIP/TODO/WARN directive
	ErrorMessage string
	ExitCode     *int
	Output       string
	SuppressYAML bool
}

// EmitTestResult writes a test point with optional YAML diagnostics.
// Mirrors rust-crap's test_point() method.
func (tw *Writer) EmitTestResult(r *ExecTestResult) int {
	tw.clearStatusIfActive()
	tw.n++
	if !r.OK {
		tw.failed = true
	}

	status := tw.colorOk()
	if !r.OK {
		status = tw.colorNotOk()
	}

	// Use explicit number for display if provided, but always increment internal counter.
	// This matches rust-crap's behavior where counter += 1 always, but result.number is
	// used for formatting.
	displayNum := tw.n
	if r.Number > 0 {
		displayNum = r.Number
	}
	num := tw.formatNumber(displayNum)

	if r.Directive != "" {
		fmt.Fprintf(tw.w, "%s %s - %s # %s\n", status, num, r.Name, r.Directive)
	} else {
		fmt.Fprintf(tw.w, "%s %s - %s\n", status, num, r.Name)
	}

	if !r.SuppressYAML && hasExecYAMLContent(r) {
		fmt.Fprintln(tw.w, "  ---")
		if r.ErrorMessage != "" {
			writeExecYAMLField(tw.w, "message", sanitizeYAMLValue(r.ErrorMessage, tw.color))
		}
		if !r.OK {
			fmt.Fprintln(tw.w, "  severity: fail")
		}
		if r.ExitCode != nil {
			fmt.Fprintf(tw.w, "  exitcode: %d\n", *r.ExitCode)
		}
		if r.Output != "" {
			writeExecYAMLField(tw.w, "output", sanitizeYAMLValue(r.Output, tw.color))
		}
		fmt.Fprintln(tw.w, "  ...")
	}

	return tw.n
}

// hasExecYAMLContent returns true if the result has any content worth emitting
// as a YAML diagnostic block. Matches rust-crap's has_yaml_block: any failing
// test gets at least severity: fail, plus any explicit message/exitcode/output.
func hasExecYAMLContent(r *ExecTestResult) bool {
	return !r.OK || r.ErrorMessage != "" || r.ExitCode != nil || r.Output != ""
}

func writeExecYAMLField(w io.Writer, key, value string) {
	if strings.Contains(value, "\n") {
		fmt.Fprintf(w, "  %s: |\n", key)
		lines := strings.Split(value, "\n")
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		for _, line := range lines {
			fmt.Fprintf(w, "    %s\n", line)
		}
	} else {
		fmt.Fprintf(w, "  %s: %s\n", key, value)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-crap && go test -run TestWriterEmitTestResult -v`
Expected: PASS

- [ ] **Step 5: Write test for EmitTestResult with SuppressYAML**

```go
func TestWriterEmitTestResultSuppressYAML(t *testing.T) {
	var buf bytes.Buffer
	tw := NewBareWriter(&buf)
	tw.EmitTestResult(&ExecTestResult{
		Name:         "build",
		OK:           false,
		ErrorMessage: "exit code 1",
		ExitCode:     intPtr(1),
		SuppressYAML: true,
	})
	tw.Plan()

	got := buf.String()
	if strings.Contains(got, "---") {
		t.Errorf("YAML block should be suppressed:\n%s", got)
	}
}
```

- [ ] **Step 6: Run test**

Run: `cd go-crap && go test -run TestWriterEmitTestResultSuppressYAML -v`
Expected: PASS

- [ ] **Step 7: Write test for EmitTestResult with directive**

```go
func TestWriterEmitTestResultDirective(t *testing.T) {
	var buf bytes.Buffer
	tw := NewBareWriter(&buf)
	tw.EmitTestResult(&ExecTestResult{
		Name:      "build",
		OK:        true,
		Directive: "compiles the project",
	})
	tw.Plan()

	got := buf.String()
	want := "ok 1 - build # compiles the project\n1::1\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
```

- [ ] **Step 8: Run test**

Run: `cd go-crap && go test -run TestWriterEmitTestResultDirective -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add go-crap/crap.go go-crap/crap_test.go
git commit -m "feat: add ExecTestResult struct and EmitTestResult method"
```

### Task 8: Add `PlanSkip()` method

Mirror rust-crap's `plan_skip(reason)` which emits `1::0 # SKIP reason`.

**Files:**
- Modify: `go-crap/crap.go`
- Modify: `go-crap/crap_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestWriterPlanSkip(t *testing.T) {
	var buf bytes.Buffer
	tw := NewBareWriter(&buf)
	tw.PlanSkip("no tests to run")

	got := buf.String()
	want := "1::0 # SKIP no tests to run\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-crap && go test -run TestWriterPlanSkip -v`
Expected: FAIL

- [ ] **Step 3: Implement `PlanSkip`**

Add to `crap.go`:

```go
func (tw *Writer) PlanSkip(reason string) {
	tw.clearStatusIfActive()
	tw.planEmitted = true
	fmt.Fprintf(tw.w, "1::0 # SKIP %s\n", reason)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-crap && go test -run TestWriterPlanSkip -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go-crap/crap.go go-crap/crap_test.go
git commit -m "feat: add PlanSkip method for empty test suites"
```

### Task 9: Round-trip validation — Writer output passes Reader

The key integration test: write CRAP-2 output using the new methods, then validate it with the Reader.

**Files:**
- Modify: `go-crap/crap_test.go`

- [ ] **Step 1: Write round-trip test**

```go
func TestRoundTripWriterReader(t *testing.T) {
	var buf bytes.Buffer
	tw := NewWriter(&buf)

	tw.EmitTestResult(&ExecTestResult{Name: "pass", OK: true})
	tw.EmitTestResult(&ExecTestResult{
		Name:         "fail",
		OK:           false,
		ErrorMessage: "assertion failed",
		ExitCode:     intPtr(1),
		Output:       "some output\nmore output",
	})
	tw.EmitTestResult(&ExecTestResult{
		Name:         "quiet-fail",
		OK:           false,
		ErrorMessage: "silent",
		SuppressYAML: true,
	})
	tw.EmitTestResult(&ExecTestResult{
		Name:      "documented",
		OK:        true,
		Directive: "builds the project",
	})
	tw.Plan()

	reader := NewReader(strings.NewReader(buf.String()))
	summary := reader.Summary()
	diags := reader.Diagnostics()

	var errors []Diagnostic
	for _, d := range diags {
		if d.Severity == SeverityError {
			errors = append(errors, d)
		}
	}

	if len(errors) > 0 {
		t.Errorf("round-trip produced validation errors:")
		for _, d := range errors {
			t.Errorf("  line %d: [%s] %s", d.Line, d.Rule, d.Message)
		}
		t.Logf("output was:\n%s", buf.String())
	}

	if summary.TotalTests != 4 {
		t.Errorf("expected 4 tests, got %d", summary.TotalTests)
	}
	if summary.Passed != 2 {
		t.Errorf("expected 2 passed, got %d", summary.Passed)
	}
	if summary.Failed != 2 {
		t.Errorf("expected 2 failed, got %d", summary.Failed)
	}
}
```

- [ ] **Step 2: Run test**

Run: `cd go-crap && go test -run TestRoundTripWriterReader -v`

- [ ] **Step 3: Fix any issues**

If the round-trip test fails, the Reader is rejecting output from the Writer. Fix either the Writer's output format or the Reader's parsing to match the CRAP-2 spec.

- [ ] **Step 4: Write round-trip test for PlanSkip**

```go
func TestRoundTripPlanSkip(t *testing.T) {
	var buf bytes.Buffer
	tw := NewWriter(&buf)
	tw.PlanSkip("nothing to do")

	reader := NewReader(strings.NewReader(buf.String()))
	summary := reader.Summary()

	if !summary.Valid {
		t.Errorf("plan-skip output not valid: %v", reader.Diagnostics())
		t.Logf("output was:\n%s", buf.String())
	}
	if summary.PlanCount != 0 {
		t.Errorf("expected plan count 0, got %d", summary.PlanCount)
	}
}
```

- [ ] **Step 5: Run test**

Run: `cd go-crap && go test -run TestRoundTripPlanSkip -v`

- [ ] **Step 6: Write round-trip test with BareWriter (just-us pattern)**

This simulates just-us's pattern: a normal writer emits version + plan-ahead,
then per-recipe bare writers emit individual test points with explicit numbers.

```go
func TestRoundTripBareWriter(t *testing.T) {
	var buf bytes.Buffer

	tw := NewWriter(&buf)
	tw.PlanAhead(2)

	// just-us creates a new bare writer per recipe, setting Number explicitly
	bare1 := NewBareWriter(&buf)
	bare1.EmitTestResult(&ExecTestResult{Number: 1, Name: "a", OK: true})

	bare2 := NewBareWriter(&buf)
	bare2.EmitTestResult(&ExecTestResult{Number: 2, Name: "b", OK: true, Directive: "doc comment"})

	reader := NewReader(strings.NewReader(buf.String()))
	summary := reader.Summary()

	if !summary.Valid {
		t.Errorf("bare writer round-trip not valid: %v", reader.Diagnostics())
		t.Logf("output was:\n%s", buf.String())
	}
	if summary.TotalTests != 2 {
		t.Errorf("expected 2 tests, got %d", summary.TotalTests)
	}
}
```

- [ ] **Step 7: Run test**

Run: `cd go-crap && go test -run TestRoundTripBareWriter -v`

- [ ] **Step 8: Run all tests**

Run: `cd go-crap && go test ./...`
Expected: All pass

- [ ] **Step 9: Commit**

```bash
git add go-crap/crap.go go-crap/crap_test.go
git commit -m "feat: round-trip validation tests for writer-reader parity"
```

### Task 10: Full test suite verification

- [ ] **Step 1: Run all Go tests**

Run: `cd go-crap && go test ./... -v`
Expected: All pass

- [ ] **Step 2: Run all Rust tests**

Run: `cd rust-crap && cargo test`
Expected: All pass (no changes to rust-crap in this plan)

- [ ] **Step 3: Run full build**

Run: `just build`
Expected: Success

- [ ] **Step 4: Commit any remaining fixes**

If any tests needed fixes, commit them.
