package crap

import (
	"bytes"
	"strings"
	"testing"
)

func TestCargoConvertSingleSuiteAllPass(t *testing.T) {
	prettyOutput := strings.Join([]string{
		"running 2 tests",
		"test tests::test_a ... ok",
		"test tests::test_b ... ok",
		"",
		"test result: ok. 2 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s",
	}, "\n") + "\n"

	var buf bytes.Buffer
	exitCode := ConvertCargoTest(strings.NewReader(prettyOutput), &buf, false, false, false)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	out := buf.String()

	// Should produce valid CRAP-2
	reader := NewReader(strings.NewReader(out))
	summary := reader.Summary()
	if !summary.Valid {
		for _, d := range reader.Diagnostics() {
			t.Errorf("diagnostic: line %d: %s: %s", d.Line, d.Severity, d.Message)
		}
		t.Fatalf("output is not valid CRAP-2:\n%s", out)
	}

	// Should have a subtest for the suite
	if !strings.Contains(out, "# Subtest:") {
		t.Errorf("expected suite subtest:\n%s", out)
	}
	if !strings.Contains(out, "tests::test_a") {
		t.Errorf("expected test_a in output:\n%s", out)
	}
	if !strings.Contains(out, "tests::test_b") {
		t.Errorf("expected test_b in output:\n%s", out)
	}
}

func TestCargoConvertFailingTest(t *testing.T) {
	prettyOutput := strings.Join([]string{
		"running 1 test",
		"test tests::test_bad ... FAILED",
		"",
		"failures:",
		"",
		"---- tests::test_bad stdout ----",
		"thread 'tests::test_bad' panicked at src/lib.rs:10:5:",
		"assertion `left == right` failed",
		"  left: 1",
		" right: 2",
		"",
		"",
		"failures:",
		"    tests::test_bad",
		"",
		"test result: FAILED. 0 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s",
	}, "\n") + "\n"

	var buf bytes.Buffer
	exitCode := ConvertCargoTest(strings.NewReader(prettyOutput), &buf, false, false, false)

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	out := buf.String()
	if !strings.Contains(out, "not ok") {
		t.Errorf("expected not ok in output:\n%s", out)
	}
	if !strings.Contains(out, "lib.rs") {
		t.Errorf("expected file reference in diagnostics:\n%s", out)
	}

	reader := NewReader(strings.NewReader(out))
	if !reader.Summary().Valid {
		t.Errorf("output is not valid CRAP-2:\n%s", out)
	}
}

func TestCargoConvertIgnoredTest(t *testing.T) {
	prettyOutput := strings.Join([]string{
		"running 2 tests",
		"test tests::test_ok ... ok",
		"test tests::test_ignored ... ignored",
		"",
		"test result: ok. 1 passed; 0 failed; 1 ignored; 0 measured; 0 filtered out; finished in 0.00s",
	}, "\n") + "\n"

	var buf bytes.Buffer
	exitCode := ConvertCargoTest(strings.NewReader(prettyOutput), &buf, false, false, false)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	out := buf.String()
	if !strings.Contains(out, "# SKIP") {
		t.Errorf("expected SKIP directive for ignored test:\n%s", out)
	}

	reader := NewReader(strings.NewReader(out))
	if !reader.Summary().Valid {
		t.Errorf("output is not valid CRAP-2:\n%s", out)
	}
}

func TestCargoConvertEmitsPragmaAndStreamedOutput(t *testing.T) {
	prettyOutput := strings.Join([]string{
		"running 1 test",
		"test tests::test_bad ... FAILED",
		"",
		"failures:",
		"",
		"---- tests::test_bad stdout ----",
		"thread 'tests::test_bad' panicked at src/lib.rs:10:5:",
		"assertion `left == right` failed",
		"  left: 1",
		" right: 2",
		"",
		"",
		"failures:",
		"    tests::test_bad",
		"",
		"test result: FAILED. 0 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s",
	}, "\n") + "\n"

	var buf bytes.Buffer
	ConvertCargoTest(strings.NewReader(prettyOutput), &buf, false, false, false)
	out := buf.String()

	// streamed-output is enabled by default, should NOT be explicitly emitted
	if strings.Contains(out, "pragma +streamed-output") {
		t.Errorf("streamed-output is default, should NOT emit pragma, got:\n%s", out)
	}

	// Output block header should appear before not ok
	headerIdx := strings.Index(out, "# Output:")
	notOkIdx := strings.Index(out, "not ok 1 - tests::test_bad")
	if headerIdx < 0 {
		t.Fatalf("expected Output Block header in output:\n%s", out)
	}
	if headerIdx > notOkIdx {
		t.Error("Output Block header should appear before not ok")
	}

	// Body lines should be 4-space indented
	if !strings.Contains(out, "    thread 'tests::test_bad' panicked") {
		t.Errorf("expected 4-space indented output body line in output:\n%s", out)
	}

	reader := NewReader(strings.NewReader(out))
	if !reader.Summary().Valid {
		for _, d := range reader.Diagnostics() {
			t.Errorf("diagnostic: line %d: %s: %s", d.Line, d.Severity, d.Message)
		}
		t.Fatalf("output is not valid CRAP-2:\n%s", out)
	}
}

func TestCargoConvertMultipleSuites(t *testing.T) {
	prettyOutput := strings.Join([]string{
		"     Running unittests src/lib.rs (target/debug/deps/my_crate-abc123)",
		"",
		"running 1 test",
		"test tests::test_lib ... ok",
		"",
		"test result: ok. 1 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s",
		"",
		"     Running tests/integration.rs (target/debug/deps/integration-def456)",
		"",
		"running 1 test",
		"test test_integration ... ok",
		"",
		"test result: ok. 1 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s",
	}, "\n") + "\n"

	var buf bytes.Buffer
	exitCode := ConvertCargoTest(strings.NewReader(prettyOutput), &buf, false, false, false)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	out := buf.String()
	if !strings.Contains(out, "# Subtest: unittests src/lib.rs") {
		t.Errorf("expected lib suite subtest:\n%s", out)
	}
	if !strings.Contains(out, "# Subtest: tests/integration.rs") {
		t.Errorf("expected integration suite subtest:\n%s", out)
	}
	if !strings.Contains(out, "1::2") {
		t.Errorf("expected plan 1::2:\n%s", out)
	}

	reader := NewReader(strings.NewReader(out))
	if !reader.Summary().Valid {
		for _, d := range reader.Diagnostics() {
			t.Errorf("diagnostic: line %d: %s: %s", d.Line, d.Severity, d.Message)
		}
		t.Fatalf("output is not valid CRAP-2:\n%s", out)
	}
}

func TestCargoConvertEmptySuiteDefault(t *testing.T) {
	prettyOutput := strings.Join([]string{
		"     Running unittests src/lib.rs (target/debug/deps/my_crate-abc123)",
		"",
		"running 0 tests",
		"",
		"test result: ok. 0 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s",
	}, "\n") + "\n"

	var buf bytes.Buffer
	exitCode := ConvertCargoTest(strings.NewReader(prettyOutput), &buf, false, false, false)

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	out := buf.String()
	if !strings.Contains(out, "not ok") {
		t.Errorf("expected not ok for empty suite:\n%s", out)
	}

	reader := NewReader(strings.NewReader(out))
	if !reader.Summary().Valid {
		for _, d := range reader.Diagnostics() {
			t.Errorf("diagnostic: line %d: %s: %s", d.Line, d.Severity, d.Message)
		}
		t.Fatalf("output is not valid CRAP-2:\n%s", out)
	}
}

func TestCargoConvertEmptySuiteSkipEmpty(t *testing.T) {
	prettyOutput := strings.Join([]string{
		"     Running unittests src/lib.rs (target/debug/deps/my_crate-abc123)",
		"",
		"running 0 tests",
		"",
		"test result: ok. 0 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s",
	}, "\n") + "\n"

	var buf bytes.Buffer
	exitCode := ConvertCargoTest(strings.NewReader(prettyOutput), &buf, false, true, false)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	out := buf.String()
	if !strings.Contains(out, "# SKIP") {
		t.Errorf("expected SKIP directive:\n%s", out)
	}
	if strings.Contains(out, "not ok") {
		t.Errorf("expected no 'not ok' with skip-empty:\n%s", out)
	}

	reader := NewReader(strings.NewReader(out))
	if !reader.Summary().Valid {
		for _, d := range reader.Diagnostics() {
			t.Errorf("diagnostic: line %d: %s: %s", d.Line, d.Severity, d.Message)
		}
		t.Fatalf("output is not valid CRAP-2:\n%s", out)
	}
}

func TestCargoConvertMixedEmptyAndRealSuites(t *testing.T) {
	prettyOutput := strings.Join([]string{
		"     Running unittests src/lib.rs (target/debug/deps/my_crate-abc123)",
		"",
		"running 1 test",
		"test tests::test_real ... ok",
		"",
		"test result: ok. 1 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s",
		"",
		"     Running unittests src/main.rs (target/debug/deps/my_crate-def456)",
		"",
		"running 0 tests",
		"",
		"test result: ok. 0 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s",
	}, "\n") + "\n"

	var buf bytes.Buffer
	exitCode := ConvertCargoTest(strings.NewReader(prettyOutput), &buf, false, true, false)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	out := buf.String()
	if !strings.Contains(out, "# Subtest: unittests src/lib.rs") {
		t.Errorf("expected real suite subtest:\n%s", out)
	}
	if !strings.Contains(out, "# SKIP") {
		t.Errorf("expected SKIP for empty suite:\n%s", out)
	}
	if !strings.Contains(out, "1::2") {
		t.Errorf("expected plan 1::2:\n%s", out)
	}

	reader := NewReader(strings.NewReader(out))
	if !reader.Summary().Valid {
		for _, d := range reader.Diagnostics() {
			t.Errorf("diagnostic: line %d: %s: %s", d.Line, d.Severity, d.Message)
		}
		t.Fatalf("output is not valid CRAP-2:\n%s", out)
	}
}
