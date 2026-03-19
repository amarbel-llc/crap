package crap

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunWithPTYReformatCommandNotFound(t *testing.T) {
	var buf bytes.Buffer
	code := RunWithPTYReformat(context.Background(), "nonexistent-command-xyz", nil, &buf, false)
	if code != 127 {
		t.Errorf("expected exit code 127 for missing command, got %d", code)
	}
}

func TestRunWithPTYReformatSuccess(t *testing.T) {
	var buf bytes.Buffer
	code := RunWithPTYReformat(context.Background(), "echo", []string{"ok 1 - hello"}, &buf, false)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	out := buf.String()
	if !strings.Contains(out, "CRAP-2") {
		t.Errorf("expected CRAP-2 header in output, got:\n%s", out)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected reformatted output containing 'ok', got:\n%s", out)
	}
}

func TestRunWithPTYReformatNonZeroExit(t *testing.T) {
	var buf bytes.Buffer
	code := RunWithPTYReformat(context.Background(), "sh", []string{"-c", "exit 42"}, &buf, false)
	if code != 42 {
		t.Errorf("expected exit code 42, got %d", code)
	}
}
