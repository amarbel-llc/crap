package viewport

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"code.linenisgreat.com/crap/go-crap/v2/ndjsoncrap"
)

// Options configures Present.
type Options struct {
	// Title is the run header shown while live and in the final frame.
	Title string
	// TailLines overrides the rolling-tail height (0 = default).
	TailLines int
	// FailureBacklog overrides the per-phase failure-backlog cap — the lines
	// persisted above a ✗ verdict (0 = default). crap#35.
	FailureBacklog int
	// Out is where the live TUI renders. Defaults to nil, which Present's
	// caller should set to os.Stderr; tests pass a buffer.
	Out io.Writer
	// IsTTY reports whether Out is an interactive terminal. When false,
	// Present renders the plain, non-interactive fallback.
	IsTTY bool
}

// Present reads an ndjson-crap stream from in and renders it. On a TTY it
// runs the bubbletea viewport (spinner + rolling tail + progress, persisted
// verdict lines); otherwise it renders a plain line-per-verdict fallback.
//
// The data stream is in; the TUI never reads it as keyboard input (the
// program is given an empty input reader), so in may be a pipe carrying the
// records while Out is the controlling terminal.
func Present(in io.Reader, opts Options) error {
	if !opts.IsTTY {
		return presentPlain(in, opts)
	}

	var mopts []Option
	if opts.Title != "" {
		mopts = append(mopts, WithTitle(opts.Title))
	}
	if opts.TailLines > 0 {
		mopts = append(mopts, WithTailLines(opts.TailLines))
	}
	if opts.FailureBacklog > 0 {
		mopts = append(mopts, WithFailureBacklog(opts.FailureBacklog))
	}

	p := tea.NewProgram(
		New(mopts...),
		tea.WithOutput(opts.Out),
		// The records arrive on `in`; keystrokes must not be read from it.
		// An empty reader disables input without consuming the data pipe.
		tea.WithInput(strings.NewReader("")),
	)

	return runProgram(p, in)
}

// runProgram runs the bubbletea program p while a driver goroutine feeds it
// ndjson-crap records read from in. The happy path: the driver reaches EOF,
// the program quits, and p.Run returns nil, so the driver's error is the
// result.
//
// If p.Run returns an error first (e.g. terminal setup failure), the driver
// goroutine would otherwise stay blocked forever on in.Read because nothing
// closes the read end. To avoid that goroutine leak (and the caller-side
// hang it causes when in is a pipe whose writer waits on a full buffer), the
// error path closes in to unblock the pending Read, then reaps the goroutine
// before returning.
func runProgram(p *tea.Program, in io.Reader) error {
	driveErr := make(chan error, 1)
	go func() {
		driveErr <- NewDriver(p).Run(ndjsoncrap.NewReader(in))
	}()

	if _, err := p.Run(); err != nil {
		if c, ok := in.(io.Closer); ok {
			// Closing unblocks the driver's pending Read (io.PipeReader
			// makes it return ErrClosedPipe), so reaping the goroutine
			// can't hang. A non-Closer reader can't be unblocked, so
			// returning immediately (leaking at worst) beats blocking
			// the caller forever on <-driveErr.
			_ = c.Close()
			<-driveErr
		}
		return err
	}
	return <-driveErr
}

// presentPlain renders the stream without a TUI: one verdict line per
// finished test/node, with failures echoing their captured output. Used
// when Out is not a terminal (e.g. piped into a file or another tool).
func presentPlain(in io.Reader, opts Options) error {
	out := opts.Out
	r := ndjsoncrap.NewReader(in)
	nodeName := map[int]string{}
	opName := map[int]string{}
	for {
		rec, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch v := rec.(type) {
		case ndjsoncrap.Test:
			plainVerdict(out, v.Description, verdictFromTest(v), v.Output)
		case ndjsoncrap.Bailout:
			fmt.Fprintf(out, "✗ Bail out! %s\n", v.Message)
		case ndjsoncrap.NodeStart:
			name := v.Name
			if name == "" {
				name = v.Namepath
			}
			nodeName[v.TP] = name
		case ndjsoncrap.NodeEnd:
			name := nodeName[v.TP]
			delete(nodeName, v.TP)
			plainVerdict(out, name, verdictFromNodeEnd(v), nil)
		case ndjsoncrap.OperationStart:
			opName[v.TP] = v.Name
		case ndjsoncrap.Item:
			// Off a TTY there is no rolling tail: done/skipped items are
			// transient and persist nothing; only failures persist a verdict
			// (crap RFC 0001 §4).
			if v.State == ndjsoncrap.ItemFailed {
				plainVerdict(out, v.Label, VerdictView{OK: false, Diagnostic: v.Diagnostic}, nil)
			}
		case ndjsoncrap.OperationEnd:
			name := opName[v.Op]
			delete(opName, v.Op)
			desc := fmt.Sprintf("%s — %d done, %d skipped, %d failed",
				name, v.Done, v.Skipped, v.Failed)
			plainVerdict(out, desc, VerdictView{OK: v.OK}, nil)
		}
	}
}

func plainVerdict(out io.Writer, desc string, v VerdictView, output *string) {
	switch {
	case v.Directive != nil:
		fmt.Fprintf(out, "↷ %s # %s %s\n", desc, strings.ToUpper(v.Directive.Kind), v.Directive.Reason)
	case v.OK:
		fmt.Fprintf(out, "✓ %s\n", desc)
	default:
		if output != nil {
			for _, line := range unnestOutput(*output) {
				fmt.Fprintf(out, "│ %s\n", line)
			}
		}
		fmt.Fprintf(out, "✗ %s\n", desc)
		for _, k := range sortedKeys(v.Diagnostic) {
			fmt.Fprintf(out, "  %s: %v\n", k, v.Diagnostic[k])
		}
	}
}
