package crap

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func collect(ch <-chan ExecResult) []ExecResult {
	var results []ExecResult
	for r := range ch {
		results = append(results, r)
	}
	return results
}

func chanFromSlice(results []ExecResult) <-chan ExecResult {
	ch := make(chan ExecResult, len(results))
	for _, r := range results {
		ch <- r
	}
	close(ch)
	return ch
}

func TestGoroutineExecutorAllSucceed(t *testing.T) {
	executor := &GoroutineExecutor{}
	results := collect(executor.Run(context.Background(), "echo {}", []string{"hello", "world"}))

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for i, r := range results {
		if r.ExitCode != 0 {
			t.Errorf("result %d: expected exit code 0, got %d", i, r.ExitCode)
		}
	}

	if string(results[0].Stdout) != "hello\n" {
		t.Errorf("result 0 stdout: expected %q, got %q", "hello\n", string(results[0].Stdout))
	}
	if string(results[1].Stdout) != "world\n" {
		t.Errorf("result 1 stdout: expected %q, got %q", "world\n", string(results[1].Stdout))
	}
}

func TestGoroutineExecutorPreservesOrder(t *testing.T) {
	executor := &GoroutineExecutor{}
	results := collect(executor.Run(context.Background(), "echo {}", []string{"a", "b", "c", "d", "e"}))

	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	for i, expected := range []string{"a", "b", "c", "d", "e"} {
		if results[i].Arg != expected {
			t.Errorf("result %d: expected arg %q, got %q", i, expected, results[i].Arg)
		}
	}
}

func TestGoroutineExecutorFailingCommand(t *testing.T) {
	executor := &GoroutineExecutor{}
	results := collect(executor.Run(context.Background(), "exit 1", []string{"x"}))

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", results[0].ExitCode)
	}
}

func TestGoroutineExecutorCapturesStderr(t *testing.T) {
	executor := &GoroutineExecutor{}
	results := collect(executor.Run(context.Background(), "echo err >&2", []string{"x"}))

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if string(results[0].Stderr) != "err\n" {
		t.Errorf("stderr: expected %q, got %q", "err\n", string(results[0].Stderr))
	}
}

func TestGoroutineExecutorSubstitution(t *testing.T) {
	executor := &GoroutineExecutor{}
	results := collect(executor.Run(context.Background(), "echo prefix-{}-suffix", []string{"mid"}))

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if string(results[0].Stdout) != "prefix-mid-suffix\n" {
		t.Errorf("stdout: expected %q, got %q", "prefix-mid-suffix\n", string(results[0].Stdout))
	}

	if results[0].Command != "echo prefix-mid-suffix" {
		t.Errorf("command: expected %q, got %q", "echo prefix-mid-suffix", results[0].Command)
	}
}

func TestConvertExecParallelAllPass(t *testing.T) {
	results := chanFromSlice([]ExecResult{
		{Arg: "a", Command: "echo a", ExitCode: 0, Stdout: []byte("a\n")},
		{Arg: "b", Command: "echo b", ExitCode: 0, Stdout: []byte("b\n")},
	})

	var buf bytes.Buffer
	exitCode := ConvertExecParallel(results, &buf, false, false)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	out := buf.String()
	reader := NewReader(strings.NewReader(out))
	summary := reader.Summary()
	if !summary.Valid {
		for _, d := range reader.Diagnostics() {
			t.Errorf("diagnostic: line %d: %s: %s", d.Line, d.Severity, d.Message)
		}
		t.Fatalf("output is not valid CRAP-2:\n%s", out)
	}

	if summary.Passed != 2 {
		t.Errorf("expected 2 passed, got %d", summary.Passed)
	}
}

func TestConvertExecParallelWithFailure(t *testing.T) {
	results := chanFromSlice([]ExecResult{
		{Arg: "a", Command: "echo a", ExitCode: 0, Stdout: []byte("a\n")},
		{Arg: "b", Command: "false", ExitCode: 1, Stdout: []byte(""), Stderr: []byte("something broke\n")},
	})

	var buf bytes.Buffer
	exitCode := ConvertExecParallel(results, &buf, false, false)

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	out := buf.String()

	if !strings.Contains(out, "ok 1 - echo a") {
		t.Errorf("expected ok for first command, got:\n%s", out)
	}
	if !strings.Contains(out, "not ok 2 - false") {
		t.Errorf("expected not ok for second command, got:\n%s", out)
	}
	if !strings.Contains(out, "exit-code: 1") {
		t.Errorf("expected exit-code diagnostic, got:\n%s", out)
	}
	if !strings.Contains(out, "something broke") {
		t.Errorf("expected stderr in diagnostics, got:\n%s", out)
	}
}

func TestConvertExecParallelNoDiagOnSuccess(t *testing.T) {
	results := chanFromSlice([]ExecResult{
		{Arg: "a", Command: "echo a", ExitCode: 0, Stdout: []byte("output\n")},
	})

	var buf bytes.Buffer
	ConvertExecParallel(results, &buf, false, false)

	out := buf.String()
	if strings.Contains(out, "---") {
		t.Errorf("verbose=false should not include diagnostics on success, got:\n%s", out)
	}
}

func TestConvertExecParallelVerboseIncludesDiagOnSuccess(t *testing.T) {
	results := chanFromSlice([]ExecResult{
		{Arg: "a", Command: "echo a", ExitCode: 0, Stdout: []byte("output\n")},
	})

	var buf bytes.Buffer
	ConvertExecParallel(results, &buf, true, false)

	out := buf.String()
	if !strings.Contains(out, "---") {
		t.Errorf("verbose=true should include diagnostics on success, got:\n%s", out)
	}
	if !strings.Contains(out, "output") {
		t.Errorf("verbose=true should include stdout, got:\n%s", out)
	}
}

func TestGoroutineExecutorMaxJobsOne(t *testing.T) {
	executor := &GoroutineExecutor{MaxJobs: 1}
	results := collect(executor.Run(context.Background(), "echo {}", []string{"a", "b", "c"}))

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, expected := range []string{"a", "b", "c"} {
		if results[i].Arg != expected {
			t.Errorf("result %d: expected arg %q, got %q", i, expected, results[i].Arg)
		}
		if results[i].ExitCode != 0 {
			t.Errorf("result %d: expected exit code 0, got %d", i, results[i].ExitCode)
		}
	}
}

func TestGoroutineExecutorMaxJobsTwo(t *testing.T) {
	executor := &GoroutineExecutor{MaxJobs: 2}
	results := collect(executor.Run(context.Background(), "echo {}", []string{"a", "b", "c", "d"}))

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	for i, expected := range []string{"a", "b", "c", "d"} {
		if results[i].Arg != expected {
			t.Errorf("result %d: expected arg %q, got %q", i, expected, results[i].Arg)
		}
	}
}

func TestGoroutineExecutorContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	executor := &GoroutineExecutor{}
	results := collect(executor.Run(ctx, "sleep 10", []string{"a"}))

	if results[0].Err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestGoroutineExecutorEmptyArgs(t *testing.T) {
	executor := &GoroutineExecutor{}
	results := collect(executor.Run(context.Background(), "echo {}", []string{}))

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// stripANSIAndControl removes ANSI escape sequences, carriage returns, and
// spinner emoji from output so tests can match content deterministically.
// The spinner frame is non-deterministic (rate-limited at 3fps) so tests
// must not assert on which emoji appears.
func stripANSIAndControl(s string) string {
	// Strip spinner emoji first (before byte-level ANSI stripping)
	for _, emoji := range []string{"🙈", "🙉", "🙊", "💤"} {
		s = strings.ReplaceAll(s, emoji, "")
	}

	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
				i++
			}
			if i < len(s) {
				i++ // skip final byte
			}
		} else if s[i] == '\r' {
			i++
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

func TestConvertExecParallelWithStatusSequential(t *testing.T) {
	var buf bytes.Buffer
	executor := &GoroutineExecutor{MaxJobs: 1}
	exitCode := ConvertExecParallelWithStatus(
		context.Background(), executor, "echo {}", []string{"hello", "world"},
		&buf, false, true,
	)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	out := buf.String()
	clean := stripANSIAndControl(out)
	if strings.Contains(out, "pragma +status-line") {
		t.Errorf("status-line is default, should NOT emit pragma, got:\n%s", clean)
	}
	if !strings.Contains(clean, "ok 1 - echo hello") {
		t.Errorf("expected ok for first command, got:\n%s", clean)
	}
	if !strings.Contains(clean, "ok 2 - echo world") {
		t.Errorf("expected ok for second command, got:\n%s", clean)
	}
	// The status line should contain the last output line (with spinner prefix)
	if !strings.Contains(clean, "hello") || !strings.Contains(out, "# ") {
		t.Errorf("expected status line with stdout content 'hello', got:\n%s", clean)
	}
}

func TestConvertExecParallelWithStatusParallel(t *testing.T) {
	var buf bytes.Buffer
	executor := &GoroutineExecutor{MaxJobs: 0}
	exitCode := ConvertExecParallelWithStatus(
		context.Background(), executor, "echo {}", []string{"a", "b"},
		&buf, false, true,
	)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	out := buf.String()
	if strings.Contains(out, "pragma +status-line") {
		t.Errorf("status-line is default, should NOT emit pragma, got:\n%s", out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("expected 'running' in status line for parallel mode, got:\n%s", out)
	}
}

func TestConvertExecParallelWithStatusSequentialFailure(t *testing.T) {
	var buf bytes.Buffer
	executor := &GoroutineExecutor{MaxJobs: 1}
	exitCode := ConvertExecParallelWithStatus(
		context.Background(), executor, "exit 1", []string{"x"},
		&buf, false, true,
	)

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	clean := stripANSIAndControl(buf.String())
	if !strings.Contains(clean, "not ok 1") {
		t.Errorf("expected not ok for failed command, got:\n%s", clean)
	}
}

func TestConvertExecSingleCommand(t *testing.T) {
	var buf bytes.Buffer
	exitCode := ConvertExec(
		context.Background(), "echo", []string{"hello"},
		&buf, false, true,
	)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	clean := stripANSIAndControl(buf.String())
	if !strings.Contains(clean, "ok 1 - echo hello") {
		t.Errorf("expected ok test point, got:\n%s", clean)
	}
	if !strings.Contains(clean, "1::1") {
		t.Errorf("expected plan 1::1, got:\n%s", clean)
	}
}

func TestConvertExecMultipleArgs(t *testing.T) {
	var buf bytes.Buffer
	exitCode := ConvertExec(
		context.Background(), "echo", []string{"a", "b", "c"},
		&buf, false, true,
	)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	clean := stripANSIAndControl(buf.String())
	if !strings.Contains(clean, "ok 1 - echo a") {
		t.Errorf("expected first test point, got:\n%s", clean)
	}
	if !strings.Contains(clean, "ok 2 - echo b") {
		t.Errorf("expected second test point, got:\n%s", clean)
	}
	if !strings.Contains(clean, "ok 3 - echo c") {
		t.Errorf("expected third test point, got:\n%s", clean)
	}
	if !strings.Contains(clean, "1::3") {
		t.Errorf("expected plan 1::3, got:\n%s", clean)
	}
}

func TestConvertExecNoArgs(t *testing.T) {
	var buf bytes.Buffer
	exitCode := ConvertExec(
		context.Background(), "echo bare", nil,
		&buf, false, true,
	)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	clean := stripANSIAndControl(buf.String())
	if !strings.Contains(clean, "ok 1 - echo bare") {
		t.Errorf("expected ok for bare command, got:\n%s", clean)
	}
}

func TestConvertExecFailure(t *testing.T) {
	var buf bytes.Buffer
	exitCode := ConvertExec(
		context.Background(), "false", []string{"x"},
		&buf, false, true,
	)

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	clean := stripANSIAndControl(buf.String())
	if !strings.Contains(clean, "not ok 1") {
		t.Errorf("expected not ok, got:\n%s", clean)
	}
}

func TestConvertExecStatusLineShowsOutput(t *testing.T) {
	var buf bytes.Buffer
	ConvertExec(
		context.Background(), "echo", []string{"status-visible"},
		&buf, false, true,
	)

	out := buf.String()
	if strings.Contains(out, "pragma +status-line") {
		t.Errorf("status-line is default, should NOT emit pragma, got:\n%s", out)
	}
	clean := stripANSIAndControl(out)
	if !strings.Contains(clean, "status-visible") || !strings.Contains(out, "# ") {
		t.Errorf("expected status line with stdout content, got:\n%s", clean)
	}
}

func TestGoroutineExecutorRunningCounter(t *testing.T) {
	executor := &GoroutineExecutor{}

	if executor.Running() != 0 {
		t.Errorf("expected running=0 before execution, got %d", executor.Running())
	}

	results := collect(executor.Run(context.Background(), "echo {}", []string{"a"}))

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if executor.Running() != 0 {
		t.Errorf("expected running=0 after completion, got %d", executor.Running())
	}
}
