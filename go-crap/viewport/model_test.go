package viewport

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// updateModel applies one message and returns the concrete Model. The Model's
// internal state (tail, phase) is unexported, so these white-box tests live
// in-package — which is exactly why they belong here now that the Model has
// moved out of cutting-garden's capture_viewport.
func updateModel(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	got, _ := m.Update(msg)
	out, ok := got.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", got)
	}
	return out
}

func TestModel_ConsecutiveIdenticalLogLinesCollapse(t *testing.T) {
	m := New()
	m = updateModel(t, m, LogLine{Text: "same"})
	m = updateModel(t, m, LogLine{Text: "same"})
	if len(m.tail) != 1 {
		t.Fatalf("consecutive identical LogLines should collapse to one; tail = %v", m.tail)
	}
}

func TestModel_DistinctLogLinesAllLand(t *testing.T) {
	m := New()
	for _, s := range []string{"a", "b", "a"} { // non-consecutive repeat still lands
		m = updateModel(t, m, LogLine{Text: s})
	}
	if len(m.tail) != 3 {
		t.Fatalf("distinct/non-consecutive lines should all land; tail = %v", m.tail)
	}
}

func TestModel_TailCapsAtTailMax(t *testing.T) {
	m := New(WithTailLines(2))
	for _, s := range []string{"1", "2", "3", "4"} {
		m = updateModel(t, m, LogLine{Text: s})
	}
	if len(m.tail) != 2 || m.tail[0] != "3" || m.tail[1] != "4" {
		t.Fatalf("tail should keep only the last 2 lines, got %v", m.tail)
	}
}

func TestModel_PhaseStartResetsTail(t *testing.T) {
	m := New()
	m = updateModel(t, m, LogLine{Text: "noise"})
	m = updateModel(t, m, PhaseStarted{Description: "build"})
	if len(m.tail) != 0 || m.phase != "build" {
		t.Fatalf("PhaseStarted should reset tail and set phase; tail=%v phase=%q", m.tail, m.phase)
	}
}

func TestModel_RenderPhaseEndVariants(t *testing.T) {
	m := New()
	if got := m.renderPhaseEnd(PhaseEnded{Description: "ok one", Verdict: VerdictView{OK: true}}); !strings.Contains(got, "✓ ok one") {
		t.Fatalf("ok verdict render: %q", got)
	}
	skip := m.renderPhaseEnd(PhaseEnded{Description: "net", Verdict: VerdictView{Directive: &DirectiveView{Kind: "skip", Reason: "offline"}}})
	if !strings.Contains(skip, "↷ net # SKIP offline") {
		t.Fatalf("skip verdict render: %q", skip)
	}
	// A failure holds the current tail above the verdict line and appends a
	// sorted diagnostic.
	m = updateModel(t, m, LogLine{Text: "boom"})
	fail := m.renderPhaseEnd(PhaseEnded{Description: "bad", Verdict: VerdictView{Diagnostic: map[string]any{"message": "nope"}}})
	if !strings.Contains(fail, "│ boom") || !strings.Contains(fail, "✗ bad") || !strings.Contains(fail, "message: nope") {
		t.Fatalf("fail verdict render: %q", fail)
	}
}
