package crap

import (
	"bytes"
	"context"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonistiigi/vt100"
)

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
		t.Errorf("'ok' at line %d col %d should be green", idx, col)
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
		t.Errorf("'not ok' at line %d col %d should be red", idx, col)
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
		t.Errorf("'# SKIP' at line %d col %d should be yellow", idx, skipCol)
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
		t.Errorf("nested 'ok' at line %d col %d should be green", idx, col)
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
		t.Errorf("'ok' should be green at line %d col %d", idx, col)
	}
}
