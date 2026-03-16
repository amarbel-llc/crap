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

func TestGitPullPhases(t *testing.T) {
	parser := NewGitPullParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"remote: Enumerating objects: 5, done.", "fetch"},
		{"remote: Counting objects: 100% (5/5), done.", "fetch"},
		{"remote: Compressing objects: 100% (3/3), done.", "fetch"},
		{"remote: Total 3 (delta 2), reused 0 (delta 0)", "fetch"},
		{"Receiving objects: 100% (3/3), done.", "fetch"},
		{"Resolving deltas: 100% (2/2), done.", "fetch"},
		{"From github.com:org/repo", "fetch"},
		{"   abc1234..def5678  main -> origin/main", "fetch"},
		{"Unpacking objects: 100% (3/3), done.", "unpack"},
		{"Updating abc1234..def5678", "merge"},
		{"Fast-forward", "merge"},
		{"Already up to date.", "merge"},
		{" file.go | 3 +++", "summary"},
		{" 1 file changed, 3 insertions(+)", "summary"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestGitPushPhases(t *testing.T) {
	parser := NewGitPushParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"Enumerating objects: 5, done.", "pack"},
		{"Counting objects: 100% (5/5), done.", "pack"},
		{"Delta compression using up to 10 threads", "pack"},
		{"Compressing objects: 100% (3/3), done.", "pack"},
		{"Writing objects: 100% (3/3), 1.23 KiB | 1.23 MiB/s, done.", "pack"},
		{"Total 3 (delta 2), reused 0 (delta 0), pack-reused 0", "transfer"},
		{"To github.com:org/repo.git", "transfer"},
		{"   abc1234..def5678  main -> main", "summary"},
		{" * [new branch]      feature -> feature", "summary"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestGitPhaseParserCollectsPhases(t *testing.T) {
	parser := NewGitPullParser()

	lines := []string{
		"remote: Enumerating objects: 5, done.",
		"remote: Counting objects: 100% (5/5), done.",
		"Unpacking objects: 100% (3/3), done.",
		"Updating abc1234..def5678",
		"Fast-forward",
		" file.go | 3 +++",
		" 1 file changed, 3 insertions(+)",
	}

	for _, line := range lines {
		parser.Feed(line)
	}

	phases := parser.Phases()

	if len(phases) != 4 {
		t.Fatalf("expected 4 phases, got %d", len(phases))
	}

	expected := []struct {
		name  string
		lines int
	}{
		{"fetch", 2},
		{"unpack", 1},
		{"merge", 2},
		{"summary", 2},
	}

	for i, exp := range expected {
		if phases[i].Name != exp.name {
			t.Errorf("phase %d: name = %q, want %q", i, phases[i].Name, exp.name)
		}
		if len(phases[i].Lines) != exp.lines {
			t.Errorf("phase %d (%s): %d lines, want %d", i, exp.name, len(phases[i].Lines), exp.lines)
		}
	}
}

func TestGitPhaseParserSkipsEmptyPhases(t *testing.T) {
	parser := NewGitPullParser()

	lines := []string{
		"Already up to date.",
	}

	for _, line := range lines {
		parser.Feed(line)
	}

	phases := parser.Phases()

	if len(phases) != 1 {
		t.Fatalf("expected 1 phase (no fetch/unpack), got %d: %+v", len(phases), phases)
	}

	if phases[0].Name != "merge" {
		t.Errorf("phase name = %q, want %q", phases[0].Name, "merge")
	}
}

func TestFindGitSkipsSelf(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not in PATH")
	}

	// When selfExe is something other than git, FindGit should find git normally
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

	// Create a temp dir with a symlink named "git" pointing to the real git
	tmpDir := t.TempDir()
	fakeGit := filepath.Join(tmpDir, "git")
	if err := os.Symlink(realGit, fakeGit); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Set PATH to only our temp dir — FindGit with selfExe=realGit should
	// skip the symlink since it resolves to the same file
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

	// Create a temp dir with a standalone executable named "git" that is NOT
	// the real git (simulates ::git renamed to "git").
	tmpDir := t.TempDir()
	fakeGit := filepath.Join(tmpDir, "git")
	if err := os.WriteFile(fakeGit, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("failed to create fake git: %v", err)
	}

	// PATH has our fake dir first, then the real git dir.
	// FindGit with selfExe=fakeGit should skip the fake one and find the real one.
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

func TestEmitGitPhasesSuccess(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"remote: Enumerating objects: 5, done.",
		"remote: Counting objects: 100% (5/5), done.",
		"Unpacking objects: 100% (3/3), done.",
		"Updating abc1234..def5678",
		"Fast-forward",
		" file.go | 3 +++",
		" 1 file changed, 3 insertions(+)",
	}

	emitPhases(tw, NewGitPullParser(), lines, 0, "git")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - fetch") {
		t.Errorf("expected fetch test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - unpack") {
		t.Errorf("expected unpack test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 3 - merge") {
		t.Errorf("expected merge test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 4 - summary") {
		t.Errorf("expected summary test point, got:\n%s", out)
	}
	if !strings.Contains(out, "1::4") {
		t.Errorf("expected plan 1::4, got:\n%s", out)
	}
}

func TestEmitGitPhasesAlreadyUpToDate(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	emitPhases(tw, NewGitPullParser(), []string{"Already up to date."}, 0, "git")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - merge") {
		t.Errorf("expected merge test point, got:\n%s", out)
	}
	if !strings.Contains(out, "1::1") {
		t.Errorf("expected plan 1::1, got:\n%s", out)
	}
}

func TestEmitGitPhasesFailure(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	emitPhases(tw, NewGitPullParser(), []string{"fatal: not a git repository"}, 128, "git")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "not ok 1 - git fetch") {
		t.Errorf("expected not ok for failed pull, got:\n%s", out)
	}
}

func TestEmitGitPushPhases(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"Enumerating objects: 5, done.",
		"Counting objects: 100% (5/5), done.",
		"Delta compression using up to 10 threads",
		"Compressing objects: 100% (3/3), done.",
		"Writing objects: 100% (3/3), 1.23 KiB | 1.23 MiB/s, done.",
		"Total 3 (delta 2), reused 0 (delta 0), pack-reused 0",
		"To github.com:org/repo.git",
		"   abc1234..def5678  main -> main",
	}

	emitPhases(tw, NewGitPushParser(), lines, 0, "git")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - pack") {
		t.Errorf("expected pack test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - transfer") {
		t.Errorf("expected transfer test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 3 - summary") {
		t.Errorf("expected summary test point, got:\n%s", out)
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

func TestEmitGitClonePhases(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"Cloning into 'repo'...",
		"remote: Enumerating objects: 100, done.",
		"remote: Counting objects: 100% (100/100), done.",
		"Receiving objects: 100% (100/100), 1.23 MiB | 5.00 MiB/s, done.",
		"Resolving deltas: 100% (20/20), done.",
	}

	emitPhases(tw, NewGitCloneParser(), lines, 0, "git")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - init") {
		t.Errorf("expected init test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - receive") {
		t.Errorf("expected receive test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 3 - resolve") {
		t.Errorf("expected resolve test point, got:\n%s", out)
	}
}

func TestGitClonePhases(t *testing.T) {
	parser := NewGitCloneParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"Cloning into 'repo'...", "init"},
		{"remote: Enumerating objects: 100, done.", "receive"},
		{"remote: Counting objects: 100% (100/100), done.", "receive"},
		{"remote: Compressing objects: 100% (80/80), done.", "receive"},
		{"remote: Total 100 (delta 20), reused 80 (delta 10)", "receive"},
		{"Receiving objects: 100% (100/100), 1.23 MiB | 5.00 MiB/s, done.", "receive"},
		{"Resolving deltas: 100% (20/20), done.", "resolve"},
		{"Updating files: 100% (50/50), done.", "checkout"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestGitFetchPhases(t *testing.T) {
	parser := NewGitFetchParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"remote: Enumerating objects: 5, done.", "negotiate"},
		{"remote: Counting objects: 100% (5/5), done.", "negotiate"},
		{"remote: Compressing objects: 100% (3/3), done.", "negotiate"},
		{"remote: Total 3 (delta 2), reused 0 (delta 0)", "negotiate"},
		{"Receiving objects: 100% (3/3), done.", "receive"},
		{"Resolving deltas: 100% (2/2), done.", "resolve"},
		{"From github.com:org/repo", "negotiate"},
		{"   abc1234..def5678  main       -> origin/main", "negotiate"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestGitRebasePhases(t *testing.T) {
	parser := NewGitRebaseParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"Created autostash: e55899e", "stash"},
		{"Applied autostash.", "stash"},
		{"Dropped refs/stash@{0} (abc1234)", "stash"},
		{"Current branch lucid-aspen is up to date.", "rebase"},
		{"Updating abc1234..def5678", "rebase"},
		{"Fast-forward", "rebase"},
		{"Successfully rebased and updated refs/heads/feature.", "rebase"},
		{"Applying: fix something", "rebase"},
		{"Rebasing (1/3)", "rebase"},
		{"CONFLICT (content): Merge conflict in file.go", "rebase"},
		{"Auto-merging file.go", "rebase"},
		{"Already up to date.", "rebase"},
		{" file.go | 3 +++", "summary"},
		{" 1 file changed, 3 insertions(+)", "summary"},
		{" 2 files changed, 10 insertions(+), 5 deletions(-)", "summary"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestEmitGitRebaseUpToDate(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"Created autostash: e55899e",
		"Current branch lucid-aspen is up to date.",
		"Applied autostash.",
	}

	emitPhases(tw, NewGitRebaseParser(), lines, 0, "git")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - stash") {
		t.Errorf("expected stash test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - rebase") {
		t.Errorf("expected rebase test point, got:\n%s", out)
	}
	if !strings.Contains(out, "1::2") {
		t.Errorf("expected plan 1::2, got:\n%s", out)
	}
}

func TestEmitGitRebaseWithSummary(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"Updating abc1234..def5678",
		"Fast-forward",
		" file.go | 3 +++",
		" 1 file changed, 3 insertions(+)",
	}

	emitPhases(tw, NewGitRebaseParser(), lines, 0, "git")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - rebase") {
		t.Errorf("expected rebase test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - summary") {
		t.Errorf("expected summary test point, got:\n%s", out)
	}
	if !strings.Contains(out, "1::2") {
		t.Errorf("expected plan 1::2, got:\n%s", out)
	}
}
