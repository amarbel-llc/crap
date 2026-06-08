// Package crap hosts the ndjson-crap producers that back the `::`
// (large-colon) toolkit: converters that turn test-runner and command output
// into ndjson-crap (see go-crap/ndjsoncrap). The canonical wire format is
// ndjson-crap and the canonical presenter is the viewport (go-crap/viewport,
// the crap-present binary); these producers only emit records.
package crap

import (
	"io"

	"github.com/amarbel-llc/crap/go-crap/v2/ndjsoncrap"
)

// writeResultStream emits a complete result-family ndjson-crap stream: a Meta
// header, a plan, the top-level test records (renumbered 1..N), and a
// trailing summary. Per the schema, only top-level records are counted in the
// summary; nested subtests are not.
func writeResultStream(w io.Writer, title, source string, tests []ndjsoncrap.Test) error {
	nw := ndjsoncrap.NewWriter(w)
	if err := nw.Write(ndjsoncrap.Meta{Title: title, Source: source}); err != nil {
		return err
	}
	if err := nw.Write(ndjsoncrap.Plan{Count: len(tests)}); err != nil {
		return err
	}
	var s ndjsoncrap.Summary
	for i := range tests {
		tests[i].N = i + 1
		if err := nw.Write(tests[i]); err != nil {
			return err
		}
		tally(&s, tests[i])
	}
	s.Total = s.Passed + s.Failed + s.Skipped + s.Todo
	s.PlanCount = len(tests)
	s.Valid = true
	return nw.Write(s)
}

// tally folds one top-level test record into the running summary counts.
func tally(s *ndjsoncrap.Summary, t ndjsoncrap.Test) {
	switch {
	case t.Directive != nil && t.Directive.Kind == "skip":
		s.Skipped++
	case t.Directive != nil && t.Directive.Kind == "todo":
		s.Todo++
	case t.OK:
		s.Passed++
	default:
		s.Failed++
	}
}

// skipDirective is a small constructor for a skip directive.
func skipDirective(reason string) *ndjsoncrap.Directive {
	return &ndjsoncrap.Directive{Kind: "skip", Reason: reason}
}

// strPtr returns a pointer to s, or nil for the empty string. Used for the
// nullable Output field.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
