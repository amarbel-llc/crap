package crap

import (
	"regexp"
	"strings"
)

type lineKind int

const (
	lineUnknown lineKind = iota
	lineVersion
	linePlan
	lineTestPoint
	lineYAMLStart
	lineYAMLEnd
	lineBailOut
	linePragma
	lineComment
	lineSubtestComment
	lineEmpty
)

var (
	planRegexp      = regexp.MustCompile(`^1\.\.([\d,.\x{00a0}\x{202f} ]+)(\s+#\s+(.*))?$`)
	testPointRegexp = regexp.MustCompile(`^(not )?ok\b`)
	pragmaRegexp    = regexp.MustCompile(`^pragma\s+[+-]\w`)
	// csiRegexp matches all CSI escape sequences (ESC [ ... <final byte>),
	// not just SGR, per the ANSI Display Hints amendment.
	csiRegexp = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")
	// nonSGRRegexp matches CSI sequences whose final byte is anything except
	// 'm' (SGR), per the ANSI in YAML Output Blocks amendment.
	nonSGRRegexp = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-ln-z]")
)

// stripANSI removes all CSI escape sequences from a string.
func stripANSI(s string) string {
	return csiRegexp.ReplaceAllString(s, "")
}

// stripNonSGR removes non-SGR CSI sequences, preserving SGR (ESC[...m) color codes.
func stripNonSGR(s string) string {
	return nonSGRRegexp.ReplaceAllString(s, "")
}

// HasVisibleContent returns true if s contains at least one visible character
// after stripping ANSI CSI sequences, whitespace, and control characters.
func HasVisibleContent(s string) bool {
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Skip CSI sequence: ESC [ <params> <final byte>
			i += 2
			for i < len(s) && (s[i] < '@' || s[i] > '~') {
				i++
			}
			if i < len(s) {
				i++ // skip final byte
			}
		} else if s[i] > ' ' && s[i] < 0x7f || s[i] > 0x7f {
			// Visible: printable ASCII (excluding space) or non-ASCII
			return true
		} else {
			i++
		}
	}
	return false
}

func classifyLine(line string) lineKind {
	if line == "CRAP version 2" {
		return lineVersion
	}

	if planRegexp.MatchString(line) {
		return linePlan
	}

	if testPointRegexp.MatchString(line) {
		return lineTestPoint
	}

	if line == "---" {
		return lineYAMLStart
	}

	if line == "..." {
		return lineYAMLEnd
	}

	if strings.HasPrefix(line, "Bail out!") {
		return lineBailOut
	}

	if pragmaRegexp.MatchString(line) {
		return linePragma
	}

	if strings.HasPrefix(line, "# Subtest") {
		return lineSubtestComment
	}

	if strings.HasPrefix(line, "#") {
		return lineComment
	}

	if strings.TrimSpace(line) == "" {
		return lineEmpty
	}

	return lineUnknown
}
