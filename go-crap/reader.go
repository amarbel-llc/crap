package crap

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type readerState int

const (
	stateStart readerState = iota
	stateHeader
	stateBody
	stateYAML
	stateDone
)

type frame struct {
	depth            int
	planSeen         bool
	planCount        int
	planLine         int
	testCount        int
	lastTestNumber   int
	streamedOutput   bool
	localeSep        string // grouping separator for active locale, empty = no locale
	inOutputBlock       bool
	pendingOutputBlock  bool // true after output block body ends, before correlated test point
	outputBlockNum      int
	outputBlockDesc     string
}

// Reader is a streaming CRAP-2 parser and validator.
type Reader struct {
	scanner          *bufio.Scanner
	state            readerState
	lineNum          int
	stack            []frame
	diags            []Diagnostic
	done             bool
	bailed           bool
	yamlBuf          map[string]string
	yamlBlockKey     string // current block literal key, empty = not in block
	yamlBlockIndent  int    // expected indent for block literal continuation lines
	lastWasTestPoint bool
	passed           int
	failed           int
	skipped          int
	todo             int
	warned           int
}

// NewReader creates a new CRAP-2 reader from the given input.
func NewReader(r io.Reader) *Reader {
	return &Reader{
		scanner: bufio.NewScanner(r),
		stack:   []frame{{depth: 0}},
	}
}

func (r *Reader) currentFrame() *frame {
	return &r.stack[len(r.stack)-1]
}

func localeGroupingSeparator(tag language.Tag) string {
	p := message.NewPrinter(tag)
	formatted := p.Sprintf("%d", 1234)
	// "1,234" for en-US, "1.234" for de-DE, "1 234" for fr-FR
	runes := []rune(formatted)
	if len(runes) >= 2 {
		return string(runes[1])
	}
	return ""
}

func (r *Reader) addDiag(severity Severity, rule, message string) {
	r.diags = append(r.diags, Diagnostic{
		Line:     r.lineNum,
		Severity: severity,
		Rule:     rule,
		Message:  message,
	})
}

// Next returns the next parsed event from the CRAP stream.
// Returns io.EOF when the stream is exhausted.
func (r *Reader) Next() (Event, error) {
	for r.scanner.Scan() {
		r.lineNum++
		original := r.scanner.Text()

		// Strip ANSI CSI escape sequences before parsing, per the
		// ANSI Display Hints amendment. This ensures colored CRAP
		// streams parse identically to uncolored streams.
		raw := stripANSI(original)

		// Determine indentation depth
		trimmed := strings.TrimLeft(raw, " ")
		indent := len(raw) - len(trimmed)
		depth := indent / 4

		// Handle YAML block state
		if r.state == stateYAML {
			yamlBaseIndent := (r.currentFrame().depth * 4) + 2

			// Handle block literal continuation
			if r.yamlBlockKey != "" {
				lineIndent := len(raw) - len(strings.TrimLeft(raw, " "))

				if raw == strings.Repeat(" ", yamlBaseIndent)+"..." {
					// End of YAML block — finalize block literal, fall through to end marker
					r.yamlBlockKey = ""
				} else if lineIndent <= yamlBaseIndent && strings.TrimSpace(raw) != "" {
					// Not a continuation — end block, fall through to key:value parsing
					r.yamlBlockKey = ""
				} else {
					// Continuation line — strip the block indent, use original to preserve ANSI
					stripped := original
					if len(stripped) >= r.yamlBlockIndent {
						stripped = stripped[r.yamlBlockIndent:]
					}
					if r.yamlBuf[r.yamlBlockKey] != "" {
						r.yamlBuf[r.yamlBlockKey] += "\n"
					}
					r.yamlBuf[r.yamlBlockKey] += strings.TrimRight(stripped, " ")
					continue
				}
			}

			if raw == strings.Repeat(" ", yamlBaseIndent)+"..." {
				r.state = stateBody
				yaml := r.yamlBuf
				r.yamlBuf = nil
				return Event{
					Type:  EventYAMLDiagnostic,
					Line:  r.lineNum,
					Depth: r.currentFrame().depth,
					Raw:   raw,
					YAML:  yaml,
				}, nil
			}
			// Accumulate YAML content using the original line to
			// preserve ANSI SGR sequences in values, per the ANSI
			// in YAML Output Blocks amendment.
			content := original
			if len(content) >= yamlBaseIndent {
				content = content[yamlBaseIndent:]
			}
			parts := strings.SplitN(content, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				if val == "|" {
					r.yamlBlockKey = key
					r.yamlBlockIndent = yamlBaseIndent + 2
					r.yamlBuf[key] = ""
				} else {
					r.yamlBuf[key] = val
				}
			}
			continue
		}

		// Handle Output Block body lines: when inside an output block,
		// 4-space indented lines relative to the frame are body lines.
		// Unindented lines end the block and fall through to normal parsing.
		if r.currentFrame().inOutputBlock {
			blockIndent := (r.currentFrame().depth * 4) + 4
			if indent >= blockIndent && strings.TrimSpace(raw) != "" {
				// Body line — strip the 4-space block indent, use original to preserve ANSI
				content := original
				if len(content) >= blockIndent {
					content = content[blockIndent:]
				}
				r.lastWasTestPoint = false
				return Event{
					Type:       EventOutputLine,
					Line:       r.lineNum,
					Depth:      r.currentFrame().depth,
					Raw:        raw,
					OutputLine: strings.TrimRight(content, " "),
				}, nil
			}
			// Not indented enough or blank — end the output block,
			// fall through to normal classification.
			r.currentFrame().inOutputBlock = false
			r.currentFrame().pendingOutputBlock = true
		}

		// Handle depth changes for subtests
		if depth > r.currentFrame().depth {
			r.stack = append(r.stack, frame{depth: depth})
		}
		for depth < r.currentFrame().depth && len(r.stack) > 1 {
			completed := r.stack[len(r.stack)-1]
			r.stack = r.stack[:len(r.stack)-1]
			if completed.planSeen && completed.testCount != completed.planCount {
				r.addDiag(SeverityError, "plan-count-mismatch",
					"subtest plan count mismatch: plan declared "+
						strconv.Itoa(completed.planCount)+
						" tests but "+strconv.Itoa(completed.testCount)+" ran")
			}
		}

		kind := classifyLine(trimmed)

		switch kind {
		case lineVersion:
			if r.state != stateStart {
				if r.currentFrame().depth > 0 {
					r.addDiag(SeverityWarning, "subtest-version",
						"subtests should omit version line for TAP13 compatibility")
				}
			}
			r.state = stateHeader
			r.lastWasTestPoint = false
			return Event{Type: EventVersion, Line: r.lineNum, Depth: depth, Raw: raw}, nil

		case linePlan:
			f := r.currentFrame()
			if f.planSeen {
				r.addDiag(SeverityError, "plan-duplicate", "duplicate plan line")
			}
			plan, _ := parsePlanWithSep(trimmed, f.localeSep)
			f.planSeen = true
			f.planCount = plan.Count
			f.planLine = r.lineNum
			if r.state == stateStart {
				r.addDiag(SeverityError, "version-required", "first line must be CRAP-2")
			}
			if r.state == stateHeader {
				r.state = stateBody
			}
			r.lastWasTestPoint = false
			return Event{Type: EventPlan, Line: r.lineNum, Depth: depth, Raw: raw, Plan: &plan}, nil

		case lineTestPoint:
			if r.state == stateStart {
				r.addDiag(SeverityError, "version-required", "first line must be CRAP-2")
			}
			r.state = stateBody
			f := r.currentFrame()
			tp, tpDiags := parseTestPointWithSep(trimmed, f.localeSep)
			r.diags = append(r.diags, tpDiags...)
			f.testCount++

			// Validate Output Block correlation
			if f.pendingOutputBlock {
				f.pendingOutputBlock = false
				if tp.Number != f.outputBlockNum {
					r.addDiag(SeverityError, "output-block-id-mismatch",
						"output block header declared test "+strconv.Itoa(f.outputBlockNum)+
							" but closing test point is "+strconv.Itoa(tp.Number))
				}
				if tp.Description != f.outputBlockDesc {
					r.addDiag(SeverityWarning, "output-block-description-mismatch",
						"output block description "+strconv.Quote(f.outputBlockDesc)+
							" differs from test point "+strconv.Quote(tp.Description))
				}
			}

			if tp.Number == 0 {
				r.addDiag(SeverityWarning, "test-number-missing", "test point without explicit number")
			} else {
				if tp.Number != f.lastTestNumber+1 {
					r.addDiag(SeverityWarning, "test-number-sequence",
						"test number "+strconv.Itoa(tp.Number)+" out of sequence, expected "+strconv.Itoa(f.lastTestNumber+1))
				}
				f.lastTestNumber = tp.Number
			}

			// Track pass/fail/skip/todo/warn
			switch tp.Directive {
			case DirectiveSkip:
				r.skipped++
			case DirectiveTodo:
				r.todo++
			case DirectiveWarn:
				r.warned++
				if tp.OK {
					r.passed++
				} else {
					r.failed++
				}
			default:
				if tp.OK {
					r.passed++
				} else {
					r.failed++
				}
			}

			r.lastWasTestPoint = true
			return Event{Type: EventTestPoint, Line: r.lineNum, Depth: depth, Raw: raw, TestPoint: &tp}, nil

		case lineYAMLStart:
			if !r.lastWasTestPoint {
				r.addDiag(SeverityWarning, "yaml-orphan", "YAML block not following a test point")
			}
			expectedIndent := (r.currentFrame().depth * 4) + 2
			if indent != expectedIndent {
				r.addDiag(SeverityError, "yaml-indent",
					"YAML block must be indented by "+strconv.Itoa(expectedIndent)+" spaces")
			}
			r.state = stateYAML
			r.yamlBuf = make(map[string]string)
			r.lastWasTestPoint = false
			continue

		case lineYAMLEnd:
			r.addDiag(SeverityError, "yaml-unclosed", "unexpected YAML end marker without opening ---")
			r.lastWasTestPoint = false
			continue

		case lineBailOut:
			b := parseBailOut(trimmed)
			r.bailed = true
			r.lastWasTestPoint = false
			return Event{Type: EventBailOut, Line: r.lineNum, Depth: depth, Raw: raw, BailOut: &b}, nil

		case linePragma:
			p := parsePragma(trimmed)
			if p.Key == "streamed-output" {
				if p.Enabled {
					r.currentFrame().streamedOutput = true
				} else if r.currentFrame().streamedOutput {
					r.addDiag(SeverityError, "streamed-output-deactivation",
						"pragma -streamed-output is not permitted after activation")
				}
			}
			if strings.HasPrefix(p.Key, "locale-formatting:") {
				tag := strings.TrimPrefix(p.Key, "locale-formatting:")
				langTag, err := language.Parse(tag)
				if err == nil {
					r.currentFrame().localeSep = localeGroupingSeparator(langTag)
				}
			}
			r.lastWasTestPoint = false
			return Event{Type: EventPragma, Line: r.lineNum, Depth: depth, Raw: raw, Pragma: &p}, nil

		case lineOutputHeader:
			oh, _ := parseOutputHeaderWithSep(trimmed, r.currentFrame().localeSep)
			f := r.currentFrame()
			f.inOutputBlock = true
			f.outputBlockNum = oh.Number
			f.outputBlockDesc = oh.Description
			r.lastWasTestPoint = false
			return Event{
				Type:         EventOutputHeader,
				Line:         r.lineNum,
				Depth:        depth,
				Raw:          raw,
				OutputHeader: &oh,
			}, nil

		case lineSubtestComment:
			comment := strings.TrimPrefix(trimmed, "#")
			comment = strings.TrimSpace(comment)
			r.lastWasTestPoint = false
			return Event{Type: EventComment, Line: r.lineNum, Depth: depth, Raw: raw, Comment: comment}, nil

		case lineComment:
			comment := strings.TrimPrefix(trimmed, "#")
			comment = strings.TrimSpace(comment)
			r.lastWasTestPoint = false
			return Event{Type: EventComment, Line: r.lineNum, Depth: depth, Raw: raw, Comment: comment, StreamedOutput: r.currentFrame().streamedOutput}, nil

		case lineEmpty:
			r.lastWasTestPoint = false
			continue

		default:
			r.lastWasTestPoint = false
			return Event{Type: EventUnknown, Line: r.lineNum, Depth: depth, Raw: raw}, nil
		}
	}

	if !r.done {
		r.done = true
		r.finalize()
	}
	return Event{}, io.EOF
}

func (r *Reader) finalize() {
	if r.state == stateStart {
		r.addDiag(SeverityError, "version-required", "first line must be CRAP-2")
	}
	if r.state == stateYAML {
		r.addDiag(SeverityError, "yaml-unclosed", "YAML block not closed at end of input")
	}

	// Validate all remaining stack frames
	for i := len(r.stack) - 1; i >= 0; i-- {
		f := r.stack[i]
		if !f.planSeen && !r.bailed {
			if f.depth == 0 {
				r.addDiag(SeverityError, "plan-required", "no plan line found")
			}
		}
		if f.planSeen && f.testCount != f.planCount && !r.bailed {
			r.addDiag(SeverityError, "plan-count-mismatch",
				"plan declared "+strconv.Itoa(f.planCount)+" tests but "+strconv.Itoa(f.testCount)+" ran")
		}
	}
}

// Diagnostics returns all validation problems found so far.
func (r *Reader) Diagnostics() []Diagnostic {
	if !r.done {
		for {
			if _, err := r.Next(); err != nil {
				break
			}
		}
	}
	return r.diags
}

// Summary returns aggregate results after the stream is fully consumed.
func (r *Reader) Summary() Summary {
	if !r.done {
		for {
			if _, err := r.Next(); err != nil {
				break
			}
		}
	}

	s := Summary{
		Version:   2,
		BailedOut: r.bailed,
		Passed:    r.passed,
		Failed:    r.failed,
		Skipped:   r.skipped,
		Todo:      r.todo,
		Warned:    r.warned,
	}

	if len(r.stack) > 0 {
		root := r.stack[0]
		s.PlanCount = root.planCount
		s.TotalTests = root.testCount
	}

	hasErrors := false
	for _, d := range r.diags {
		if d.Severity == SeverityError {
			hasErrors = true
			break
		}
	}
	s.Valid = !hasErrors

	return s
}

// ReadFrom reads the entire CRAP stream, consuming all events and
// collecting diagnostics.
func (r *Reader) ReadFrom(src io.Reader) (int64, error) {
	r.scanner = bufio.NewScanner(src)
	r.lineNum = 0
	r.state = stateStart
	r.stack = []frame{{depth: 0}}
	r.diags = nil
	r.done = false

	for {
		if _, err := r.Next(); err != nil {
			break
		}
	}
	return int64(r.lineNum), nil
}

// WriteTo writes the validation report to the given writer.
func (r *Reader) WriteTo(w io.Writer) (int64, error) {
	if !r.done {
		for {
			if _, err := r.Next(); err != nil {
				break
			}
		}
	}

	var total int64
	summary := r.Summary()

	for _, d := range r.diags {
		line := fmt.Sprintf("line %d: %s: [%s] %s\n", d.Line, d.Severity, d.Rule, d.Message)
		n, err := io.WriteString(w, line)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	status := "valid"
	if !summary.Valid {
		status = "invalid"
	}
	line := fmt.Sprintf("\n%s: %d tests (%d passed, %d failed, %d skipped, %d todo, %d warned)\n",
		status, summary.TotalTests, summary.Passed, summary.Failed, summary.Skipped, summary.Todo, summary.Warned)
	n, err := io.WriteString(w, line)
	total += int64(n)
	return total, err
}
