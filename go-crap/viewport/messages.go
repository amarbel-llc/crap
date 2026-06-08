// Package viewport is the CRAP-2 TTY presenter: a bubbletea model that
// renders a spinner + rolling log tail and, when a total is known, a
// progress bar, plus a driver that turns an ndjson-crap stream into the
// messages that feed it.
//
// The Model and message vocabulary are extracted from cutting-garden's
// internal/capture_viewport (itself a WET copy of purse-first FDR 0010's
// operation_viewport), so the vocabulary is shared across the ecosystem.
// The cutting-garden-specific event adapter is replaced here by driver.go,
// which consumes ndjson-crap records directly.
package viewport

// Message types are delivered to the Model via tea.Program.Send.

// LogLine appends one line to the rolling tail.
type LogLine struct{ Text string }

// OperationStarted (re)labels the header and, when Total > 0, arms the bar.
type OperationStarted struct {
	Name  string // label, e.g. "go test ./..."
	Index int    // 1-based position in a batch; 0 when not batched
	Total int    // total operations; 0 = unknown (indeterminate)
}

// OperationProgress advances the bar numerator. Current/Total drive the
// item-count bar; Bytes/BytesTotal drive the byte bar. A consumer may set
// either pair, both, or neither — the Model's View precedence (items >
// byte bar > indeterminate byte counter) decides what renders.
type OperationProgress struct {
	Current    int   // item numerator
	Total      int   // item denominator; 0 leaves the existing total unchanged
	Bytes      int64 // bytes processed so far
	BytesTotal int64 // total bytes; 0 leaves the existing byte total unchanged
}

// OperationDone ends one operation: success collapses its tail, failure
// holds it and records the error.
type OperationDone struct{ Err error }

// PhaseStarted begins a phase: retitle the header and reset all per-phase
// live state (tail, bar, bytes).
type PhaseStarted struct{ Description string }

// DirectiveView / VerdictView mirror the directive/verdict shape for the
// view layer (the driver converts ndjson-crap records into these).
type DirectiveView struct{ Kind, Reason string }

// VerdictView is the resolved verdict for a finished phase.
type VerdictView struct {
	OK         bool
	Directive  *DirectiveView
	Diagnostic map[string]any
}

// PhaseEnded completes a phase: persist a verdict line above the live
// region (tea.Println) and reset per-phase state. Description is carried
// here too so an end without a start still renders something sensible.
type PhaseEnded struct {
	Description string
	Verdict     VerdictView
}

// ItemFailed persists one failed operation item's verdict (✗ label +
// diagnostic) via tea.Println WITHOUT resetting the operation's live region.
// Unlike PhaseEnded (which resetPhase()s the bars+tail and holds the whole
// tail above its verdict), ItemFailed leaves the progress bars and rolling
// tail intact so the operation keeps advancing after a mid-run failure
// (crap RFC 0001 §7).
type ItemFailed struct {
	Label      string
	Diagnostic map[string]any
}

// BatchDone ends the whole run and quits the program.
type BatchDone struct{ Err error }
