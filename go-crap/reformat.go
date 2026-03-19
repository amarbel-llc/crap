package crap

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var (
	okLine      = regexp.MustCompile(`^(ok\b)(.*)`)
	notOkLine   = regexp.MustCompile(`^(not ok\b)(.*)`)
	bailOutLine = regexp.MustCompile(`^(Bail out!)(.*)`)
	skipDir     = regexp.MustCompile(`(?i)#\s*skip\b`)
	todoDir     = regexp.MustCompile(`(?i)#\s*todo\b`)
	warnDir     = regexp.MustCompile(`(?i)#\s*warn\b`)
)

// ReformatTAP reads raw CRAP (with or without a version line) from r and
// re-emits it as CRAP-2 on w. When color is true, ok/not ok/skip/todo/warn/
// bail out tokens are wrapped in ANSI SGR sequences.
func ReformatTAP(r io.Reader, w io.Writer, color bool) {
	fmt.Fprintln(w, "CRAP-2")

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()

		// Drop any existing version line — we already emitted ours.
		if strings.HasPrefix(line, "CRAP-2") || strings.HasPrefix(line, "CRAP version ") {
			continue
		}

		if m := notOkLine.FindStringSubmatchIndex(line); m != nil {
			rest := line[m[4]:m[5]]
			rest = colorizeDirective(rest, todoDir, "# TODO", color, ansiYellow)
			rest = colorizeDirective(rest, warnDir, "# WARN", color, ansiYellow)
			fmt.Fprintf(w, "%s%s\n", colorToken("not ok", color, ansiRed), rest)
		} else if m := okLine.FindStringSubmatchIndex(line); m != nil {
			rest := line[m[4]:m[5]]
			rest = colorizeDirective(rest, skipDir, "# SKIP", color, ansiYellow)
			rest = colorizeDirective(rest, warnDir, "# WARN", color, ansiYellow)
			fmt.Fprintf(w, "%s%s\n", colorToken("ok", color, ansiGreen), rest)
		} else if m := bailOutLine.FindStringSubmatchIndex(line); m != nil {
			rest := line[m[4]:m[5]]
			fmt.Fprintf(w, "%s%s\n", colorToken("Bail out!", color, ansiRed), rest)
		} else if color && strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			fmt.Fprintf(w, "%s%s%s\n", ansiDim, line, ansiReset)
		} else {
			fmt.Fprintln(w, line)
		}
	}
}

func colorToken(token string, color bool, code string) string {
	if color {
		return code + token + ansiReset
	}
	return token
}

func colorizeDirective(rest string, pattern *regexp.Regexp, canonical string, color bool, code string) string {
	loc := pattern.FindStringIndex(rest)
	if loc == nil {
		return rest
	}
	before := rest[:loc[0]]
	after := rest[loc[1]:]
	directive := colorToken(canonical, color, code)
	return before + directive + after
}
