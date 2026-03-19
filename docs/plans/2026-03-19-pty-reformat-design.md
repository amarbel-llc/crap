# PTY-Wrapping Reformatter

**Date:** 2026-03-19

## Problem

`large-colon` (`::`) doesn't forward signals to child processes, doesn't fail
when `reformat` is given a non-existent utility, and has no way to run a command
with PTY allocation and reformat its output.

## Design

### CLI behavior

```
::                    → read stdin through reformatter (same as :: reformat)
:: reformat           → read stdin through reformatter (unchanged)
:: some-cmd args...   → run some-cmd in a PTY, pipe output through reformatter
:: validate ...       → existing subcommand (unchanged)
:: exec ...           → existing subcommand (unchanged)
```

The `default:` case in main becomes "run this as a command with PTY + reformat"
instead of "unknown command, exit 1".

### Architecture

New function in `go-crap/ptyrun.go`:

```go
func RunWithPTYReformat(ctx context.Context, command string, args []string,
    w io.Writer, color bool) int
```

1. Resolve `command` via `exec.LookPath`; if not found, exit 127
2. Allocate PTY via `creack/pty`, start child with `pty.Start(cmd)`
3. Forward SIGINT/SIGTERM/SIGHUP to child process
4. Read child's PTY output, pipe through `ReformatTAP` to `w`
5. Wait for child; return child's exit code

### Signal handling

- `signal.Notify` for SIGINT, SIGTERM, SIGHUP
- Forward received signal to child via `cmd.Process.Signal(sig)`
- Let the child decide how to exit
- Exit with the child's exit status (128+signal if killed by signal)

### Error cases

- Command not found: stderr message, exit 127
- Command not executable: exit 126
- PTY allocation fails: bail out with error message

### Rollback

Purely additive. Existing subcommands checked first in switch. Revert = revert
commits.
