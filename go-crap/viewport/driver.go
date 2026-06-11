package viewport

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amarbel-llc/crap/go-crap/v2/ndjsoncrap"
)

// sender is the subset of *tea.Program the driver uses, narrowed so tests
// can inject a fake without a running program. *tea.Program satisfies it.
type sender interface {
	Send(tea.Msg)
}

// Driver translates an ndjson-crap record stream into viewport messages.
// It handles both record families:
//
//   - result family (plan/test/bailout/summary): each top-level test
//     persists one verdict line; the plan count arms the progress bar; the
//     summary ends the run.
//   - execution family (node_start/command/output/node_end): each node is a
//     phase; output and command records feed the rolling tail; node_end
//     persists the phase verdict.
//
// A stream may mix both families. Unknown records are ignored.
type Driver struct {
	s sender

	// nodeName maps an execution-family node tp to its description so a
	// node_end can name the phase it closes.
	nodeName map[int]string
	// testsSeen counts top-level result-family tests for the progress bar.
	testsSeen int
	// sawSummary records whether a result-family summary ended the run.
	sawSummary bool

	// Operation family (crap RFC 0001), keyed by the operation's tp: opName
	// names the operation_end verdict; opCurrent and opBytes track the running
	// item/byte totals so each item can send the absolute OperationProgress
	// the Model expects (its OperationProgress handler overwrites current/bytes
	// rather than incrementing).
	opName    map[int]string
	opCurrent map[int]int
	opBytes   map[int]int64
}

// NewDriver builds a Driver that sends messages to s.
func NewDriver(s sender) *Driver {
	return &Driver{
		s:         s,
		nodeName:  map[int]string{},
		opName:    map[int]string{},
		opCurrent: map[int]int{},
		opBytes:   map[int]int64{},
	}
}

// Run reads every record from r, driving the viewport, until io.EOF or a
// decode error. It always finalizes the run (BatchDone) so the program
// quits even on a truncated or summary-less stream.
func (d *Driver) Run(r *ndjsoncrap.Reader) error {
	var streamErr error
	for {
		rec, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			streamErr = err
			break
		}
		d.feed(rec)
	}
	if !d.sawSummary {
		d.s.Send(BatchDone{Err: streamErr})
	}
	return streamErr
}

func (d *Driver) feed(rec ndjsoncrap.Record) {
	switch r := rec.(type) {
	case ndjsoncrap.Meta:
		if r.Title != "" {
			d.s.Send(OperationStarted{Name: r.Title})
		}

	case ndjsoncrap.Plan:
		if r.Count > 0 {
			d.s.Send(OperationStarted{Total: r.Count})
		}

	case ndjsoncrap.Test:
		d.testsSeen++
		if r.Output != nil && *r.Output != "" {
			for _, line := range splitLines(*r.Output) {
				d.s.Send(LogLine{Text: line})
			}
		}
		d.s.Send(PhaseEnded{Description: r.Description, Verdict: verdictFromTest(r)})
		d.s.Send(OperationProgress{Current: d.testsSeen})

	case ndjsoncrap.Bailout:
		d.s.Send(PhaseEnded{
			Description: "Bail out! " + r.Message,
			Verdict:     VerdictView{OK: false},
		})

	case ndjsoncrap.Summary:
		d.sawSummary = true
		var err error
		if r.Failed > 0 || r.Bailed || !r.Valid {
			err = fmt.Errorf("%d failed", r.Failed)
			if r.Bailed {
				err = fmt.Errorf("bailed out")
			}
		}
		d.s.Send(BatchDone{Err: err})

	case ndjsoncrap.NodeStart:
		name := r.Name
		if name == "" {
			name = r.Namepath
		}
		d.nodeName[r.TP] = name
		d.s.Send(PhaseStarted{Description: name})

	case ndjsoncrap.Command:
		d.s.Send(LogLine{Text: "$ " + r.Command})

	case ndjsoncrap.Output:
		for _, line := range splitLines(decodeOutput(r)) {
			d.s.Send(LogLine{Text: line})
		}

	case ndjsoncrap.NodeEnd:
		desc := d.nodeName[r.TP]
		delete(d.nodeName, r.TP)
		d.s.Send(PhaseEnded{Description: desc, Verdict: verdictFromNodeEnd(r)})

	case ndjsoncrap.OperationStart:
		d.opName[r.TP] = r.Name
		d.opCurrent[r.TP] = 0
		d.opBytes[r.TP] = 0
		// Always arm the item bar via OperationStarted (never PhaseStarted).
		d.s.Send(OperationStarted{Name: r.Name, Total: r.Total})
		// OperationStarted carries no byte total, so arm the byte bar with a
		// progress message when bytes_total is known.
		if r.BytesTotal > 0 {
			d.s.Send(OperationProgress{BytesTotal: r.BytesTotal})
		}

	case ndjsoncrap.Progress:
		d.opCurrent[r.Op] = r.Current
		d.opBytes[r.Op] = r.Bytes
		d.s.Send(OperationProgress{
			Current:    r.Current,
			Total:      r.Total,
			Bytes:      r.Bytes,
			BytesTotal: r.BytesTotal,
		})
		if r.Label != "" {
			d.s.Send(LogLine{Text: r.Label})
		}

	case ndjsoncrap.Item:
		d.feedItem(r)

	case ndjsoncrap.OperationEnd:
		name := d.opName[r.Op]
		delete(d.opName, r.Op)
		delete(d.opCurrent, r.Op)
		delete(d.opBytes, r.Op)
		desc := fmt.Sprintf("%s — %d done, %d skipped, %d failed",
			name, r.Done, r.Skipped, r.Failed)
		d.s.Send(PhaseEnded{Description: desc, Verdict: VerdictView{OK: r.OK}})

	case ndjsoncrap.Unknown:
		// Forward compatibility: ignore record types we do not present.
	}
}

// feedItem maps one operation item (crap RFC 0001 §4, §7). A done item feeds
// the rolling tail transiently; a skipped item feeds it dimmed with a ↷
// prefix; a failed item persists a verdict via ItemFailed (which does NOT
// reset the operation's live region). All three then advance the operation's
// progress — the OperationProgress is sent AFTER the item message, per §7's
// left-to-right ordering. Bytes are accumulated so the absolute byte counter
// the Model expects never regresses on a zero-byte item.
func (d *Driver) feedItem(r ndjsoncrap.Item) {
	switch r.State {
	case ndjsoncrap.ItemFailed:
		d.s.Send(ItemFailed{Label: r.Label, Diagnostic: r.Diagnostic})
	case ndjsoncrap.ItemSkipped:
		d.s.Send(LogLine{Text: "↷ " + r.Label})
	default: // ItemDone
		d.s.Send(LogLine{Text: r.Label})
	}
	d.opCurrent[r.Op]++
	d.opBytes[r.Op] += r.Bytes
	d.s.Send(OperationProgress{Current: d.opCurrent[r.Op], Bytes: d.opBytes[r.Op]})
}

// verdictFromTest resolves a result-family test record to a view verdict.
func verdictFromTest(t ndjsoncrap.Test) VerdictView {
	if t.Directive != nil {
		return VerdictView{
			OK:        t.OK,
			Directive: &DirectiveView{Kind: t.Directive.Kind, Reason: t.Directive.Reason},
		}
	}
	return VerdictView{OK: t.OK, Diagnostic: t.Diagnostic}
}

// verdictFromNodeEnd resolves an execution-family node_end to a view
// verdict: success is exit 0 with no signal; otherwise the exit code or
// signal is synthesized into the diagnostic and merged with the producer's
// own diagnostic, producer keys winning (crap#22).
func verdictFromNodeEnd(n ndjsoncrap.NodeEnd) VerdictView {
	ok := n.Signal == nil && n.ExitCode != nil && *n.ExitCode == 0
	if ok {
		return VerdictView{OK: true}
	}
	diag := map[string]any{}
	switch {
	case n.Signal != nil:
		diag["signal"] = *n.Signal
	case n.ExitCode != nil:
		diag["exit_code"] = *n.ExitCode
	}
	for k, v := range n.Diagnostic {
		diag[k] = v
	}
	return VerdictView{OK: false, Diagnostic: diag}
}

// decodeOutput returns an Output record's data as text, base64-decoding when
// needed. Undecodable base64 falls back to the raw string.
func decodeOutput(o ndjsoncrap.Output) string {
	if o.Format == ndjsoncrap.FormatBase64 {
		if b, err := base64.StdEncoding.DecodeString(o.Data); err == nil {
			return string(b)
		}
	}
	return o.Data
}

// splitLines splits s into non-empty trimmed-of-trailing-newline lines.
func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
