// Package ndjsoncrap defines ndjson-crap: the canonical newline-delimited
// JSON wire format for CRAP-2 streams, and a tolerant reader/writer for it.
//
// ndjson-crap is a deliberate union ("drying") of the divergent
// newline-delimited-JSON schemas that grew up across the ecosystem:
//
//   - tap-dancer's tap-ndjson(7) result model: plan / test / bailout /
//     summary records. The result-family record types here are field-for-
//     field compatible with tap-ndjson(7), so a tap-ndjson stream is a
//     valid ndjson-crap stream.
//   - just-us's --events-fd execution model (RFC 0002): recipe_start /
//     recipe_command / output / recipe_complete records. Those map onto the
//     execution-family record types here (node_start / command / output /
//     node_end); the Reader accepts just-us's discriminators directly as
//     aliases so a just-us event stream is also a valid input.
//
// A single ndjson-crap stream may mix both families: a result-style harness
// emits plan/test/summary, while an execution-style runner emits
// node_start/output/node_end. Consumers MUST ignore record types they do
// not recognize (forward compatibility), which is why Decode returns an
// Unknown record rather than erroring on an unrecognized "type".
package ndjsoncrap

import "encoding/json"

// Schema versions emitted in the Meta header record.
const (
	// CrapVersion is the CRAP major version this schema belongs to.
	CrapVersion = 2
	// NdjsonVersion is the ndjson-crap schema version.
	NdjsonVersion = 1
)

// Record is one decoded ndjson-crap record. The concrete types below all
// implement it. The empty-interface escape hatch is Unknown.
type Record interface {
	// RecordType returns the canonical "type" discriminator value.
	RecordType() string
}

// Meta is the optional stream header. When present it MUST be the first
// record. It carries the schema versions and optional presentation hints.
type Meta struct {
	Type    string `json:"type"` // "crap"
	Version int    `json:"version"`
	Ndjson  int    `json:"ndjson"`
	Title   string `json:"title,omitempty"`
	Source  string `json:"source,omitempty"`
}

func (Meta) RecordType() string { return "crap" }

// Plan is the result-family plan record. count is the number of top-level
// test points. If present it MUST precede the first test record.
type Plan struct {
	Type   string `json:"type"` // "plan"
	Count  int    `json:"count"`
	Reason string `json:"reason,omitempty"`
}

func (Plan) RecordType() string { return "plan" }

// Directive is a skip/todo annotation on a test record.
type Directive struct {
	Kind   string `json:"kind"` // "skip" | "todo"
	Reason string `json:"reason"`
}

// Test is the result-family test record. It is field-compatible with
// tap-ndjson(7): every field is always present, with null for the
// nullable ones. Subtests recurse.
type Test struct {
	Type        string         `json:"type"` // "test"
	N           int            `json:"n"`
	Description string         `json:"description"`
	OK          bool           `json:"ok"`
	Directive   *Directive     `json:"directive"`
	Diagnostic  map[string]any `json:"diagnostic"`
	Output      *string        `json:"output"`
	Subtest     []Test         `json:"subtest"`
	Line        int            `json:"line"`
}

func (Test) RecordType() string { return "test" }

// Bailout is the result-family bailout record. At most one per stream.
type Bailout struct {
	Type    string `json:"type"` // "bailout"
	Message string `json:"message"`
	Line    int    `json:"line"`
}

func (Bailout) RecordType() string { return "bailout" }

// SummaryDiagnostic is one parse diagnostic carried in a Summary.
type SummaryDiagnostic struct {
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Message  string `json:"message"`
}

// Summary is the result-family summary record. Exactly one per result
// stream; it is the final result-family record.
type Summary struct {
	Type        string              `json:"type"` // "summary"
	Passed      int                 `json:"passed"`
	Failed      int                 `json:"failed"`
	Skipped     int                 `json:"skipped"`
	Todo        int                 `json:"todo"`
	Total       int                 `json:"total"`
	PlanCount   int                 `json:"plan_count"`
	Bailed      bool                `json:"bailed"`
	Valid       bool                `json:"valid"`
	Diagnostics []SummaryDiagnostic `json:"diagnostics"`
}

func (Summary) RecordType() string { return "summary" }

// NodeStart is the execution-family node-start record (just-us
// recipe_start). A node is any unit of execution: a recipe, a phase, a
// build step. tp is a stream-unique node id; parent links the tree.
type NodeStart struct {
	Type     string  `json:"type"` // "node_start"
	TP       int     `json:"tp"`
	Name     string  `json:"name"`
	Namepath string  `json:"namepath"`
	Depth    int     `json:"depth"`
	Parent   *int    `json:"parent"`
	Doc      *string `json:"doc"`
	Quiet    bool    `json:"quiet"`
}

func (NodeStart) RecordType() string { return "node_start" }

// Command is an execution-family record naming a command about to run
// under node tp (just-us recipe_command).
type Command struct {
	Type    string `json:"type"` // "command"
	TP      int    `json:"tp"`
	Command string `json:"command"`
	Line    int    `json:"line"`
}

func (Command) RecordType() string { return "command" }

// Output stream identifiers.
const (
	StreamStdout = "stdout"
	StreamStderr = "stderr"
)

// Output data encodings.
const (
	FormatUTF8   = "utf8"
	FormatBase64 = "base64"
)

// Output is an execution-family record carrying a chunk of a node's child
// output. data is utf8 text or base64 per format.
type Output struct {
	Type   string `json:"type"` // "output"
	TP     int    `json:"tp"`
	Stream string `json:"stream"`
	Format string `json:"format"`
	Data   string `json:"data"`
}

func (Output) RecordType() string { return "output" }

// NodeEnd is the execution-family node-end record (just-us
// recipe_complete). Exactly one of exit_code / signal is non-null for a
// process-backed node. Diagnostic optionally carries producer verdict detail
// (failure summary, command, elapsed, …) shaped like Test.Diagnostic; it is
// omitted when nil, so just-us and pre-crap#22 streams stay byte-identical,
// and an absent field decodes to nil (forward compat).
type NodeEnd struct {
	Type       string         `json:"type"` // "node_end"
	TP         int            `json:"tp"`
	ExitCode   *int           `json:"exit_code"`
	Signal     *string        `json:"signal"`
	DurationMs uint64         `json:"duration_ms"`
	Diagnostic map[string]any `json:"diagnostic,omitempty"`
}

func (NodeEnd) RecordType() string { return "node_end" }

// OperationStart begins an operation-family operation: a unit of work over
// many items that renders as a capped rolling tail + progress bar collapsing
// to one verdict (crap RFC 0001). tp is a stream-unique operation id; parent
// links it into the shared tp tree. total/bytes_total of 0 mean unknown
// (indeterminate bar).
type OperationStart struct {
	Type       string `json:"type"` // "operation_start"
	TP         int    `json:"tp"`
	Name       string `json:"name"`
	Parent     *int   `json:"parent"`
	Depth      int    `json:"depth"`
	Total      int    `json:"total"`
	BytesTotal int64  `json:"bytes_total"`
}

func (OperationStart) RecordType() string { return "operation_start" }

// Progress advances an operation's bars without naming an item and without
// persisting a line (crap RFC 0001). op references the OperationStart tp. A
// total/bytes_total of 0 leaves the prior denominator unchanged.
type Progress struct {
	Type       string `json:"type"` // "progress"
	Op         int    `json:"op"`
	Current    int    `json:"current"`
	Total      int    `json:"total"`
	Bytes      int64  `json:"bytes"`
	BytesTotal int64  `json:"bytes_total"`
	Label      string `json:"label,omitempty"`
}

func (Progress) RecordType() string { return "progress" }

// Item reports the outcome of one work item within an operation (crap RFC
// 0001). state is "done", "skipped", or "failed". diagnostic is null except
// for failures; directive may carry a skip reason for "skipped".
type Item struct {
	Type       string         `json:"type"` // "item"
	Op         int            `json:"op"`
	Label      string         `json:"label"`
	State      string         `json:"state"`
	Bytes      int64          `json:"bytes"`
	Diagnostic map[string]any `json:"diagnostic"`
	Directive  *Directive     `json:"directive,omitempty"`
}

func (Item) RecordType() string { return "item" }

// Item state values.
const (
	ItemDone    = "done"
	ItemSkipped = "skipped"
	ItemFailed  = "failed"
)

// OperationEnd terminates an operation with a tallied verdict (crap RFC
// 0001). Exactly one per OperationStart. ok is failed==0 and the operation
// did not abort.
type OperationEnd struct {
	Type       string `json:"type"` // "operation_end"
	Op         int    `json:"op"`
	Done       int    `json:"done"`
	Skipped    int    `json:"skipped"`
	Failed     int    `json:"failed"`
	Total      int    `json:"total"`
	OK         bool   `json:"ok"`
	DurationMs uint64 `json:"duration_ms"`
}

func (OperationEnd) RecordType() string { return "operation_end" }

// Unknown holds a record whose "type" this build does not recognize. It is
// retained verbatim so a consumer can round-trip or skip it. Producers MUST
// NOT emit Unknown; it exists only for forward-compatible decoding.
type Unknown struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

func (u Unknown) RecordType() string { return u.Type }
