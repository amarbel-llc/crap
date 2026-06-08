package crap

import (
	"bufio"
	"context"
	"io"
	"os/exec"

	"github.com/amarbel-llc/crap/go-crap/v2/ndjsoncrap"
)

// ConvertExec runs a single command and writes an execution-family
// ndjson-crap stream to w: a node_start, an output record per captured line
// (stdout and stderr interleaved in arrival order), and a node_end carrying
// the exit code or terminating signal. Returns the command's exit code (or
// 1 if it could not be started).
func ConvertExec(ctx context.Context, name string, args []string, w io.Writer) int {
	nw := ndjsoncrap.NewWriter(w)
	label := name
	for _, a := range args {
		label += " " + a
	}

	_ = nw.Write(ndjsoncrap.NodeStart{TP: 1, Name: label, Namepath: name})

	cmd := exec.CommandContext(ctx, name, args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		msg := err.Error()
		_ = nw.Write(ndjsoncrap.Output{TP: 1, Stream: ndjsoncrap.StreamStderr, Data: msg + "\n"})
		code := 1
		_ = nw.Write(ndjsoncrap.NodeEnd{TP: 1, ExitCode: &code})
		return code
	}

	done := make(chan struct{}, 2)
	pump := func(r io.Reader, stream string) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
		for sc.Scan() {
			_ = nw.Write(ndjsoncrap.Output{TP: 1, Stream: stream, Data: sc.Text() + "\n"})
		}
		done <- struct{}{}
	}
	go pump(stdout, ndjsoncrap.StreamStdout)
	go pump(stderr, ndjsoncrap.StreamStderr)
	<-done
	<-done

	err := cmd.Wait()
	end := ndjsoncrap.NodeEnd{TP: 1}
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	end.ExitCode = &code
	_ = nw.Write(end)
	return code
}
