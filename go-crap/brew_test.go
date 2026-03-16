package crap

import (
	"bytes"
	"strings"
	"testing"
)

func TestBrewInstallPhases(t *testing.T) {
	parser := NewBrewInstallParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"==> Downloading https://ghcr.io/v2/homebrew/core/jq/manifests/1.7.1", "download"},
		{"==> Fetching jq", "download"},
		{"==> Fetching dependencies for jq: oniguruma", "download"},
		{"Already downloaded: /Users/user/Library/Caches/Homebrew/downloads/abc--jq-1.7.1.tar.gz", "download"},
		{"==> Installing jq", "install"},
		{"==> Pouring jq--1.7.1.arm64_sonoma.bottle.tar.gz", "install"},
		{"==> Linking jq", "link"},
		{"==> Caveats", "caveats"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestBrewUpdatePhases(t *testing.T) {
	parser := NewBrewUpdateParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"==> Fetching newest version of Homebrew...", "fetch"},
		{"remote: Enumerating objects: 100, done.", "fetch"},
		{"remote: Counting objects: 100% (100/100), done.", "fetch"},
		{"==> Updated 2 taps (homebrew/core and homebrew/cask).", "update"},
		{"==> New Formulae", "update"},
		{"==> Updated Formulae", "update"},
		{"==> Deleted Formulae", "update"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestBrewTapPhases(t *testing.T) {
	parser := NewBrewTapParser()

	tests := []struct {
		line  string
		phase string
	}{
		{"==> Tapping homebrew/cask", "clone"},
		{"Cloning into '/opt/homebrew/Library/Taps/homebrew/homebrew-cask'...", "clone"},
		{"remote: Enumerating objects: 100, done.", "clone"},
		{"==> Tapped 1 command and 4000 casks (4,123 files, 300MB).", "install"},
	}

	for _, tt := range tests {
		got := parser.Classify(tt.line)
		if got != tt.phase {
			t.Errorf("Classify(%q) = %q, want %q", tt.line, got, tt.phase)
		}
	}
}

func TestEmitBrewInstallPhases(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"==> Fetching jq",
		"==> Downloading https://ghcr.io/v2/homebrew/core/jq/manifests/1.7.1",
		"==> Installing jq",
		"==> Pouring jq--1.7.1.arm64_sonoma.bottle.tar.gz",
		"==> Linking jq",
	}

	emitPhases(tw, NewBrewInstallParser(), lines, 0, "brew")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - download") {
		t.Errorf("expected download test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - install") {
		t.Errorf("expected install test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 3 - link") {
		t.Errorf("expected link test point, got:\n%s", out)
	}
}

func TestEmitBrewInstallFailure(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	emitPhases(tw, NewBrewInstallParser(), []string{"Error: No formula found"}, 1, "brew")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "not ok 1 - brew download") {
		t.Errorf("expected not ok for failed install, got:\n%s", out)
	}
}

func TestEmitBrewUpdatePhases(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"==> Fetching newest version of Homebrew...",
		"remote: Enumerating objects: 100, done.",
		"==> Updated 2 taps (homebrew/core and homebrew/cask).",
		"==> New Formulae",
	}

	emitPhases(tw, NewBrewUpdateParser(), lines, 0, "brew")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - fetch") {
		t.Errorf("expected fetch test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - update") {
		t.Errorf("expected update test point, got:\n%s", out)
	}
}

func TestEmitBrewTapPhases(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.EnableTTYBuildLastLine()

	lines := []string{
		"==> Tapping homebrew/cask",
		"Cloning into '/opt/homebrew/Library/Taps/homebrew/homebrew-cask'...",
		"==> Tapped 1 command and 4000 casks (4,123 files, 300MB).",
	}

	emitPhases(tw, NewBrewTapParser(), lines, 0, "brew")
	tw.Plan()

	out := stripANSIAndControl(buf.String())
	if !strings.Contains(out, "ok 1 - clone") {
		t.Errorf("expected clone test point, got:\n%s", out)
	}
	if !strings.Contains(out, "ok 2 - install") {
		t.Errorf("expected install test point, got:\n%s", out)
	}
}
