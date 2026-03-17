package crap

import (
	"bytes"
	"strings"
	"testing"
)

func TestConvertTAPToCRAP(t *testing.T) {
	tap := "TAP version 14\n# status line text\nok 1 - stash\nok 2 - rebase\n1..2\n"
	var buf bytes.Buffer
	convertTAPToCRAP(strings.NewReader(tap), &buf, false)
	out := stripANSIAndControl(buf.String())

	// Should contain CRAP-2 header (emitted as "CRAP-2" by NewColorWriter)
	if !strings.Contains(out, "CRAP-2") {
		t.Errorf("expected CRAP-2 header, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 1 - stash") {
		t.Errorf("expected stash test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - rebase") {
		t.Errorf("expected rebase test point, got:\n%s", out)
	}
	if !strings.Contains(out, "1::2") {
		t.Errorf("expected plan 1::2, got:\n%s", out)
	}
	if strings.Contains(out, "TAP version") {
		t.Errorf("TAP version line should be consumed, got:\n%s", out)
	}
}

func TestConvertTAPToCRAPSkipsComments(t *testing.T) {
	tap := "TAP version 14\n# hello world\nok 1 - test\n1..1\n"
	var buf bytes.Buffer
	convertTAPToCRAP(strings.NewReader(tap), &buf, false)
	out := buf.String()

	if strings.Contains(out, "# hello world") {
		t.Errorf("bare comment should not appear in output, got:\n%s", out)
	}
}

func TestConvertTAPToCRAPWithSubtests(t *testing.T) {
	tap := "TAP version 14\n    # status\n    ok 1 - download\n    ok 2 - install\n    1..2\nok 1 - foo\n1..1\n"
	var buf bytes.Buffer
	convertTAPToCRAP(strings.NewReader(tap), &buf, false)
	out := stripANSIAndControl(buf.String())

	if !strings.Contains(out, "ok 1 - foo") {
		t.Errorf("expected top-level test point, got:\n%s", out)
	}
	if !strings.Contains(out, "1::1") {
		t.Errorf("expected plan 1::1, got:\n%s", out)
	}
}
