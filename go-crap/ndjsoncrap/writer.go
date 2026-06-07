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
		r.Type = "test"
		return r
	case Bailout:
		r.Type = "bailout"
		return r
	case Summary:
		r.Type = "summary"
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
