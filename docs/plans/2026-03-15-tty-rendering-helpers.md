# TTY Rendering Helpers Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Lift TTY rendering helpers from just-us into rust-crap (and then go-crap) so consumers don't independently rediscover the same ANSI/PTY fixes.

**Architecture:** Five changes to `rust-crap/src/lib.rs` — DECAWM wrapping, `has_visible_content`, `StatusLineProcessor`, auto-clear before test points, blank-line filtering — then port the same to `go-crap/crap.go`. All changes are additive or fix existing behavior.

**Tech Stack:** Rust (rust-crap), Go (go-crap). No new dependencies.

**Rollback:** N/A — purely additive API additions and bug fixes.

---

## Rust tasks

### Task 1: `has_visible_content()` public utility

**Files:**
- Modify: `rust-crap/src/lib.rs`

**Step 1: Write the failing test**

Add to the `#[cfg(test)] mod tests` block:

```rust
#[test]
fn has_visible_content_plain_text() {
    assert!(has_visible_content("hello"));
}

#[test]
fn has_visible_content_ansi_only() {
    assert!(!has_visible_content("\x1b[0m\x1b[K"));
}

#[test]
fn has_visible_content_ansi_with_text() {
    assert!(has_visible_content("\x1b[32mok\x1b[0m"));
}

#[test]
fn has_visible_content_whitespace_only() {
    assert!(!has_visible_content("   \t  "));
}

#[test]
fn has_visible_content_empty() {
    assert!(!has_visible_content(""));
}

#[test]
fn has_visible_content_control_chars_only() {
    assert!(!has_visible_content("\x01\x02\x03"));
}
```

**Step 2: Run test to verify it fails**

Run: `cd rust-crap && cargo test has_visible_content`
Expected: FAIL — `has_visible_content` not found

**Step 3: Write minimal implementation**

Add above `strip_ansi` (around line 448):

```rust
/// Check whether a string has any visible content after stripping ANSI escape
/// sequences. Returns false for strings that are only whitespace and/or
/// control sequences (e.g. `\x1b[0m\x1b[K`).
pub fn has_visible_content(s: &str) -> bool {
    let mut chars = s.chars();
    while let Some(c) = chars.next() {
        if c == '\x1b' {
            // Skip CSI sequence: ESC [ <params>* <intermediate>* <final 0x40-0x7E>
            if chars.next() == Some('[') {
                for c in chars.by_ref() {
                    if ('@'..='~').contains(&c) {
                        break;
                    }
                }
            }
        } else if !c.is_whitespace() && !c.is_ascii_control() {
            return true;
        }
    }
    false
}
```

**Step 4: Run test to verify it passes**

Run: `cd rust-crap && cargo test has_visible_content`
Expected: all 6 tests PASS

**Step 5: Commit**

```
feat(rust-crap): add has_visible_content() public utility

Port from just-us recipe.rs. Walks chars, skips CSI sequences and
whitespace/control, returns true on first visible character.
```

---

### Task 2: DECAWM wrapping in `update_last_line()` + `status_line_active` tracking

**Files:**
- Modify: `rust-crap/src/lib.rs`

**Step 1: Write the failing tests**

```rust
#[test]
fn update_last_line_decawm_with_color() {
    let mut buf = Vec::new();
    let mut tw = CrapWriterBuilder::new(&mut buf)
        .color(true)
        .status_line(true)
        .build()
        .unwrap();
    tw.update_last_line("long line here").unwrap();
    let out = String::from_utf8(buf).unwrap();
    assert!(
        out.contains("\r\x1b[2K\x1b[?7l# long line here\x1b[?7h"),
        "expected DECAWM wrapping, got:\n{out}"
    );
}

#[test]
fn update_last_line_no_decawm_without_color() {
    let mut buf = Vec::new();
    let mut tw = CrapWriterBuilder::new(&mut buf)
        .color(false)
        .status_line(true)
        .build()
        .unwrap();
    tw.update_last_line("line").unwrap();
    let out = String::from_utf8(buf).unwrap();
    assert!(
        out.contains("\r\x1b[2K# line"),
        "expected no DECAWM wrapping without color, got:\n{out}"
    );
    assert!(
        !out.contains("\x1b[?7l"),
        "should not contain DECAWM disable without color"
    );
}
```

**Step 2: Run test to verify it fails**

Run: `cd rust-crap && cargo test update_last_line_decawm`
Expected: FAIL — `update_last_line_decawm_with_color` fails because current output lacks `\x1b[?7l`

**Step 3: Implement**

Add `status_line_active: bool` field to `CrapWriter` struct (initialized `false` in both `build()` and `build_without_printing()`). Also add it to the `subtest` child initialization with `false`.

Modify `update_last_line`:

```rust
pub fn update_last_line(&mut self, text: &str) -> io::Result<()> {
    if self.config.color {
        write!(self.w, "\r\x1b[2K\x1b[?7l# {}\x1b[?7h", text)?;
    } else {
        write!(self.w, "\r\x1b[2K# {}", text)?;
    }
    self.status_line_active = true;
    self.w.flush()
}
```

Modify `finish_last_line`:

```rust
pub fn finish_last_line(&mut self) -> io::Result<()> {
    self.status_line_active = false;
    write!(self.w, "\r\x1b[2K")?;
    self.w.flush()
}
```

**Step 4: Run tests to verify they pass**

Run: `cd rust-crap && cargo test`
Expected: all tests PASS (including existing `writer_update_last_line` — update its assertion to match new DECAWM-free output since that test uses `color(false)` implicitly via `CrapWriterBuilder::new`)

Check: the existing `writer_update_last_line` test uses `CrapWriterBuilder::new` which defaults `color: false`, so its assertion `\r\x1b[2K# building... 1/3` still holds. Verify this.

**Step 5: Commit**

```
feat(rust-crap): add DECAWM wrapping to update_last_line

Bracket status lines with \x1b[?7l / \x1b[?7h when color is enabled
to prevent long lines from wrapping and leaving ghost artifacts.
Track status_line_active for auto-clear in a follow-up task.
```

---

### Task 3: Auto-clear before test points

**Files:**
- Modify: `rust-crap/src/lib.rs`

**Step 1: Write the failing test**

```rust
#[test]
fn test_point_auto_clears_status_line() {
    let mut buf = Vec::new();
    let mut tw = CrapWriterBuilder::new(&mut buf)
        .status_line(true)
        .build()
        .unwrap();
    tw.update_last_line("building...").unwrap();
    let result = TestResult {
        number: 1,
        name: "build".into(),
        ok: true,
        directive: None,
        error_message: None,
        exit_code: None,
        output: None,
        suppress_yaml: false,
    };
    tw.test_point(&result).unwrap();
    let out = String::from_utf8(buf).unwrap();
    // After update_last_line, test_point should emit \r\x1b[2K before the ok line
    let ok_pos = out.rfind("ok 1 - build").unwrap();
    let clear_before = &out[..ok_pos];
    assert!(
        clear_before.ends_with("\r\x1b[2K"),
        "test_point should auto-clear status line, got:\n{out}"
    );
}

#[test]
fn test_point_no_clear_without_active_status() {
    let mut buf = Vec::new();
    let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
    let result = TestResult {
        number: 1,
        name: "build".into(),
        ok: true,
        directive: None,
        error_message: None,
        exit_code: None,
        output: None,
        suppress_yaml: false,
    };
    tw.test_point(&result).unwrap();
    let out = String::from_utf8(buf).unwrap();
    // Count occurrences of \r\x1b[2K — should be zero
    assert!(
        !out.contains("\r\x1b[2K"),
        "should not emit clear when no status line active, got:\n{out}"
    );
}
```

**Step 2: Run test to verify it fails**

Run: `cd rust-crap && cargo test test_point_auto_clear`
Expected: FAIL — `test_point_auto_clears_status_line` fails because `test_point` doesn't emit clear

**Step 3: Implement**

At the top of `test_point()`, before `self.counter += 1`:

```rust
if self.status_line_active {
    self.finish_last_line()?;
}
```

Also add the same guard at the top of `ok()`, `not_ok()`, `not_ok_diag()`, `skip()`, `todo()`, and `bail_out()` — these all emit test-point-level output that should clear a transient status line.

**Step 4: Run tests to verify they pass**

Run: `cd rust-crap && cargo test`
Expected: all tests PASS

**Step 5: Commit**

```
feat(rust-crap): auto-clear status line before test points

When status_line_active is true, test_point/ok/not_ok/skip/todo/bail_out
call finish_last_line() before emitting output. Consumers no longer
need to manually clear the status line before each result.
```

---

### Task 4: `StatusLineProcessor` + `CrapWriter::feed_status_bytes()`

**Files:**
- Modify: `rust-crap/src/lib.rs`

**Step 1: Write the failing tests**

```rust
#[test]
fn status_line_processor_splits_on_newline() {
    let mut p = StatusLineProcessor::new();
    let lines: Vec<_> = p.feed(b"hello\nworld\n").collect();
    assert_eq!(lines, vec!["hello", "world"]);
}

#[test]
fn status_line_processor_splits_on_cr() {
    let mut p = StatusLineProcessor::new();
    let lines: Vec<_> = p.feed(b"evaluating...\rdownloading...\n").collect();
    assert_eq!(lines, vec!["evaluating...", "downloading..."]);
}

#[test]
fn status_line_processor_filters_ansi_only() {
    let mut p = StatusLineProcessor::new();
    let lines: Vec<_> = p.feed(b"\x1b[0m\x1b[K\nreal content\n").collect();
    assert_eq!(lines, vec!["real content"]);
}

#[test]
fn status_line_processor_filters_empty() {
    let mut p = StatusLineProcessor::new();
    let lines: Vec<_> = p.feed(b"\n\n\nhello\n").collect();
    assert_eq!(lines, vec!["hello"]);
}

#[test]
fn status_line_processor_buffers_partial() {
    let mut p = StatusLineProcessor::new();
    let lines1: Vec<_> = p.feed(b"partial").collect();
    assert!(lines1.is_empty());
    let lines2: Vec<_> = p.feed(b" line\n").collect();
    assert_eq!(lines2, vec!["partial line"]);
}

#[test]
fn status_line_processor_trims_whitespace() {
    let mut p = StatusLineProcessor::new();
    let lines: Vec<_> = p.feed(b"  spaced  \n").collect();
    assert_eq!(lines, vec!["spaced"]);
}

#[test]
fn status_line_processor_handles_crlf() {
    let mut p = StatusLineProcessor::new();
    let lines: Vec<_> = p.feed(b"line one\r\nline two\r\n").collect();
    assert_eq!(lines, vec!["line one", "line two"]);
}

#[test]
fn feed_status_bytes_updates_status_line() {
    let mut buf = Vec::new();
    let mut tw = CrapWriterBuilder::new(&mut buf)
        .status_line(true)
        .build()
        .unwrap();
    tw.feed_status_bytes(b"building...\nlinking...\n").unwrap();
    let out = String::from_utf8(buf).unwrap();
    assert!(out.contains("# building..."), "got:\n{out}");
    assert!(out.contains("# linking..."), "got:\n{out}");
}
```

**Step 2: Run test to verify it fails**

Run: `cd rust-crap && cargo test status_line_processor`
Expected: FAIL — `StatusLineProcessor` not found

**Step 3: Implement**

Add `StatusLineProcessor` struct above `CrapWriter`:

```rust
/// Processes raw byte chunks from PTY output into clean status lines.
///
/// Splits on `\r` and `\n`, trims whitespace, and filters out lines
/// that contain only ANSI escape sequences or whitespace. Buffers
/// partial lines across `feed()` calls.
pub struct StatusLineProcessor {
    buf: Vec<u8>,
}

impl Default for StatusLineProcessor {
    fn default() -> Self {
        Self::new()
    }
}

impl StatusLineProcessor {
    pub fn new() -> Self {
        Self { buf: Vec::new() }
    }

    pub fn feed(&mut self, chunk: &[u8]) -> impl Iterator<Item = String> + '_ {
        self.buf.extend_from_slice(chunk);
        StatusLineIter { proc: self }
    }
}

struct StatusLineIter<'a> {
    proc: &'a mut StatusLineProcessor,
}

impl Iterator for StatusLineIter<'_> {
    type Item = String;

    fn next(&mut self) -> Option<String> {
        loop {
            let pos = self
                .proc
                .buf
                .iter()
                .position(|&b| b == b'\n' || b == b'\r')?;
            let line_bytes = self.proc.buf[..pos].to_vec();
            self.proc.buf.drain(..=pos);
            let line = String::from_utf8_lossy(&line_bytes);
            let trimmed = line.trim();
            if has_visible_content(trimmed) {
                return Some(trimmed.to_string());
            }
        }
    }
}
```

Add `status_processor: Option<StatusLineProcessor>` field to `CrapWriter` (initialized `None`).

Add method to `CrapWriter`:

```rust
pub fn feed_status_bytes(&mut self, chunk: &[u8]) -> io::Result<()> {
    let proc = self.status_processor.get_or_insert_with(StatusLineProcessor::new);
    // Collect into a Vec to avoid borrow conflict with self
    let lines: Vec<_> = proc.feed(chunk).collect();
    for line in lines {
        self.update_last_line(&line)?;
    }
    Ok(())
}
```

**Step 4: Run tests to verify they pass**

Run: `cd rust-crap && cargo test`
Expected: all tests PASS

**Step 5: Commit**

```
feat(rust-crap): add StatusLineProcessor and feed_status_bytes

StatusLineProcessor splits raw PTY byte chunks on \r/\n, trims, and
filters ANSI-only lines. CrapWriter::feed_status_bytes is a convenience
that feeds chunks and calls update_last_line for each clean line.
```

---

### Task 5: Blank-line filtering in `sanitize_yaml_value()`

**Files:**
- Modify: `rust-crap/src/lib.rs`

**Step 1: Write the failing test**

```rust
#[test]
fn sanitize_yaml_value_filters_blank_lines() {
    let input = "line one\n\n\nline two\n  \nline three";
    let result = sanitize_yaml_value(input, false);
    assert_eq!(result, "line one\nline two\nline three");
}

#[test]
fn sanitize_yaml_value_filters_blank_lines_from_crlf() {
    let input = "line one\r\n\r\nline two\r\n";
    let result = sanitize_yaml_value(input, false);
    assert_eq!(result, "line one\nline two");
}
```

**Step 2: Run test to verify it fails**

Run: `cd rust-crap && cargo test sanitize_yaml_value_filters`
Expected: FAIL — blank lines are preserved

**Step 3: Implement**

Modify `sanitize_yaml_value`:

```rust
fn sanitize_yaml_value(value: &str, color: bool) -> String {
    let value = normalize_line_endings(value);
    let stripped = if color {
        strip_non_sgr_csi(&value)
    } else {
        strip_ansi(&value)
    };
    stripped
        .split('\n')
        .filter(|line| !line.trim().is_empty())
        .collect::<Vec<_>>()
        .join("\n")
}
```

**Step 4: Run tests to verify they pass**

Run: `cd rust-crap && cargo test`
Expected: all tests PASS. Check that existing YAML tests still pass — multiline YAML values with intentional content lines should be unaffected. If any existing test included intentional blank lines in YAML values, it would need updating, but the current tests don't.

**Step 5: Commit**

```
feat(rust-crap): filter blank lines in sanitize_yaml_value

After normalizing line endings and stripping ANSI, filter out
blank/whitespace-only lines. Handles PTY \r\n translation artifacts
that produce spurious empty lines in YAML output blocks.
```

---

## Go tasks

### Task 6: `HasVisibleContent()` public function (Go)

**Files:**
- Modify: `go-crap/crap.go`

**Step 1: Write the failing test**

Add to a new or existing test file `go-crap/crap_test.go`:

```go
func TestHasVisibleContent(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"hello", true},
		{"\x1b[0m\x1b[K", false},
		{"\x1b[32mok\x1b[0m", true},
		{"   \t  ", false},
		{"", false},
		{"\x01\x02\x03", false},
	}
	for _, tt := range tests {
		got := HasVisibleContent(tt.input)
		if got != tt.want {
			t.Errorf("HasVisibleContent(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd go-crap && go test -run TestHasVisibleContent -v`
Expected: FAIL — `HasVisibleContent` undefined

**Step 3: Write minimal implementation**

Add to `go-crap/crap.go`:

```go
// HasVisibleContent reports whether s contains any visible characters after
// skipping ANSI CSI escape sequences, whitespace, and control characters.
func HasVisibleContent(s string) bool {
	i := 0
	runes := []rune(s)
	for i < len(runes) {
		c := runes[i]
		if c == '\x1b' {
			i++
			if i < len(runes) && runes[i] == '[' {
				i++
				for i < len(runes) {
					if runes[i] >= '@' && runes[i] <= '~' {
						i++
						break
					}
					i++
				}
			}
		} else if !unicode.IsSpace(c) && !unicode.IsControl(c) {
			return true
		} else {
			i++
		}
	}
	return false
}
```

Add `"unicode"` to imports.

**Step 4: Run test to verify it passes**

Run: `cd go-crap && go test -run TestHasVisibleContent -v`
Expected: PASS

**Step 5: Commit**

```
feat(go-crap): add HasVisibleContent() public function

Port from rust-crap. Walks runes, skips CSI sequences and
whitespace/control, returns true on first visible character.
```

---

### Task 7: DECAWM wrapping in `UpdateLastLine()` + auto-clear (Go)

**Files:**
- Modify: `go-crap/crap.go`

**Step 1: Write the failing tests**

```go
func TestUpdateLastLineDECAWM(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, true)
	tw.UpdateLastLine("long line")
	out := buf.String()
	if !strings.Contains(out, "\r\x1b[2K\x1b[?7l# long line\x1b[?7h") {
		t.Errorf("expected DECAWM wrapping, got:\n%s", out)
	}
}

func TestUpdateLastLineNoDECAWM(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.UpdateLastLine("line")
	out := buf.String()
	if strings.Contains(out, "\x1b[?7l") {
		t.Errorf("should not contain DECAWM without color, got:\n%s", out)
	}
}

func TestAutoClrBeforeOk(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.UpdateLastLine("building...")
	tw.Ok("build")
	out := buf.String()
	okIdx := strings.LastIndex(out, "ok")
	before := out[:okIdx]
	if !strings.HasSuffix(before, "\r\x1b[2K") {
		t.Errorf("Ok should auto-clear status line, got:\n%s", out)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd go-crap && go test -run 'TestUpdateLastLine|TestAutoClr' -v`
Expected: FAIL

**Step 3: Implement**

Add `statusLineActive bool` field to `Writer` struct.

Modify `UpdateLastLine`:

```go
func (tw *Writer) UpdateLastLine(text string) {
	if tw.color {
		fmt.Fprintf(tw.w, "\r\x1b[2K\x1b[?7l# %s\x1b[?7h", text)
	} else {
		fmt.Fprintf(tw.w, "\r\x1b[2K# %s", text)
	}
	tw.statusLineActive = true
}
```

Modify `FinishLastLine`:

```go
func (tw *Writer) FinishLastLine() {
	tw.statusLineActive = false
	fmt.Fprint(tw.w, "\r\x1b[2K")
}
```

Add auto-clear helper and call it at the top of `Ok`, `OkDiag`, `NotOk`, `Skip`, `SkipDiag`, `Todo`, `BailOut`, and `WriteAll` (before each test point):

```go
func (tw *Writer) clearStatusIfActive() {
	if tw.statusLineActive {
		tw.FinishLastLine()
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd go-crap && go test -v`
Expected: all tests PASS

**Step 5: Commit**

```
feat(go-crap): DECAWM wrapping and auto-clear before test points

UpdateLastLine brackets with \x1b[?7l/\x1b[?7h when color is true.
Ok/NotOk/Skip/Todo/BailOut auto-clear active status lines.
```

---

### Task 8: `StatusLineProcessor` + `Writer.FeedStatusBytes()` (Go)

**Files:**
- Modify: `go-crap/crap.go`

**Step 1: Write the failing tests**

```go
func TestStatusLineProcessorNewline(t *testing.T) {
	p := NewStatusLineProcessor()
	lines := p.Feed([]byte("hello\nworld\n"))
	want := []string{"hello", "world"}
	if !slices.Equal(lines, want) {
		t.Errorf("got %v, want %v", lines, want)
	}
}

func TestStatusLineProcessorCR(t *testing.T) {
	p := NewStatusLineProcessor()
	lines := p.Feed([]byte("evaluating...\rdownloading...\n"))
	want := []string{"evaluating...", "downloading..."}
	if !slices.Equal(lines, want) {
		t.Errorf("got %v, want %v", lines, want)
	}
}

func TestStatusLineProcessorFiltersAnsiOnly(t *testing.T) {
	p := NewStatusLineProcessor()
	lines := p.Feed([]byte("\x1b[0m\x1b[K\nreal content\n"))
	want := []string{"real content"}
	if !slices.Equal(lines, want) {
		t.Errorf("got %v, want %v", lines, want)
	}
}

func TestStatusLineProcessorBuffersPartial(t *testing.T) {
	p := NewStatusLineProcessor()
	lines1 := p.Feed([]byte("partial"))
	if len(lines1) != 0 {
		t.Errorf("expected empty, got %v", lines1)
	}
	lines2 := p.Feed([]byte(" line\n"))
	want := []string{"partial line"}
	if !slices.Equal(lines2, want) {
		t.Errorf("got %v, want %v", lines2, want)
	}
}

func TestFeedStatusBytesWritesStatusLines(t *testing.T) {
	var buf bytes.Buffer
	tw := NewColorWriter(&buf, false)
	tw.FeedStatusBytes([]byte("building...\nlinking...\n"))
	out := buf.String()
	if !strings.Contains(out, "# building...") {
		t.Errorf("expected building status line, got:\n%s", out)
	}
	if !strings.Contains(out, "# linking...") {
		t.Errorf("expected linking status line, got:\n%s", out)
	}
}
```

Add `"slices"` to test imports.

**Step 2: Run test to verify it fails**

Run: `cd go-crap && go test -run 'TestStatusLineProcessor|TestFeedStatusBytes' -v`
Expected: FAIL — `StatusLineProcessor` undefined

**Step 3: Implement**

Add to `go-crap/crap.go`:

```go
// StatusLineProcessor processes raw byte chunks from PTY output into clean
// status lines. Splits on \r and \n, trims whitespace, and filters out lines
// that contain only ANSI escape sequences or whitespace.
type StatusLineProcessor struct {
	buf []byte
}

func NewStatusLineProcessor() *StatusLineProcessor {
	return &StatusLineProcessor{}
}

func (p *StatusLineProcessor) Feed(chunk []byte) []string {
	p.buf = append(p.buf, chunk...)
	var lines []string
	for {
		pos := bytes.IndexAny(p.buf, "\r\n")
		if pos < 0 {
			break
		}
		line := strings.TrimSpace(string(p.buf[:pos]))
		p.buf = p.buf[pos+1:]
		if HasVisibleContent(line) {
			lines = append(lines, line)
		}
	}
	return lines
}
```

Add `FeedStatusBytes` to `Writer`:

```go
func (tw *Writer) FeedStatusBytes(chunk []byte) {
	if tw.statusProcessor == nil {
		tw.statusProcessor = NewStatusLineProcessor()
	}
	for _, line := range tw.statusProcessor.Feed(chunk) {
		tw.UpdateLastLine(line)
	}
}
```

Add `statusProcessor *StatusLineProcessor` field to `Writer` struct.

**Step 4: Run tests to verify they pass**

Run: `cd go-crap && go test -v`
Expected: all tests PASS

**Step 5: Commit**

```
feat(go-crap): add StatusLineProcessor and FeedStatusBytes

StatusLineProcessor splits raw PTY byte chunks on \r/\n, trims, and
filters ANSI-only lines. Writer.FeedStatusBytes is a convenience
that feeds chunks and calls UpdateLastLine for each clean line.
```

---

### Task 9: Blank-line filtering in `sanitizeYAMLValue()` (Go)

**Files:**
- Modify: `go-crap/crap.go`

**Step 1: Write the failing test**

```go
func TestSanitizeYAMLValueFiltersBlankLines(t *testing.T) {
	result := sanitizeYAMLValue("line one\n\n\nline two\n  \nline three", false)
	want := "line one\nline two\nline three"
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}
}

func TestSanitizeYAMLValueFiltersCRLFBlanks(t *testing.T) {
	result := sanitizeYAMLValue("line one\r\n\r\nline two\r\n", false)
	want := "line one\nline two"
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd go-crap && go test -run TestSanitizeYAMLValue -v`
Expected: FAIL — blank lines preserved

**Step 3: Implement**

The Go `sanitizeYAMLValue` currently does ANSI stripping. Add blank-line filtering after stripping. If the function doesn't exist yet as a standalone, find where YAML value sanitization happens (likely inline in `NotOk` or `writeDiagnostics`) and extract + modify:

```go
func sanitizeYAMLValue(value string, color bool) string {
	if color {
		value = stripNonSGR(value)
	} else {
		value = stripANSI(value)
	}
	// Normalize line endings and filter blank lines
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
```

**Step 4: Run tests to verify they pass**

Run: `cd go-crap && go test -v`
Expected: all tests PASS

**Step 5: Commit**

```
feat(go-crap): filter blank lines in sanitizeYAMLValue

After normalizing line endings and stripping ANSI, filter out
blank/whitespace-only lines from YAML diagnostic values.
```

---

### Task 10: Final integration verification

**Step 1: Run full test suite**

Run: `just test`
Expected: all Go and Rust tests PASS

**Step 2: Run nix build**

Run: `just build`
Expected: builds successfully

**Step 3: Commit design + plan docs**

```
docs: add TTY rendering helpers design and implementation plan
```
