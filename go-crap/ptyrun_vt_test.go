package crap

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pty "github.com/creack/pty/v2"
	"github.com/tonistiigi/vt100"
)

// syncWriter is a concurrency-safe io.Writer for capturing output mid-flight.
type syncWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sw *syncWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.buf.Write(p)
}

func (sw *syncWriter) snapshot() []byte {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	cp := make([]byte, sw.buf.Len())
	copy(cp, sw.buf.Bytes())
	return cp
}

// renderScreen feeds raw output bytes through a 120x80 VT100 emulator
// and returns the visible lines (trailing blank lines trimmed) and the
// full Format grid.
func renderScreen(raw []byte) ([]string, [][]vt100.Format) {
	term := vt100.NewVT100(80, 120)
	term.Write(raw)

	var lines []string
	for _, row := range term.Content {
		lines = append(lines, strings.TrimRight(string(row), " "))
	}

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines, term.Format
}

func findLine(lines []string, substr string) int {
	for i, line := range lines {
		if strings.Contains(line, substr) {
			return i
		}
	}
	return -1
}

func findLinePrefix(lines []string, prefix string) int {
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " "), prefix) {
			return i
		}
	}
	return -1
}

func cellsHaveColor(formats [][]vt100.Format, row, startCol, endCol int, fg color.RGBA) bool {
	if row >= len(formats) {
		return false
	}
	for col := startCol; col < endCol && col < len(formats[row]); col++ {
		if formats[row][col].Fg != fg {
			return false
		}
	}
	return true
}

// renderLineANSI reconstructs an ANSI-colored string from a vt100 line and
// its format row by emitting 24-bit SGR sequences on color transitions.
func renderLineANSI(line string, fmtRow []vt100.Format) string {
	var b strings.Builder
	var prevFg color.RGBA
	zero := color.RGBA{}
	for i, r := range line {
		var fg color.RGBA
		if i < len(fmtRow) {
			fg = fmtRow[i].Fg
		}
		if fg != prevFg {
			if fg == zero {
				b.WriteString("\033[0m")
			} else {
				fmt.Fprintf(&b, "\033[38;2;%d;%d;%dm", fg.R, fg.G, fg.B)
			}
			prevFg = fg
		}
		b.WriteRune(r)
	}
	if prevFg != zero {
		b.WriteString("\033[0m")
	}
	return b.String()
}

// colorMismatch returns an error message showing ANSI-rendered actual and
// expected lines when a color assertion fails. expectedFg is applied to
// columns [startCol, endCol) in the expected rendering.
func colorMismatch(t *testing.T, lines []string, formats [][]vt100.Format, row, startCol, endCol int, expectedFg color.RGBA) {
	t.Helper()
	actual := renderLineANSI(lines[row], formats[row])

	expectedFmt := make([]vt100.Format, len(formats[row]))
	copy(expectedFmt, formats[row])
	for col := startCol; col < endCol && col < len(expectedFmt); col++ {
		expectedFmt[col].Fg = expectedFg
	}
	expected := renderLineANSI(lines[row], expectedFmt)

	t.Errorf("color mismatch on line %d:\nexpected: %s\n  actual: %s", row, expected, actual)
}

func cellsAreDim(formats [][]vt100.Format, row, startCol, endCol int) bool {
	if row >= len(formats) {
		return false
	}
	for col := startCol; col < endCol && col < len(formats[row]); col++ {
		if formats[row][col].Intensity != vt100.Dim {
			return false
		}
	}
	return true
}

func writeTAPScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "emit-tap.sh")
	script := "#!/bin/sh\ncat <<'EOF'\nTAP version 14\n1::3\n# Subtest: compilation\n    1::2\n    ok 1 - compile main.rs\n      ---\n      output: |\n        compiling main.rs\n        compiling lib.rs\n        linking binary\n      ...\n    ok 2 - compile tests\n      ---\n      output: |\n        compiling test_parse.rs\n        compiling test_format.rs\n      ...\nok 1 - compilation\n# Subtest: test suite\n    1::3\n    ok 1 - test_parse\n      ---\n      output: |\n        running test_parse\n        assertion passed: expected 42 got 42\n      ...\n    not ok 2 - test_format\n      ---\n      message: \"assertion failed\"\n      severity: fail\n      output: |\n        running test_format\n        FAIL: expected \"hello\" got \"world\"\n      ...\n    ok 3 - test_lint # skip no linter configured\nok 2 - test suite\nok 3 - cleanup\n1::3\nEOF\n"
	os.WriteFile(path, []byte(script), 0o755)
	return path
}

func runTAPFixture(t *testing.T, color bool) ([]string, [][]vt100.Format) {
	t.Helper()
	script := writeTAPScript(t)
	var buf bytes.Buffer
	code := RunWithPTYReformat(
		context.Background(), script, nil, &buf, color,
		WithSpinner(false),
	)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nraw output:\n%s", code, buf.String())
	}
	return renderScreen(buf.Bytes())
}

// --- Smoke test ---

func TestVTRenderScreenSmoke(t *testing.T) {
	raw := []byte("hello world\n")
	lines, _ := renderScreen(raw)
	if len(lines) == 0 {
		t.Fatal("expected at least one line")
	}
	if !strings.Contains(lines[0], "hello world") {
		t.Errorf("expected 'hello world', got %q", lines[0])
	}
}

// --- TAP reformat: structure ---

func TestVTTAPVersionHeader(t *testing.T) {
	lines, _ := runTAPFixture(t, false)
	if len(lines) == 0 || lines[0] != "CRAP-2" {
		t.Errorf("expected first line 'CRAP-2', got %q\nall lines:\n%s", lines[0], strings.Join(lines, "\n"))
	}
}

func TestVTTAPSubtestComments(t *testing.T) {
	lines, _ := runTAPFixture(t, false)
	if findLine(lines, "# Subtest: compilation") < 0 {
		t.Errorf("missing '# Subtest: compilation'\nall lines:\n%s", strings.Join(lines, "\n"))
	}
	if findLine(lines, "# Subtest: test suite") < 0 {
		t.Errorf("missing '# Subtest: test suite'\nall lines:\n%s", strings.Join(lines, "\n"))
	}
}

func TestVTTAPNestedTestPointsIndented(t *testing.T) {
	lines, _ := runTAPFixture(t, false)
	idx := findLine(lines, "ok 1 - compile main.rs")
	if idx < 0 {
		t.Fatalf("missing 'ok 1 - compile main.rs'\nall lines:\n%s", strings.Join(lines, "\n"))
	}
	line := lines[idx]
	if !strings.HasPrefix(line, "    ") {
		t.Errorf("expected 4-space indent, got %q", line)
	}
}

func TestVTTAPParentTestPoints(t *testing.T) {
	lines, _ := runTAPFixture(t, false)
	for _, want := range []string{
		"ok 1 - compilation",
		"ok 2 - test suite",
		"ok 3 - cleanup",
	} {
		idx := findLine(lines, want)
		if idx < 0 {
			t.Errorf("missing parent test point %q\nall lines:\n%s", want, strings.Join(lines, "\n"))
			continue
		}
		if strings.HasPrefix(lines[idx], " ") {
			t.Errorf("parent test point %q should not be indented: %q", want, lines[idx])
		}
	}
}

func TestVTTAPPlanLine(t *testing.T) {
	lines, _ := runTAPFixture(t, false)
	if findLine(lines, "1::3") < 0 {
		t.Errorf("missing plan line '1::3'\nall lines:\n%s", strings.Join(lines, "\n"))
	}
}

func TestVTTAPYAMLPassthrough(t *testing.T) {
	lines, _ := runTAPFixture(t, false)
	for _, want := range []string{
		"---",
		"output: |",
		"compiling main.rs",
		"...",
	} {
		if findLine(lines, want) < 0 {
			t.Errorf("missing YAML content %q\nall lines:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}

// --- TAP reformat: colorization ---

func TestVTTAPColorOkGreen(t *testing.T) {
	lines, formats := runTAPFixture(t, true)
	idx := findLinePrefix(lines, "ok 1 - compilation")
	if idx < 0 {
		t.Fatalf("missing 'ok 1 - compilation'\nall lines:\n%s", strings.Join(lines, "\n"))
	}
	col := strings.Index(lines[idx], "ok")
	if !cellsHaveColor(formats, idx, col, col+2, vt100.Green) {
		colorMismatch(t, lines, formats, idx, col, col+2, vt100.Green)
	}
}

func TestVTTAPColorNotOkRed(t *testing.T) {
	lines, formats := runTAPFixture(t, true)
	idx := findLine(lines, "not ok 2 - test_format")
	if idx < 0 {
		t.Fatalf("missing 'not ok 2 - test_format'\nall lines:\n%s", strings.Join(lines, "\n"))
	}
	col := strings.Index(lines[idx], "not ok")
	if !cellsHaveColor(formats, idx, col, col+6, vt100.Red) {
		colorMismatch(t, lines, formats, idx, col, col+6, vt100.Red)
	}
}

func TestVTTAPColorSkipYellow(t *testing.T) {
	lines, formats := runTAPFixture(t, true)
	idx := findLine(lines, "test_lint")
	if idx < 0 {
		t.Fatalf("missing test_lint line\nall lines:\n%s", strings.Join(lines, "\n"))
	}
	skipCol := strings.Index(lines[idx], "# SKIP")
	if skipCol < 0 {
		t.Fatalf("missing '# SKIP' on test_lint line: %q", lines[idx])
	}
	if !cellsHaveColor(formats, idx, skipCol, skipCol+6, vt100.Yellow) {
		colorMismatch(t, lines, formats, idx, skipCol, skipCol+6, vt100.Yellow)
	}
}

func TestVTTAPColorSubtestCommentDim(t *testing.T) {
	lines, formats := runTAPFixture(t, true)
	idx := findLine(lines, "# Subtest: compilation")
	if idx < 0 {
		t.Fatalf("missing '# Subtest: compilation'\nall lines:\n%s", strings.Join(lines, "\n"))
	}
	col := strings.Index(lines[idx], "#")
	if !cellsAreDim(formats, idx, col, col+len("# Subtest: compilation")) {
		t.Errorf("'# Subtest: compilation' at line %d should be dim", idx)
	}
}

func TestVTTAPColorNestedOkGreen(t *testing.T) {
	lines, formats := runTAPFixture(t, true)
	idx := findLine(lines, "ok 1 - compile main.rs")
	if idx < 0 {
		t.Fatalf("missing nested 'ok 1 - compile main.rs'\nall lines:\n%s", strings.Join(lines, "\n"))
	}
	col := strings.Index(lines[idx], "ok")
	if !cellsHaveColor(formats, idx, col, col+2, vt100.Green) {
		colorMismatch(t, lines, formats, idx, col, col+2, vt100.Green)
	}
}

// --- TAP reformat: no status line artifacts ---

func TestVTTAPNoStatusLineArtifacts(t *testing.T) {
	lines, _ := runTAPFixture(t, true)
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "# ok ") || strings.HasPrefix(trimmed, "# not ok ") {
			t.Errorf("line %d looks like a status line artifact: %q", i, line)
		}
		if strings.HasPrefix(trimmed, "# 1::") {
			t.Errorf("line %d looks like a status line artifact: %q", i, line)
		}
	}
}

// --- Status line: in-progress spinner ---

func TestVTStatusLineShowsSpinnerWhileInProgress(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, true)
	tw.EnableTTYBuildLastLine()
	tw.StartTestPoint("my-command")

	// Render the screen mid-flight, before FinishInProgress is called.
	lines, _ := renderScreen(buf.Bytes())
	if len(lines) == 0 {
		t.Fatal("expected at least one line of output")
	}

	// Find the in-progress line (should contain the description).
	idx := findLine(lines, "my-command")
	if idx < 0 {
		t.Fatalf("missing in-progress line with 'my-command'\nall lines:\n%s", strings.Join(lines, "\n"))
	}

	// The line should start with a two-character braille spinner.
	line := lines[idx]
	runes := []rune(line)
	if len(runes) < 2 {
		t.Fatalf("in-progress line too short: %q", line)
	}
	r0, r1 := runes[0], runes[1]
	// Braille characters are in the Unicode range U+2800..U+28FF.
	if r0 < 0x2800 || r0 > 0x28FF || r1 < 0x2800 || r1 > 0x28FF {
		t.Errorf("expected two braille spinner characters at start of in-progress line, got %q (%U %U)", line, r0, r1)
	}
}

// --- Bug-documenting tests (skipped, tracked in amarbel-llc/crap#4) ---

func TestVTRunWithPTYReformatShowsSpinnerMidFlight(t *testing.T) {

	sw := &syncWriter{}
	done := make(chan int, 1)
	go func() {
		code := RunWithPTYReformat(
			context.Background(), "sh", []string{"-c", "sleep 2 && echo hello"}, sw, true,
		)
		done <- code
	}()

	time.Sleep(500 * time.Millisecond)

	snap := sw.snapshot()
	lines, _ := renderScreen(snap)

	idx := findLine(lines, "sh -c sleep 2 && echo hello")
	if idx < 0 {
		t.Errorf("missing in-progress spinner line mid-flight\nall lines:\n%s", strings.Join(lines, "\n"))
	} else {
		runes := []rune(lines[idx])
		if len(runes) < 2 || runes[0] < 0x2800 || runes[0] > 0x28FF {
			t.Errorf("expected braille spinner at start of in-progress line, got %q", lines[idx])
		}
	}

	code := <-done
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestVTTAPStatusLineBetweenSlowLines(t *testing.T) {

	sw := &syncWriter{}
	done := make(chan int, 1)
	go func() {
		code := RunWithPTYReformat(
			context.Background(), "sh", []string{"-c",
				"echo 'TAP version 14'; echo '1..2'; echo 'ok 1 - fast'; sleep 2; echo 'ok 2 - slow'",
			}, sw, true,
		)
		done <- code
	}()

	time.Sleep(800 * time.Millisecond)

	snap := sw.snapshot()
	lines, _ := renderScreen(snap)

	t.Logf("mid-flight TAP output (%d lines):", len(lines))
	for i, l := range lines {
		t.Logf("  %d: %q", i, l)
	}

	if findLine(lines, "CRAP-2") < 0 {
		t.Error("missing CRAP-2 header mid-flight")
	}

	if findLine(lines, "ok 1 - fast") < 0 {
		t.Error("missing 'ok 1 - fast' mid-flight")
	}

	hasSpinner := false
	for _, l := range lines {
		runes := []rune(l)
		if len(runes) >= 2 && runes[0] >= 0x2800 && runes[0] <= 0x28FF {
			hasSpinner = true
			t.Logf("found spinner line: %q", l)
			break
		}
	}
	if !hasSpinner {
		t.Error("no spinner visible mid-flight between slow TAP lines")
	}

	<-done
}

// --- Passing behavior tests ---

func TestVTSpinnerAppearsAfterFirstOutput(t *testing.T) {
	sw := &syncWriter{}
	done := make(chan int, 1)
	go func() {
		code := RunWithPTYReformat(
			context.Background(), "sh", []string{"-c", "echo hello; sleep 2"}, sw, true,
		)
		done <- code
	}()

	time.Sleep(500 * time.Millisecond)

	snap := sw.snapshot()
	lines, _ := renderScreen(snap)

	idx := findLine(lines, "sh -c echo hello; sleep 2")
	if idx < 0 {
		t.Errorf("missing spinner line after first output\nall lines:\n%s", strings.Join(lines, "\n"))
	} else {
		runes := []rune(lines[idx])
		if len(runes) < 2 || runes[0] < 0x2800 || runes[0] > 0x28FF {
			t.Errorf("expected braille spinner, got %q", lines[idx])
		}
	}

	<-done
}

func TestVTOpaqueStatusLineShowsLastOutput(t *testing.T) {
	sw := &syncWriter{}
	done := make(chan int, 1)
	go func() {
		code := RunWithPTYReformat(
			context.Background(), "sh", []string{"-c",
				"echo 'line one'; echo 'line two'; sleep 2",
			}, sw, true,
		)
		done <- code
	}()

	time.Sleep(500 * time.Millisecond)

	snap := sw.snapshot()
	lines, _ := renderScreen(snap)

	t.Logf("mid-flight opaque output (%d lines):", len(lines))
	for i, l := range lines {
		t.Logf("  %d: %q", i, l)
	}

	if findLine(lines, "line two") < 0 {
		t.Error("missing status line with 'line two' mid-flight")
	}

	hasSpinner := false
	for _, l := range lines {
		runes := []rune(l)
		if len(runes) >= 2 && runes[0] >= 0x2800 && runes[0] <= 0x28FF {
			hasSpinner = true
			t.Logf("found spinner line: %q", l)
			break
		}
	}
	if !hasSpinner {
		t.Error("no spinner visible mid-flight in opaque path")
	}

	<-done
}

// --- Opaque path ---

func TestVTOpaqueWrapsAsSingleTestPoint(t *testing.T) {
	var buf bytes.Buffer
	code := RunWithPTYReformat(
		context.Background(), "echo", []string{"hello"}, &buf, false,
		WithSpinner(false),
	)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	lines, _ := renderScreen(buf.Bytes())
	if len(lines) == 0 || lines[0] != "CRAP-2" {
		t.Errorf("expected CRAP-2 header, got %q", lines[0])
	}
	if findLine(lines, "ok 1 - echo hello") < 0 {
		t.Errorf("missing 'ok 1 - echo hello' in:\n%s", strings.Join(lines, "\n"))
	}
	if findLine(lines, "1::1") < 0 {
		t.Errorf("missing plan '1::1' in:\n%s", strings.Join(lines, "\n"))
	}
}

func TestVTOpaqueColorOkGreen(t *testing.T) {
	var buf bytes.Buffer
	RunWithPTYReformat(
		context.Background(), "echo", []string{"hello"}, &buf, true,
		WithSpinner(false),
	)
	lines, formats := renderScreen(buf.Bytes())
	idx := findLine(lines, "ok 1 - echo hello")
	if idx < 0 {
		t.Fatalf("missing 'ok 1 - echo hello'\nall lines:\n%s", strings.Join(lines, "\n"))
	}
	col := strings.Index(lines[idx], "ok")
	if !cellsHaveColor(formats, idx, col, col+2, vt100.Green) {
		colorMismatch(t, lines, formats, idx, col, col+2, vt100.Green)
	}
}

// buildLargeColon builds the :: binary once per test binary invocation.
var (
	largeColonOnce sync.Once
	largeColonBin  string
	largeColonErr  error
)

func requireLargeColon(t *testing.T) string {
	t.Helper()
	largeColonOnce.Do(func() {
		dir, err := os.MkdirTemp("", "large-colon-test-*")
		if err != nil {
			largeColonErr = fmt.Errorf("failed to create temp dir: %s", err)
			return
		}
		largeColonBin = filepath.Join(dir, "large-colon")
		out, buildErr := exec.Command("go", "build", "-o", largeColonBin, "./cmd/large-colon").CombinedOutput()
		if buildErr != nil {
			largeColonErr = fmt.Errorf("failed to build large-colon: %s\n%s", buildErr, out)
		}
	})
	if largeColonErr != nil {
		t.Fatal(largeColonErr)
	}
	return largeColonBin
}

// runLargeColonPTY runs `:: <args>` in a real PTY (so stdoutIsTerminal()
// returns true) and returns the final rendered screen. This matches how
// users actually invoke :: from a terminal.
func runLargeColonPTY(t *testing.T, args ...string) ([]string, [][]vt100.Format) {
	t.Helper()
	bin := requireLargeColon(t)
	cmd := exec.Command(bin, args...)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("failed to start large-colon in PTY: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, ptmx)
	cmd.Wait()

	return renderScreen(buf.Bytes())
}

// runLargeColonPTYMidFlight runs `:: <args>` in a real PTY and snapshots
// the output after delay, then waits for the command to finish.
func runLargeColonPTYMidFlight(t *testing.T, delay time.Duration, args ...string) ([]string, [][]vt100.Format, *exec.Cmd) {
	t.Helper()
	bin := requireLargeColon(t)
	cmd := exec.Command(bin, args...)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("failed to start large-colon in PTY: %v", err)
	}

	sw := &syncWriter{}
	go io.Copy(sw, ptmx)

	time.Sleep(delay)
	snap := sw.snapshot()
	lines, formats := renderScreen(snap)
	return lines, formats, cmd
}

// --- End-to-end :: tests (real PTY, real binary) ---

func TestE2ETAPFinalOutputHasCRAPHeader(t *testing.T) {
	script := writeTAPScript(t)
	lines, _ := runLargeColonPTY(t, script)

	if findLine(lines, "CRAP-2") < 0 {
		t.Errorf("missing CRAP-2 header in final :: output\nall lines:\n%s", strings.Join(lines, "\n"))
	}
}

func TestE2ETAPPlanFormatPassthrough(t *testing.T) {

	dir := t.TempDir()
	script := filepath.Join(dir, "tap-plan.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho 'TAP version 14'\necho '1..2'\necho 'ok 1 - a'\necho 'ok 2 - b'\n"), 0o755)

	lines, _ := runLargeColonPTY(t, script)

	if findLine(lines, "1::2") < 0 {
		t.Errorf("expected plan converted to CRAP format (1::2)\nall lines:\n%s", strings.Join(lines, "\n"))
	}
}

func TestE2ETAPFinalOutputNoArtifacts(t *testing.T) {
	script := writeTAPScript(t)
	lines, _ := runLargeColonPTY(t, script)

	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "# ok ") || strings.HasPrefix(trimmed, "# not ok ") {
			t.Errorf("line %d: status line artifact: %q", i, line)
		}
		if strings.HasPrefix(trimmed, "# 1::") || strings.HasPrefix(trimmed, "# 1..") {
			t.Errorf("line %d: status line artifact: %q", i, line)
		}
	}
}

func TestE2ENoOutputBeforeFirstLine(t *testing.T) {

	lines, _, cmd := runLargeColonPTYMidFlight(t, 500*time.Millisecond,
		"sh", "-c", "sleep 2; echo hello")

	t.Logf("mid-flight output before first line (%d lines):", len(lines))
	for i, l := range lines {
		t.Logf("  %d: %q", i, l)
	}

	if len(lines) == 0 {
		t.Error("no output at all before child's first line — no spinner, no header")
	} else {
		hasHeader := findLine(lines, "CRAP-2") >= 0
		hasSpinner := false
		for _, l := range lines {
			runes := []rune(l)
			if len(runes) >= 2 && runes[0] >= 0x2800 && runes[0] <= 0x28FF {
				hasSpinner = true
				break
			}
		}
		if !hasHeader {
			t.Error("missing CRAP-2 header before first child output")
		}
		if !hasSpinner {
			t.Error("missing spinner before first child output")
		}
	}

	cmd.Wait()
}

func TestE2ETAPNoSpinnerBetweenLines(t *testing.T) {

	dir := t.TempDir()
	script := filepath.Join(dir, "slow-tap.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho 'TAP version 14'\necho '1..2'\necho 'ok 1 - fast'\nsleep 2\necho 'ok 2 - slow'\n"), 0o755)

	lines, _, cmd := runLargeColonPTYMidFlight(t, 800*time.Millisecond, script)

	t.Logf("mid-flight TAP via :: (%d lines):", len(lines))
	for i, l := range lines {
		t.Logf("  %d: %q", i, l)
	}

	hasSpinner := false
	for _, l := range lines {
		runes := []rune(l)
		if len(runes) >= 2 && runes[0] >= 0x2800 && runes[0] <= 0x28FF {
			hasSpinner = true
			t.Logf("found spinner: %q", l)
			break
		}
	}
	if !hasSpinner {
		t.Error("no spinner visible mid-flight in TAP path via ::")
	}

	cmd.Wait()
}

func TestE2EOpaqueSpinnerAndStatusLine(t *testing.T) {
	lines, _, cmd := runLargeColonPTYMidFlight(t, 500*time.Millisecond,
		"sh", "-c", "echo 'building...'; echo 'compiling main.go'; sleep 2; echo 'done'")

	t.Logf("mid-flight opaque via :: (%d lines):", len(lines))
	for i, l := range lines {
		t.Logf("  %d: %q", i, l)
	}

	if findLine(lines, "CRAP-2") < 0 {
		t.Error("missing CRAP-2 header")
	}

	hasSpinner := false
	for _, l := range lines {
		runes := []rune(l)
		if len(runes) >= 2 && runes[0] >= 0x2800 && runes[0] <= 0x28FF {
			hasSpinner = true
			break
		}
	}
	if !hasSpinner {
		t.Error("no spinner visible mid-flight in opaque path via ::")
	}

	if findLine(lines, "compiling main.go") < 0 {
		t.Error("missing status line with 'compiling main.go'")
	}

	cmd.Wait()
}

func TestE2EOpaqueNonZeroExitFinal(t *testing.T) {
	lines, formats := runLargeColonPTY(t, "sh", "-c", "echo oops; exit 1")

	t.Logf("final output:\n%s", strings.Join(lines, "\n"))

	idx := findLine(lines, "not ok")
	if idx < 0 {
		t.Fatal("missing 'not ok' for failed command")
	}

	col := strings.Index(lines[idx], "not ok")
	if !cellsHaveColor(formats, idx, col, col+6, vt100.Red) {
		colorMismatch(t, lines, formats, idx, col, col+6, vt100.Red)
	}

	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "# oops") {
			t.Errorf("line %d: status line artifact not cleaned up: %q", i, line)
		}
	}
}

func TestE2ETAPColorInPTY(t *testing.T) {
	script := writeTAPScript(t)
	lines, formats := runLargeColonPTY(t, script)

	idx := findLinePrefix(lines, "ok 1 - compilation")
	if idx < 0 {
		t.Fatal("missing 'ok 1 - compilation'")
	}
	col := strings.Index(lines[idx], "ok")
	if !cellsHaveColor(formats, idx, col, col+2, vt100.Green) {
		colorMismatch(t, lines, formats, idx, col, col+2, vt100.Green)
	}

	idx = findLine(lines, "not ok 2 - test_format")
	if idx < 0 {
		t.Fatal("missing 'not ok 2 - test_format'")
	}
	col = strings.Index(lines[idx], "not ok")
	if !cellsHaveColor(formats, idx, col, col+6, vt100.Red) {
		colorMismatch(t, lines, formats, idx, col, col+6, vt100.Red)
	}
}
