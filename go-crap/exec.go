package crap

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"strings"

	"github.com/amarbel-llc/crap/go-crap/v2/ndjsoncrap"
)

// ExecOptions adjusts how ConvertExecOpts shapes its execution-family
// records, so shell orchestrators can concatenate several invocations into
// one multi-node stream.
type ExecOptions struct {
	// TP is the stream-unique node id stamped on the node_start / output /
	// node_end records. Zero means 1.
	TP int
	// Name is the rendered node label (node_start.name). Empty means the
	// joined command line.
	Name string
}

// ConvertExec runs a single command and writes an execution-family
// ndjson-crap stream to w: a node_start, an output record per captured line
// (stdout and stderr interleaved in arrival order), and a node_end carrying
// the exit code or terminating signal. Returns the command's exit code (or
// 1 if it could not be started).
func ConvertExec(ctx context.Context, name string, args []string, w io.Writer) int {
	return ConvertExecOpts(ctx, name, args, w, ExecOptions{})
}

// ConvertExecOpts is ConvertExec with an explicit node id and label. On
// failure (start error, nonzero exit, or signal) the node_end carries a
// diagnostic ({"error": ..., "command": ...}) so the node is a
// self-sufficient verdict unit (docs/ndjson-crap-schema.md); the field is
// absent on success.
func ConvertExecOpts(ctx context.Context, name string, args []string, w io.Writer, opts ExecOptions) int {
	nw := ndjsoncrap.NewWriter(w)
	tp := opts.TP
	if tp == 0 {
		tp = 1
	}
	command := name
	if len(args) > 0 {
		command += " " + strings.Join(args, " ")
	}
	label := opts.Name
	if label == "" {
		label = command
	}

	_ = nw.Write(ndjsoncrap.NodeStart{TP: tp, Name: label, Namepath: name})

	cmd := exec.CommandContext(ctx, name, args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		msg := err.Error()
		_ = nw.Write(ndjsoncrap.Output{TP: tp, Stream: ndjsoncrap.StreamStderr, Data: msg + "\n"})
		code := 1
		_ = nw.Write(ndjsoncrap.NodeEnd{
			TP:         tp,
			ExitCode:   &code,
			Diagnostic: map[string]any{"error": msg, "command": command},
		})
		return code
	}

	done := make(chan struct{}, 2)
	pump := func(r io.Reader, stream string) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
		for sc.Scan() {
			_ = nw.Write(ndjsoncrap.Output{TP: tp, Stream: stream, Data: sc.Text() + "\n"})
		}
		done <- struct{}{}
	}
	go pump(stdout, ndjsoncrap.StreamStdout)
	go pump(stderr, ndjsoncrap.StreamStderr)
	<-done
	<-done

	err := cmd.Wait()
	end := ndjsoncrap.NodeEnd{TP: tp}
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
		end.Diagnostic = map[string]any{"error": err.Error(), "command": command}
	}
	end.ExitCode = &code
	_ = nw.Write(end)
	return code
}
