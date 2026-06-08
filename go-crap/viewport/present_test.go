package viewport

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// When p.Run errors before the driver drains the input, runProgram must close
// the input so the driver goroutine unblocks instead of leaking on a pending
// Read (issue #20). A pre-cancelled context makes p.Run return ErrProgramKilled
// immediately, exercising the error path deterministically.
func TestRunProgramUnblocksDriverOnError(t *testing.T) {
	pr, pw := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := tea.NewProgram(
		New(),
		tea.WithContext(ctx),
		tea.WithOutput(io.Discard),
		tea.WithInput(strings.NewReader("")),
	)

	done := make(chan error, 1)
	go func() { done <- runProgram(p, pr) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected runProgram to return the killed-program error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runProgram did not return after p.Run errored")
	}

	// runProgram closed pr on the error path, so the driver's pending Read is
	// unblocked and a late write to the pipe fails with ErrClosedPipe. Without
	// the fix the leaked driver is still reading and the write would succeed,
	// so this assertion is the bug detector.
	werr := make(chan error, 1)
	go func() {
		_, err := pw.Write([]byte("late\n"))
		werr <- err
	}()
	select {
	case err := <-werr:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("expected ErrClosedPipe (driver unblocked, input closed), got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write to pipe did not return; driver goroutine leaked (input never closed)")
	}
}
