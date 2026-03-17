package crap

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindGitSkipsSelf(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not in PATH")
	}

	result, err := FindGit("/some/other/binary")
	if err != nil {
		t.Fatalf("FindGit failed: %v", err)
	}

	realResolved, _ := filepath.EvalSymlinks(realGit)
	resultResolved, _ := filepath.EvalSymlinks(result)
	if resultResolved != realResolved {
		t.Errorf("FindGit = %q, want %q", resultResolved, realResolved)
	}
}

func TestFindGitDetectsRecursion(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not in PATH")
	}

	tmpDir := t.TempDir()
	fakeGit := filepath.Join(tmpDir, "git")
	if err := os.Symlink(realGit, fakeGit); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	t.Setenv("PATH", tmpDir)
	_, err = FindGit(realGit)
	if err == nil {
		t.Error("expected error when all git candidates resolve to selfExe")
	}
}

func TestFindGitSelectsNextCandidate(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not in PATH")
	}
	realGitDir := filepath.Dir(realGit)

	tmpDir := t.TempDir()
	fakeGit := filepath.Join(tmpDir, "git")
	if err := os.WriteFile(fakeGit, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("failed to create fake git: %v", err)
	}

	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+realGitDir)
	result, err := FindGit(fakeGit)
	if err != nil {
		t.Fatalf("FindGit failed: %v", err)
	}

	resultResolved, _ := filepath.EvalSymlinks(result)
	realResolved, _ := filepath.EvalSymlinks(realGit)
	if resultResolved != realResolved {
		t.Errorf("FindGit = %q, want %q", resultResolved, realResolved)
	}
}

func TestConvertGitPassthroughSuccess(t *testing.T) {
	var buf bytes.Buffer
	exitCode := ConvertGit(
		context.Background(), os.Args[0], []string{"version"},
		&buf, os.Stdin, os.Stderr, false, false,
	)

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	out := buf.String()
	if strings.Contains(out, "CRAP-2") {
		t.Errorf("passthrough should not contain CRAP-2 header, got:\n%s", out)
	}
	if !strings.Contains(out, "git version") {
		t.Errorf("expected raw git version output, got:\n%s", out)
	}
}

func TestConvertGitPassthroughFailure(t *testing.T) {
	var buf bytes.Buffer
	var stderrBuf bytes.Buffer
	exitCode := ConvertGit(
		context.Background(), os.Args[0], []string{"status", "--bad-flag-that-does-not-exist"},
		&buf, os.Stdin, &stderrBuf, false, false,
	)

	if exitCode == 0 {
		t.Error("expected non-zero exit code")
	}

	out := buf.String()
	if strings.Contains(out, "CRAP-2") {
		t.Errorf("passthrough should not contain CRAP-2 header, got:\n%s", out)
	}
}
