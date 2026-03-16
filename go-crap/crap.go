package crap

import (
	"fmt"
	"io"
	"iter"
	"sort"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// ANSI color codes for TTY output.
const (
	ansiGreen  = "\033[32m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiReset  = "\033[0m"
)

type Writer struct {
	w                 io.Writer
	n                 int
	depth             int
	planEmitted       bool
	failed            bool
	color             bool
	locale            language.Tag
	printer           *message.Printer
	streamedOutput    bool
	ttyBuildLastLine  bool
	statusLineActive  bool
	statusProcessor   *StatusLineProcessor
}

func NewWriter(w io.Writer) *Writer {
	fmt.Fprintln(w, "CRAP-2")
	return &Writer{w: w}
}

// NewColorWriter creates a Writer that colorizes ok/not ok when color is true.
func NewColorWriter(w io.Writer, color bool) *Writer {
	fmt.Fprintln(w, "CRAP-2")
	return &Writer{w: w, color: color}
}

// NewBareWriter creates a Writer that does not emit the CRAP-2 version line
// or any pragma lines. Use this for incremental emission after a normal Writer
// has already emitted the header and plan.
func NewBareWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// NewBareColorWriter creates a bare Writer with ANSI color support.
func NewBareColorWriter(w io.Writer, color bool) *Writer {
	return &Writer{w: w, color: color}
}

func NewLocaleWriter(w io.Writer, locale language.Tag) *Writer {
	fmt.Fprintln(w, "CRAP-2")
	fmt.Fprintf(w, "pragma +locale-formatting:%s\n", locale)
	return &Writer{
		w:       w,
		locale:  locale,
		printer: message.NewPrinter(locale),
	}
}

func (tw *Writer) formatNumber(n int) string {
	if tw.printer != nil {
		return tw.printer.Sprintf("%d", n)
	}
	return fmt.Sprintf("%d", n)
}

func (tw *Writer) colorOk() string {
	if tw.color {
		return ansiGreen + "ok" + ansiReset
	}
	return "ok"
}

func (tw *Writer) colorNotOk() string {
	if tw.color {
		return ansiRed + "not ok" + ansiReset
	}
	return "not ok"
}

func (tw *Writer) colorSkip() string {
	if tw.color {
		return ansiYellow + "# SKIP" + ansiReset
	}
	return "# SKIP"
}

func (tw *Writer) colorTodo() string {
	if tw.color {
		return ansiYellow + "# TODO" + ansiReset
	}
	return "# TODO"
}

func (tw *Writer) colorWarn() string {
	if tw.color {
		return ansiYellow + "# WARN" + ansiReset
	}
	return "# WARN"
}

func (tw *Writer) colorBailOut() string {
	if tw.color {
		return ansiRed + "Bail out!" + ansiReset
	}
	return "Bail out!"
}

func (tw *Writer) Ok(description string) int {
	tw.clearStatusIfActive()
	tw.n++
	fmt.Fprintf(tw.w, "%s %s - %s\n", tw.colorOk(), tw.formatNumber(tw.n), description)
	return tw.n
}

func (tw *Writer) OkDiag(description string, diagnostics *Diagnostics) int {
	tw.clearStatusIfActive()
	tw.n++
	fmt.Fprintf(tw.w, "%s %s - %s\n", tw.colorOk(), tw.formatNumber(tw.n), description)
	writeDiagnostics(tw.w, diagnostics, tw.color)
	return tw.n
}

func (tw *Writer) HasFailures() bool {
	return tw.failed
}

func (tw *Writer) NotOk(description string, diagnostics map[string]string) int {
	tw.clearStatusIfActive()
	tw.n++
	tw.failed = true
	fmt.Fprintf(tw.w, "%s %s - %s\n", tw.colorNotOk(), tw.formatNumber(tw.n), description)
	if len(diagnostics) > 0 {
		fmt.Fprintln(tw.w, "  ---")
		keys := make([]string, 0, len(diagnostics))
		for k := range diagnostics {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := sanitizeYAMLValue(diagnostics[k], tw.color)
			if strings.Contains(v, "\n") {
				fmt.Fprintf(tw.w, "  %s: |\n", k)
				lines := strings.Split(v, "\n")
				for len(lines) > 0 && lines[len(lines)-1] == "" {
					lines = lines[:len(lines)-1]
				}
				for _, line := range lines {
					fmt.Fprintf(tw.w, "    %s\n", line)
				}
			} else {
				fmt.Fprintf(tw.w, "  %s: %s\n", k, v)
			}
		}
		fmt.Fprintln(tw.w, "  ...")
	}
	return tw.n
}

func (tw *Writer) Skip(description, reason string) int {
	tw.clearStatusIfActive()
	tw.n++
	fmt.Fprintf(tw.w, "%s %s - %s %s %s\n", tw.colorOk(), tw.formatNumber(tw.n), description, tw.colorSkip(), reason)
	return tw.n
}

func (tw *Writer) SkipDiag(description, reason string, diagnostics *Diagnostics) int {
	tw.clearStatusIfActive()
	tw.n++
	fmt.Fprintf(tw.w, "%s %s - %s %s %s\n", tw.colorOk(), tw.formatNumber(tw.n), description, tw.colorSkip(), reason)
	writeDiagnostics(tw.w, diagnostics, tw.color)
	return tw.n
}

func (tw *Writer) Todo(description, reason string) int {
	tw.clearStatusIfActive()
	tw.n++
	fmt.Fprintf(tw.w, "%s %s - %s %s %s\n", tw.colorNotOk(), tw.formatNumber(tw.n), description, tw.colorTodo(), reason)
	return tw.n
}

func (tw *Writer) Warn(description, reason string) int {
	tw.clearStatusIfActive()
	tw.n++
	fmt.Fprintf(tw.w, "%s %s - %s %s %s\n", tw.colorOk(), tw.formatNumber(tw.n), description, tw.colorWarn(), reason)
	return tw.n
}

func (tw *Writer) WarnNotOk(description, reason string) int {
	tw.clearStatusIfActive()
	tw.n++
	tw.failed = true
	fmt.Fprintf(tw.w, "%s %s - %s %s %s\n", tw.colorNotOk(), tw.formatNumber(tw.n), description, tw.colorWarn(), reason)
	return tw.n
}

func (tw *Writer) PlanAhead(n int) {
	fmt.Fprintf(tw.w, "1::%s\n", tw.formatNumber(n))
	tw.planEmitted = true
}

func (tw *Writer) Plan() {
	if tw.planEmitted {
		return
	}
	tw.planEmitted = true
	fmt.Fprintf(tw.w, "1::%s\n", tw.formatNumber(tw.n))
}

func (tw *Writer) PlanSkip(reason string) {
	tw.clearStatusIfActive()
	tw.planEmitted = true
	fmt.Fprintf(tw.w, "1::0 # SKIP %s\n", reason)
}

func (tw *Writer) BailOut(reason string) {
	tw.clearStatusIfActive()
	fmt.Fprintf(tw.w, "%s %s\n", tw.colorBailOut(), reason)
}

func (tw *Writer) Comment(text string) {
	fmt.Fprintf(tw.w, "# %s\n", text)
}

func (tw *Writer) Pragma(key string, enabled bool) {
	sign := "-"
	if enabled {
		sign = "+"
	}
	fmt.Fprintf(tw.w, "pragma %s%s\n", sign, key)
	if key == "streamed-output" && enabled {
		tw.streamedOutput = true
	}
	if key == "status-line" && enabled {
		tw.ttyBuildLastLine = true
	}
}

func (tw *Writer) StreamedOutput(text string) {
	fmt.Fprintf(tw.w, "# %s\n", text)
}

func (tw *Writer) EnableTTYBuildLastLine() {
	tw.ttyBuildLastLine = true
}

func (tw *Writer) UpdateLastLine(text string) {
	if tw.color {
		fmt.Fprintf(tw.w, "\r\033[2K\033[?7l# %s\033[?7h", text)
	} else {
		fmt.Fprintf(tw.w, "\r\033[2K# %s", text)
	}
	tw.statusLineActive = true
}

func (tw *Writer) FinishLastLine() {
	fmt.Fprint(tw.w, "\r\033[2K")
	tw.statusLineActive = false
}

func (tw *Writer) clearStatusIfActive() {
	if tw.statusLineActive {
		tw.FinishLastLine()
	}
}

type Diagnostics struct {
	Message  string
	Severity string
	File     string
	Line     int
	Extras   map[string]any
}

func sanitizeYAMLValue(value string, color bool) string {
	var stripped string
	if color {
		stripped = stripNonSGR(value)
	} else {
		stripped = stripANSI(value)
	}
	lines := strings.Split(stripped, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}

func writeDiagnostics(w io.Writer, d *Diagnostics, color bool) {
	if d == nil {
		return
	}

	entries := make([]struct{ k, v string }, 0, 8)

	if d.File != "" {
		entries = append(entries, struct{ k, v string }{"file", d.File})
	}
	if d.Line != 0 {
		entries = append(entries, struct{ k, v string }{"line", fmt.Sprintf("%d", d.Line)})
	}
	if d.Message != "" {
		entries = append(entries, struct{ k, v string }{"message", sanitizeYAMLValue(d.Message, color)})
	}
	if d.Severity != "" {
		entries = append(entries, struct{ k, v string }{"severity", d.Severity})
	}

	if len(d.Extras) > 0 {
		keys := make([]string, 0, len(d.Extras))
		for k := range d.Extras {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			entries = append(entries, struct{ k, v string }{k, sanitizeYAMLValue(fmt.Sprintf("%v", d.Extras[k]), color)})
		}
	}

	if len(entries) == 0 {
		return
	}

	fmt.Fprintln(w, "  ---")
	for _, e := range entries {
		if strings.Contains(e.v, "\n") {
			fmt.Fprintf(w, "  %s: |\n", e.k)
			lines := strings.Split(e.v, "\n")
			for len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			for _, line := range lines {
				fmt.Fprintf(w, "    %s\n", line)
			}
		} else {
			fmt.Fprintf(w, "  %s: %s\n", e.k, e.v)
		}
	}
	fmt.Fprintln(w, "  ...")
}

type indentWriter struct {
	w      io.Writer
	prefix string
}

func (iw *indentWriter) Write(p []byte) (int, error) {
	lines := strings.Split(string(p), "\n")
	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			break
		}
		out := iw.prefix + line + "\n"
		if _, err := iw.w.Write([]byte(out)); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (tw *Writer) Subtest(name string) *Writer {
	prefix := "    "
	fmt.Fprintf(tw.w, "%s# Subtest: %s\n", prefix, name)
	iw := &indentWriter{w: tw.w, prefix: prefix}
	child := &Writer{
		w:       iw,
		depth:   tw.depth + 1,
		color:   tw.color,
		locale:  tw.locale,
		printer: tw.printer,
	}
	if tw.printer != nil {
		fmt.Fprintf(iw, "pragma +locale-formatting:%s\n", tw.locale)
	}
	child.streamedOutput = tw.streamedOutput
	return child
}

type TestPoint struct {
	Description string
	Ok          bool
	Skip        string
	Todo        string
	Warn        string
	Diagnostics *Diagnostics
	Subtests    func(*Writer)
}

func (tw *Writer) WriteAll(tests iter.Seq[TestPoint]) {
	for tp := range tests {
		if tp.Subtests != nil {
			child := tw.Subtest(tp.Description)
			tp.Subtests(child)
			if !child.planEmitted {
				child.Plan()
			}
			tw.Ok(tp.Description)
		} else if tp.Skip != "" {
			tw.SkipDiag(tp.Description, tp.Skip, tp.Diagnostics)
		} else if tp.Todo != "" {
			tw.Todo(tp.Description, tp.Todo)
		} else if tp.Warn != "" {
			if tp.Ok {
				tw.Warn(tp.Description, tp.Warn)
			} else {
				tw.WarnNotOk(tp.Description, tp.Warn)
			}
		} else if tp.Ok {
			tw.n++
			fmt.Fprintf(tw.w, "%s %s - %s\n", tw.colorOk(), tw.formatNumber(tw.n), tp.Description)
			writeDiagnostics(tw.w, tp.Diagnostics, tw.color)
		} else {
			tw.n++
			tw.failed = true
			fmt.Fprintf(tw.w, "%s %s - %s\n", tw.colorNotOk(), tw.formatNumber(tw.n), tp.Description)
			writeDiagnostics(tw.w, tp.Diagnostics, tw.color)
		}
	}
	if !tw.planEmitted {
		tw.Plan()
	}
}

// ExecTestResult represents the outcome of a single command/recipe execution.
// Mirrors rust-crap's TestResult struct used by just-us.
type ExecTestResult struct {
	Number       int    // explicit test number for display; 0 means use auto-increment
	Name         string
	OK           bool
	Directive    string // generic comment (e.g. recipe doc), not a SKIP/TODO/WARN directive
	ErrorMessage string
	ExitCode     *int
	Output       string
	SuppressYAML bool
}

// EmitTestResult writes a test point with optional YAML diagnostics.
// Mirrors rust-crap's test_point() method.
func (tw *Writer) EmitTestResult(r *ExecTestResult) int {
	tw.clearStatusIfActive()
	tw.n++
	if !r.OK {
		tw.failed = true
	}

	status := tw.colorOk()
	if !r.OK {
		status = tw.colorNotOk()
	}

	// Use explicit number for display if provided, but always increment internal counter.
	displayNum := tw.n
	if r.Number > 0 {
		displayNum = r.Number
	}
	num := tw.formatNumber(displayNum)

	if r.Directive != "" {
		fmt.Fprintf(tw.w, "%s %s - %s # %s\n", status, num, r.Name, r.Directive)
	} else {
		fmt.Fprintf(tw.w, "%s %s - %s\n", status, num, r.Name)
	}

	if !r.SuppressYAML && hasExecYAMLContent(r) {
		fmt.Fprintln(tw.w, "  ---")
		if r.ErrorMessage != "" {
			writeExecYAMLField(tw.w, "message", sanitizeYAMLValue(r.ErrorMessage, tw.color))
		}
		if !r.OK {
			fmt.Fprintln(tw.w, "  severity: fail")
		}
		if r.ExitCode != nil {
			fmt.Fprintf(tw.w, "  exitcode: %d\n", *r.ExitCode)
		}
		if r.Output != "" {
			writeExecYAMLField(tw.w, "output", sanitizeYAMLValue(r.Output, tw.color))
		}
		fmt.Fprintln(tw.w, "  ...")
	}

	return tw.n
}

// hasExecYAMLContent returns true if the result has content for a YAML block.
// Any failing test gets at least severity: fail.
func hasExecYAMLContent(r *ExecTestResult) bool {
	return !r.OK || r.ErrorMessage != "" || r.ExitCode != nil || r.Output != ""
}

func writeExecYAMLField(w io.Writer, key, value string) {
	if strings.Contains(value, "\n") {
		fmt.Fprintf(w, "  %s: |\n", key)
		lines := strings.Split(value, "\n")
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		for _, line := range lines {
			fmt.Fprintf(w, "    %s\n", line)
		}
	} else {
		fmt.Fprintf(w, "  %s: %s\n", key, value)
	}
}

// StatusLineProcessor is a stateful byte buffer that splits PTY output on
// \r and \n boundaries, trims whitespace, and filters out lines with no
// visible content (ANSI-only or blank).
type StatusLineProcessor struct {
	buf []byte
}

// NewStatusLineProcessor creates a new StatusLineProcessor.
func NewStatusLineProcessor() *StatusLineProcessor {
	return &StatusLineProcessor{}
}

// Feed appends chunk to the internal buffer and returns all complete lines
// that have visible content.
func (p *StatusLineProcessor) Feed(chunk []byte) []string {
	p.buf = append(p.buf, chunk...)
	var lines []string
	for {
		pos := -1
		for i, b := range p.buf {
			if b == '\n' || b == '\r' {
				pos = i
				break
			}
		}
		if pos < 0 {
			break
		}
		line := string(p.buf[:pos])
		p.buf = p.buf[pos+1:]
		trimmed := strings.TrimSpace(line)
		if HasVisibleContent(trimmed) {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// FeedStatusBytes is a convenience method that feeds chunk through a
// StatusLineProcessor and calls UpdateLastLine for each visible line.
func (tw *Writer) FeedStatusBytes(chunk []byte) {
	if tw.statusProcessor == nil {
		tw.statusProcessor = NewStatusLineProcessor()
	}
	for _, line := range tw.statusProcessor.Feed(chunk) {
		tw.UpdateLastLine(line)
	}
}
