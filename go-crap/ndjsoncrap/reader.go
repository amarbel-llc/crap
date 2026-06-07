package ndjsoncrap

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// maxLineBytes bounds a single ndjson-crap record. Output records can carry
// large base64 blobs, so this is generous.
const maxLineBytes = 64 << 20 // 64 MiB

// Reader decodes an ndjson-crap stream one record at a time. It is tolerant:
// blank lines are skipped, just-us discriminators are normalized to their
// canonical equivalents, and unrecognized record types decode to Unknown
// rather than erroring.
type Reader struct {
	sc *bufio.Scanner
}

// NewReader builds a Reader over r.
func NewReader(r io.Reader) *Reader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	return &Reader{sc: sc}
}

// Next returns the next record, or io.EOF when the stream is exhausted. A
// line that is not valid JSON returns an error; an unrecognized "type"
// returns an Unknown record (not an error).
func (r *Reader) Next() (Record, error) {
	for r.sc.Scan() {
		line := bytes.TrimSpace(r.sc.Bytes())
		if len(line) == 0 {
			continue
		}
		return Decode(line)
	}
	if err := r.sc.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

// envelope is just enough to read the discriminator.
type envelope struct {
	Type string `json:"type"`
}

// Decode parses a single ndjson-crap record from a JSON object. It accepts
// both canonical ndjson-crap and the just-us --events-fd discriminators
// (recipe_start, recipe_command, recipe_complete, and a plan carrying
// recipe_count). Unknown types decode to Unknown.
func Decode(data []byte) (Record, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("ndjsoncrap: invalid JSON record: %w", err)
	}
	switch env.Type {
	case "crap":
		var m Meta
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, decodeErr("crap", err)
		}
		return m, nil

	case "plan":
		// Accept canonical {count} and just-us {recipe_count, version}.
		var p Plan
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, decodeErr("plan", err)
		}
		if p.Count == 0 {
			var je struct {
				RecipeCount int `json:"recipe_count"`
			}
			if json.Unmarshal(data, &je) == nil && je.RecipeCount != 0 {
				p.Count = je.RecipeCount
			}
		}
		return p, nil

	case "test":
		var t Test
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, decodeErr("test", err)
		}
		return t, nil

	case "bailout":
		var b Bailout
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, decodeErr("bailout", err)
		}
		return b, nil

	case "summary":
		var s Summary
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, decodeErr("summary", err)
		}
		return s, nil

	case "node_start", "recipe_start":
		var n NodeStart
		if err := json.Unmarshal(data, &n); err != nil {
			return nil, decodeErr(env.Type, err)
		}
		n.Type = "node_start"
		return n, nil

	case "command", "recipe_command":
		var c Command
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, decodeErr(env.Type, err)
		}
		c.Type = "command"
		return c, nil

	case "output":
		var o Output
		if err := json.Unmarshal(data, &o); err != nil {
			return nil, decodeErr("output", err)
		}
		if o.Format == "" {
			o.Format = FormatUTF8
		}
		return o, nil

	case "node_end", "recipe_complete":
		var n NodeEnd
		if err := json.Unmarshal(data, &n); err != nil {
			return nil, decodeErr(env.Type, err)
		}
		n.Type = "node_end"
		return n, nil

	default:
		// Forward compatibility: retain the record verbatim, do not error.
		return Unknown{Type: env.Type, Raw: append(json.RawMessage(nil), data...)}, nil
	}
}

func decodeErr(kind string, err error) error {
	return fmt.Errorf("ndjsoncrap: decoding %q record: %w", kind, err)
}
