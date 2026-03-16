package crap

import (
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
