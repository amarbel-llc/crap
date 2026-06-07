package ndjsoncrap

import (
	"encoding/json"
	"io"
)

// Writer emits ndjson-crap, one JSON record per line. HTML escaping is
// disabled so diagnostic text and command strings round-trip unmangled.
type Writer struct {
	enc *json.Encoder
}

// NewWriter builds a Writer over w.
func NewWriter(w io.Writer) *Writer {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &Writer{enc: enc}
}

// Write stamps the canonical "type" discriminator (and, for Meta, default
// schema versions) on rec and encodes it as one line. Callers may pass a
// record with Type unset; Write fills it in.
func (w *Writer) Write(rec Record) error {
	return w.enc.Encode(withType(rec))
}

// WriteHeader emits a Meta header with the current schema versions.
func (w *Writer) WriteHeader(title, source string) error {
	return w.Write(Meta{Title: title, Source: source})
}

// withType returns rec with its Type field stamped to the canonical value
// (and Meta's version fields defaulted). Records are value types, so this
// returns a copy rather than mutating the caller's value.
func withType(rec Record) Record {
	switch r := rec.(type) {
	case Meta:
		r.Type = "crap"
		if r.Version == 0 {
			r.Version = CrapVersion
		}
		if r.Ndjson == 0 {
			r.Ndjson = NdjsonVersion
		}
		return r
	case Plan:
		r.Type = "plan"
		return r
	case Test:
		return stampTest(r)
	case Bailout:
		r.Type = "bailout"
		return r
	case Summary:
		r.Type = "summary"
		// The schema specifies an array; emit [] rather than null when empty
		// so the wire shape matches tap-ndjson(7).
		if r.Diagnostics == nil {
			r.Diagnostics = []SummaryDiagnostic{}
		}
		return r
	case NodeStart:
		r.Type = "node_start"
		return r
	case Command:
		r.Type = "command"
		return r
	case Output:
		r.Type = "output"
		if r.Format == "" {
			r.Format = FormatUTF8
		}
		return r
	case NodeEnd:
		r.Type = "node_end"
		return r
	default:
		return rec
	}
}

// stampTest sets Type="test" on a test record and, recursively, on every
// nested subtest, so subtest records carry the discriminator the schema
// requires.
func stampTest(t Test) Test {
	t.Type = "test"
	if t.Subtest != nil {
		subs := make([]Test, len(t.Subtest))
		for i, s := range t.Subtest {
			subs[i] = stampTest(s)
		}
		t.Subtest = subs
	}
	return t
}
