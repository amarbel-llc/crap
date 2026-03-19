use std::io::{self, Write};
use std::time::{Duration, Instant};

use fixed_decimal::Decimal;
use icu_decimal::DecimalFormatter;
use icu_locale_core::Locale;

#[derive(Clone)]
pub struct CrapConfig {
    color: bool,
    locale: Option<Locale>,
    formatter: Option<DecimalFormatter>,
    streamed_output: bool,
    status_line: bool,
}

impl CrapConfig {
    pub fn color(&self) -> bool {
        self.color
    }

    pub fn format_number(&self, n: usize) -> String {
        match &self.formatter {
            Some(fmt) => fmt.format(&Decimal::from(n as i64)).to_string(),
            None => n.to_string(),
        }
    }
}

pub struct CrapWriterBuilder<'a> {
    w: &'a mut dyn Write,
    color: bool,
    locale: Option<Locale>,
    status_line: bool,
}

impl<'a> CrapWriterBuilder<'a> {
    pub fn new(w: &'a mut dyn Write) -> Self {
        Self {
            w,
            color: false,
            locale: None,
            status_line: false,
        }
    }

    pub fn auto(w: &'a mut dyn Write) -> Self {
        Self::new(w).default_color().default_locale()
    }

    pub fn color(mut self, color: bool) -> Self {
        self.color = color;
        self
    }

    pub fn locale(mut self, locale: Locale) -> Self {
        self.locale = Some(locale);
        self
    }

    pub fn no_locale(mut self) -> Self {
        self.locale = None;
        self
    }

    pub fn status_line(mut self, enabled: bool) -> Self {
        self.status_line = enabled;
        self
    }

    pub fn default_color(mut self) -> Self {
        self.color = std::env::var("NO_COLOR").is_err();
        self
    }

    pub fn default_locale(mut self) -> Self {
        let locale_str = std::env::var("LC_ALL")
            .or_else(|_| std::env::var("LC_NUMERIC"))
            .or_else(|_| std::env::var("LANG"))
            .ok();
        if let Some(s) = locale_str {
            // Strip .UTF-8 or other encoding suffixes, then normalize
            // POSIX underscores (en_US) to BCP 47 hyphens (en-US)
            let base = s.split('.').next().unwrap_or(&s).replace('_', "-");
            if let Ok(locale) = base.parse::<Locale>() {
                self.locale = Some(locale);
            }
        }
        self
    }

    fn build_config(&self) -> io::Result<CrapConfig> {
        let (locale, formatter) = match &self.locale {
            Some(locale) => {
                let formatter =
                    DecimalFormatter::try_new(locale.clone().into(), Default::default())
                        .map_err(|e| io::Error::other(e.to_string()))?;
                (Some(locale.clone()), Some(formatter))
            }
            None => (None, None),
        };
        Ok(CrapConfig {
            color: self.color,
            locale,
            formatter,
            streamed_output: false,
            status_line: self.status_line,
        })
    }

    pub fn build(self) -> io::Result<CrapWriter<'a>> {
        // Create formatter before any I/O to avoid partial output on error
        let config = self.build_config()?;
        writeln!(self.w, "CRAP-2")?;
        if let Some(ref locale) = config.locale {
            writeln!(self.w, "pragma +locale-formatting:{locale}")?;
        }
        Ok(CrapWriter {
            w: self.w,
            counter: 0,
            failed: false,
            plan_emitted: false,
            status_line_active: false,
            status_processor: None,
            config,
        })
    }

    pub fn build_without_printing(self) -> io::Result<CrapWriter<'a>> {
        let config = self.build_config()?;
        Ok(CrapWriter {
            w: self.w,
            counter: 0,
            failed: false,
            plan_emitted: false,
            status_line_active: false,
            status_processor: None,
            config,
        })
    }
}

fn status_ok(color: bool) -> &'static str {
    if color {
        "\x1b[32mok\x1b[0m"
    } else {
        "ok"
    }
}

fn status_not_ok(color: bool) -> &'static str {
    if color {
        "\x1b[31mnot ok\x1b[0m"
    } else {
        "not ok"
    }
}

fn directive_skip(color: bool) -> &'static str {
    if color {
        "\x1b[33mSKIP\x1b[0m"
    } else {
        "SKIP"
    }
}

fn directive_todo(color: bool) -> &'static str {
    if color {
        "\x1b[33mTODO\x1b[0m"
    } else {
        "TODO"
    }
}

fn directive_warn(color: bool) -> &'static str {
    if color {
        "\x1b[33mWARN\x1b[0m"
    } else {
        "WARN"
    }
}

fn token_bail_out(color: bool) -> &'static str {
    if color {
        "\x1b[31mBail out!\x1b[0m"
    } else {
        "Bail out!"
    }
}

pub struct TestResult {
    pub number: usize,
    pub name: String,
    pub ok: bool,
    pub directive: Option<String>,
    pub error_message: Option<String>,
    pub exit_code: Option<i32>,
    pub output: Option<String>,
    pub suppress_yaml: bool,
}

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

    pub fn feed(&mut self, chunk: &[u8]) -> StatusLineIter<'_> {
        self.buf.extend_from_slice(chunk);
        StatusLineIter { proc: self }
    }
}

pub struct StatusLineIter<'a> {
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

pub struct CrapWriter<'a> {
    w: &'a mut dyn Write,
    counter: usize,
    failed: bool,
    plan_emitted: bool,
    status_line_active: bool,
    status_processor: Option<StatusLineProcessor>,
    pub(crate) config: CrapConfig,
}

impl<'a> CrapWriter<'a> {
    pub fn count(&self) -> usize {
        self.counter
    }

    pub fn has_failures(&self) -> bool {
        self.failed
    }

    fn clear_status_if_active(&mut self) -> io::Result<()> {
        if self.status_line_active {
            self.finish_last_line()?;
        }
        Ok(())
    }

    pub fn ok(&mut self, desc: &str) -> io::Result<usize> {
        self.clear_status_if_active()?;
        self.counter += 1;
        let num = self.config.format_number(self.counter);
        writeln!(
            self.w,
            "{} {} - {}",
            status_ok(self.config.color()),
            num,
            desc
        )?;
        Ok(self.counter)
    }

    pub fn not_ok(&mut self, desc: &str) -> io::Result<usize> {
        self.clear_status_if_active()?;
        self.counter += 1;
        self.failed = true;
        let num = self.config.format_number(self.counter);
        writeln!(
            self.w,
            "{} {} - {}",
            status_not_ok(self.config.color()),
            num,
            desc
        )?;
        Ok(self.counter)
    }

    pub fn not_ok_diag(&mut self, desc: &str, diagnostics: &[(&str, &str)]) -> io::Result<usize> {
        self.clear_status_if_active()?;
        self.counter += 1;
        self.failed = true;
        let num = self.config.format_number(self.counter);
        writeln!(
            self.w,
            "{} {} - {}",
            status_not_ok(self.config.color()),
            num,
            desc
        )?;
        write_diagnostics_block(self.w, diagnostics, self.config.color())?;
        Ok(self.counter)
    }

    pub fn skip(&mut self, desc: &str, reason: &str) -> io::Result<usize> {
        self.clear_status_if_active()?;
        self.counter += 1;
        let num = self.config.format_number(self.counter);
        writeln!(
            self.w,
            "{} {} - {} # {} {}",
            status_ok(self.config.color()),
            num,
            desc,
            directive_skip(self.config.color()),
            reason
        )?;
        Ok(self.counter)
    }

    pub fn todo(&mut self, desc: &str, reason: &str) -> io::Result<usize> {
        self.clear_status_if_active()?;
        self.counter += 1;
        let num = self.config.format_number(self.counter);
        writeln!(
            self.w,
            "{} {} - {} # {} {}",
            status_not_ok(self.config.color()),
            num,
            desc,
            directive_todo(self.config.color()),
            reason
        )?;
        Ok(self.counter)
    }

    pub fn warn(&mut self, desc: &str, reason: &str) -> io::Result<usize> {
        self.clear_status_if_active()?;
        self.counter += 1;
        let num = self.config.format_number(self.counter);
        writeln!(
            self.w,
            "{} {} - {} # {} {}",
            status_ok(self.config.color()),
            num,
            desc,
            directive_warn(self.config.color()),
            reason
        )?;
        Ok(self.counter)
    }

    pub fn warn_not_ok(&mut self, desc: &str, reason: &str) -> io::Result<usize> {
        self.clear_status_if_active()?;
        self.counter += 1;
        self.failed = true;
        let num = self.config.format_number(self.counter);
        writeln!(
            self.w,
            "{} {} - {} # {} {}",
            status_not_ok(self.config.color()),
            num,
            desc,
            directive_warn(self.config.color()),
            reason
        )?;
        Ok(self.counter)
    }

    pub fn bail_out(&mut self, reason: &str) -> io::Result<()> {
        self.clear_status_if_active()?;
        writeln!(self.w, "{} {}", token_bail_out(self.config.color()), reason)
    }

    pub fn comment(&mut self, text: &str) -> io::Result<()> {
        if self.config.color {
            writeln!(self.w, "\x1b[2m# {}\x1b[0m", text)
        } else {
            writeln!(self.w, "# {}", text)
        }
    }

    pub fn update_last_line(&mut self, text: &str) -> io::Result<()> {
        let up = if self.status_line_active {
            "\x1b[A"
        } else {
            ""
        };
        if self.config.color {
            write!(self.w, "{}\r\x1b[2K\x1b[?7l# {}\x1b[?7h\n", up, text)?;
        } else {
            write!(self.w, "{}\r\x1b[2K# {}\n", up, text)?;
        }
        self.status_line_active = true;
        self.w.flush()
    }

    pub fn finish_last_line(&mut self) -> io::Result<()> {
        self.status_line_active = false;
        write!(self.w, "\x1b[A\r\x1b[2K")?;
        self.w.flush()
    }

    pub fn feed_status_bytes(&mut self, chunk: &[u8]) -> io::Result<()> {
        let proc = self
            .status_processor
            .get_or_insert_with(StatusLineProcessor::new);
        let lines: Vec<_> = proc.feed(chunk).collect();
        for line in lines {
            self.update_last_line(&line)?;
        }
        Ok(())
    }

    pub fn pragma(&mut self, key: &str, enabled: bool) -> io::Result<()> {
        let sign = if enabled { "+" } else { "-" };
        writeln!(self.w, "pragma {}{}", sign, key)?;
        if key == "streamed-output" && enabled {
            self.config.streamed_output = true;
        }
        if key == "status-line" && enabled {
            self.config.status_line = true;
        }
        Ok(())
    }

    pub fn plan(&mut self) -> io::Result<()> {
        if self.plan_emitted {
            return Ok(());
        }
        self.plan_emitted = true;
        let num = self.config.format_number(self.counter);
        writeln!(self.w, "1::{}", num)
    }

    pub fn plan_ahead(&mut self, n: usize) -> io::Result<()> {
        self.plan_emitted = true;
        let num = self.config.format_number(n);
        writeln!(self.w, "1::{}", num)
    }

    pub fn plan_skip(&mut self, reason: &str) -> io::Result<()> {
        self.plan_emitted = true;
        writeln!(self.w, "1::0 # SKIP {}", reason)
    }

    pub fn test_point(&mut self, result: &TestResult) -> io::Result<()> {
        self.clear_status_if_active()?;
        self.counter += 1;
        if !result.ok {
            self.failed = true;
        }

        let status = if result.ok {
            status_ok(self.config.color())
        } else {
            status_not_ok(self.config.color())
        };

        let num = self.config.format_number(result.number);
        if let Some(ref directive) = result.directive {
            writeln!(self.w, "{status} {num} - {} # {directive}", result.name)?;
        } else {
            writeln!(self.w, "{status} {num} - {}", result.name)?;
        }

        if !result.suppress_yaml && has_yaml_block(result) {
            writeln!(self.w, "  ---")?;
            if let Some(ref message) = result.error_message {
                write_yaml_field(&mut *self.w, "message", message, self.config.color())?;
            }
            if !result.ok {
                writeln!(self.w, "  severity: fail")?;
            }
            if let Some(code) = result.exit_code {
                writeln!(self.w, "  exitcode: {code}")?;
            }
            if let Some(ref output) = result.output {
                write_yaml_field(&mut *self.w, "output", output, self.config.color())?;
            }
            writeln!(self.w, "  ...")?;
        }

        Ok(())
    }

    pub fn subtest(
        &mut self,
        name: &str,
        f: impl FnOnce(&mut CrapWriter) -> io::Result<()>,
    ) -> io::Result<()> {
        if self.config.color {
            writeln!(self.w, "    \x1b[2m# Subtest: {}\x1b[0m", name)?;
        } else {
            writeln!(self.w, "    # Subtest: {}", name)?;
        }
        let mut indent = IndentWriter { w: &mut *self.w };
        let mut config = self.config.clone();
        config.status_line = false;
        let mut child = CrapWriter {
            w: &mut indent,
            counter: 0,
            failed: false,
            plan_emitted: false,
            status_line_active: false,
            status_processor: None,
            config,
        };
        if let Some(ref locale) = child.config.locale {
            writeln!(child.w, "pragma +locale-formatting:{locale}")?;
        }
        f(&mut child)
    }
}

struct IndentWriter<'a> {
    w: &'a mut dyn Write,
}

impl IndentWriter<'_> {
    fn indent_lines(&mut self, s: &str) -> io::Result<()> {
        let lines: Vec<&str> = s.split('\n').collect();
        for (i, line) in lines.iter().enumerate() {
            if i == lines.len() - 1 && line.is_empty() {
                break;
            }
            let indented = format!("    {}\n", line);
            self.w.write_all(indented.as_bytes())?;
        }
        Ok(())
    }
}

impl Write for IndentWriter<'_> {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        let s =
            std::str::from_utf8(buf).map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
        self.indent_lines(s)?;
        Ok(buf.len())
    }

    fn write_fmt(&mut self, fmt: std::fmt::Arguments<'_>) -> io::Result<()> {
        let s = fmt.to_string();
        self.indent_lines(&s)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.w.flush()
    }
}

fn write_diagnostics_block(
    w: &mut dyn Write,
    diagnostics: &[(&str, &str)],
    color: bool,
) -> io::Result<()> {
    if diagnostics.is_empty() {
        return Ok(());
    }
    writeln!(w, "  ---")?;
    for (key, value) in diagnostics {
        write_yaml_field(w, key, value, color)?;
    }
    writeln!(w, "  ...")
}

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

fn strip_ansi(s: &str) -> String {
    let mut result = String::with_capacity(s.len());
    let mut chars = s.chars();
    while let Some(c) = chars.next() {
        if c == '\x1b' {
            if let Some(next) = chars.next() {
                if next == '[' {
                    // Consume CSI sequence: parameters and final byte
                    for c in chars.by_ref() {
                        if c.is_ascii_alphabetic() {
                            break;
                        }
                    }
                }
                // Non-CSI escape sequence: skip the two chars
            }
        } else {
            result.push(c);
        }
    }
    result
}

fn strip_non_sgr_csi(s: &str) -> String {
    let mut result = String::with_capacity(s.len());
    let bytes = s.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'\x1b' && i + 1 < bytes.len() && bytes[i + 1] == b'[' {
            // Found CSI sequence start, collect the whole sequence
            let start = i;
            i += 2; // skip ESC [
                    // Skip parameter bytes (digits and semicolons)
            while i < bytes.len() && (bytes[i].is_ascii_digit() || bytes[i] == b';') {
                i += 1;
            }
            // Check the final byte
            if i < bytes.len() && bytes[i].is_ascii_alphabetic() {
                if bytes[i] == b'm' {
                    // SGR sequence — preserve it
                    result.push_str(&s[start..=i]);
                }
                // Non-SGR — drop the sequence
                i += 1;
            }
        } else {
            result.push(s[i..].chars().next().unwrap());
            i += s[i..].chars().next().unwrap().len_utf8();
        }
    }
    result
}

fn normalize_line_endings(s: &str) -> String {
    s.replace("\r\n", "\n").replace('\r', "\n")
}

fn sanitize_yaml_value(value: &str, color: bool) -> String {
    let value = normalize_line_endings(value);
    let stripped = if color {
        strip_non_sgr_csi(&value)
    } else {
        strip_ansi(&value)
    };
    stripped
        .lines()
        .filter(|line| !line.trim().is_empty())
        .collect::<Vec<_>>()
        .join("\n")
}

fn write_yaml_field(
    w: &mut (impl Write + ?Sized),
    key: &str,
    value: &str,
    color: bool,
) -> io::Result<()> {
    let value = sanitize_yaml_value(value, color);
    if value.contains('\n') {
        writeln!(w, "  {key}: |")?;
        for line in value.lines() {
            writeln!(w, "    {line}")?;
        }
    } else {
        writeln!(w, "  {key}: \"{value}\"")?;
    }
    Ok(())
}

fn has_yaml_block(result: &TestResult) -> bool {
    !result.ok || result.output.is_some()
}

// --- Free functions (original API, unchanged) ---

pub fn write_version(w: &mut impl Write) -> io::Result<()> {
    writeln!(w, "CRAP-2")
}

pub fn write_plan(w: &mut impl Write, count: usize) -> io::Result<()> {
    writeln!(w, "1::{count}")
}

pub fn write_test_point(w: &mut impl Write, result: &TestResult) -> io::Result<()> {
    let status = if result.ok { "ok" } else { "not ok" };
    if let Some(ref directive) = result.directive {
        writeln!(
            w,
            "{status} {} - {} # {directive}",
            result.number, result.name
        )?;
    } else {
        writeln!(w, "{status} {} - {}", result.number, result.name)?;
    }

    if !result.suppress_yaml && has_yaml_block(result) {
        writeln!(w, "  ---")?;
        if let Some(ref message) = result.error_message {
            write_yaml_field(w, "message", message, false)?;
        }
        if !result.ok {
            writeln!(w, "  severity: fail")?;
        }
        if let Some(code) = result.exit_code {
            writeln!(w, "  exitcode: {code}")?;
        }
        if let Some(ref output) = result.output {
            write_yaml_field(w, "output", output, false)?;
        }
        writeln!(w, "  ...")?;
    }

    Ok(())
}

pub fn write_bail_out(w: &mut impl Write, reason: &str) -> io::Result<()> {
    writeln!(w, "Bail out! {reason}")
}

pub fn write_comment(w: &mut impl Write, text: &str) -> io::Result<()> {
    writeln!(w, "# {text}")
}

pub fn write_skip(w: &mut impl Write, num: usize, desc: &str, reason: &str) -> io::Result<()> {
    writeln!(w, "ok {num} - {desc} # SKIP {reason}")
}

pub fn write_todo(w: &mut impl Write, num: usize, desc: &str, reason: &str) -> io::Result<()> {
    writeln!(w, "not ok {num} - {desc} # TODO {reason}")
}

// --- New free functions ---

pub fn write_pragma(w: &mut impl Write, key: &str, enabled: bool) -> io::Result<()> {
    let sign = if enabled { "+" } else { "-" };
    writeln!(w, "pragma {sign}{key}")
}

pub fn write_plan_skip(w: &mut impl Write, reason: &str) -> io::Result<()> {
    writeln!(w, "1::0 # SKIP {reason}")
}

pub fn write_warn(w: &mut impl Write, num: usize, desc: &str, reason: &str) -> io::Result<()> {
    writeln!(w, "ok {num} - {desc} # WARN {reason}")
}

pub fn write_plan_locale(
    w: &mut impl Write,
    count: usize,
    fmt: &DecimalFormatter,
) -> io::Result<()> {
    let decimal = Decimal::from(count as i64);
    let formatted = fmt.format(&decimal);
    writeln!(w, "1::{formatted}")
}

const MONKEY_FRAMES: &[&str] = &["🙈", "🙉", "🙊"];
const SPINNER_MIN_DUR: Duration = Duration::from_millis(333); // 3fps cap
const SPINNER_SLEEP_AFTER: Duration = Duration::from_secs(5);

/// Cycling spinner that advances on content updates, rate-limited to 3fps.
///
/// Appends 💤 when no [`touch`](Spinner::touch) call has occurred for 5 seconds,
/// signaling the process is alive but idle.
///
/// The spinner is a pure state machine — it does no I/O and owns no threads.
/// Call [`prefix`](Spinner::prefix) from your content-producing code, and
/// [`current_prefix`](Spinner::current_prefix) from a background re-render loop.
///
/// # Example: background ticker with `std::thread`
///
/// The spinner is designed to work with `std::thread::scope`. Put the spinner and
/// content in a `Mutex`, then share between a ticker thread and worker thread.
/// The `CrapWriter` must also be behind a `Mutex` since both threads update it.
///
/// ```rust,ignore
/// use std::sync::{atomic::{AtomicBool, Ordering}, Mutex};
/// use std::time::Duration;
/// use rust_crap::{Spinner, CrapWriterBuilder};
///
/// let mut buf = std::io::stdout().lock();
/// let tw = Mutex::new(
///     CrapWriterBuilder::new(&mut buf)
///         .status_line(true)
///         .build()
///         .unwrap(),
/// );
/// let spinner = Mutex::new(Spinner::new());
/// let content = Mutex::new(String::new());
/// let stop = AtomicBool::new(false);
///
/// std::thread::scope(|s| {
///     // Ticker thread: re-renders at spinner frame rate.
///     // Uses current_prefix() which does NOT advance the frame.
///     s.spawn(|| {
///         while !stop.load(Ordering::Relaxed) {
///             std::thread::sleep(Duration::from_millis(333));
///             let sp = spinner.lock().unwrap();
///             let c = content.lock().unwrap();
///             if !c.is_empty() {
///                 let pfx = sp.formatted_current_prefix();
///                 let _ = tw.lock().unwrap().update_last_line(&format!("{pfx}{c}"));
///             }
///         }
///     });
///
///     // Worker: produces content and advances the spinner.
///     for line in ["compiling...", "linking...", "done"] {
///         {
///             let mut sp = spinner.lock().unwrap();
///             sp.touch();
///             let pfx = sp.formatted_prefix();
///             let mut c = content.lock().unwrap();
///             *c = line.to_string();
///             let _ = tw.lock().unwrap().update_last_line(&format!("{pfx}{c}"));
///         }
///         std::thread::sleep(Duration::from_millis(500));
///     }
///
///     stop.store(true, Ordering::Relaxed);
/// });
///
/// // After stopping the ticker, finish the status line and emit results.
/// tw.lock().unwrap().finish_last_line().unwrap();
/// ```
pub struct Spinner {
    frames: &'static [&'static str],
    index: usize,
    last_advance: Option<Instant>,
    last_content: Option<Instant>,
    min_dur: Duration,
    sleep_after: Duration,
    disabled: bool,
}

impl Default for Spinner {
    fn default() -> Self {
        Self::new()
    }
}

impl Spinner {
    pub fn new() -> Self {
        Self {
            frames: MONKEY_FRAMES,
            index: 0,
            last_advance: None,
            last_content: None,
            min_dur: SPINNER_MIN_DUR,
            sleep_after: SPINNER_SLEEP_AFTER,
            disabled: false,
        }
    }

    /// Create a disabled spinner whose prefix methods always return empty strings.
    pub fn disabled() -> Self {
        Self {
            disabled: true,
            ..Self::new()
        }
    }

    /// Signal that new content arrived, resetting the sleep timer.
    pub fn touch(&mut self) {
        self.last_content = Some(Instant::now());
    }

    /// Advance the spinner (rate-limited) and return the current frame + trailing
    /// space. Returns empty string when disabled. Call this when new content arrives.
    pub fn prefix(&mut self) -> &'static str {
        if self.disabled {
            return "";
        }
        let now = Instant::now();
        let should_advance = match self.last_advance {
            None => true,
            Some(t) => now.duration_since(t) >= self.min_dur,
        };
        if should_advance {
            self.index = (self.index + 1) % self.frames.len();
            self.last_advance = Some(now);
        }
        self.frames[self.index]
    }

    /// Return the current frame without advancing. Call this from a ticker thread
    /// to re-render without progressing the animation. Returns empty string when
    /// disabled.
    pub fn current_prefix(&self) -> &'static str {
        if self.disabled {
            return "";
        }
        self.frames[self.index]
    }

    /// Whether the spinner is in the sleeping (💤) state — no content update
    /// for longer than the sleep threshold.
    pub fn is_sleeping(&self) -> bool {
        match self.last_content {
            None => false,
            Some(t) => Instant::now().duration_since(t) >= self.sleep_after,
        }
    }

    /// Format the full prefix string including 💤 indicator and trailing space.
    /// Allocates only when sleeping; returns a static str otherwise.
    ///
    /// For the non-allocating path, use [`current_prefix`](Spinner::current_prefix)
    /// or [`prefix`](Spinner::prefix) and check [`is_sleeping`](Spinner::is_sleeping)
    /// separately.
    pub fn formatted_prefix(&mut self) -> String {
        if self.disabled {
            return String::new();
        }
        let frame = self.prefix();
        if self.is_sleeping() {
            format!("{frame}💤 ")
        } else {
            format!("{frame} ")
        }
    }

    /// Like [`formatted_prefix`](Spinner::formatted_prefix) but does not advance
    /// the frame. Use from ticker threads.
    pub fn formatted_current_prefix(&self) -> String {
        if self.disabled {
            return String::new();
        }
        let frame = self.current_prefix();
        if self.is_sleeping() {
            format!("{frame}💤 ")
        } else {
            format!("{frame} ")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Mutex;

    // Env-var tests must not run in parallel — they share process-wide state.
    static ENV_MUTEX: Mutex<()> = Mutex::new(());

    // --- Free function tests (existing, unchanged) ---

    #[test]
    fn version_line() {
        let mut buf = Vec::new();
        write_version(&mut buf).unwrap();
        assert_eq!(String::from_utf8(buf).unwrap(), "CRAP-2\n");
    }

    #[test]
    fn plan_line() {
        let mut buf = Vec::new();
        write_plan(&mut buf, 3).unwrap();
        assert_eq!(String::from_utf8(buf).unwrap(), "1::3\n");
    }

    #[test]
    fn plan_zero() {
        let mut buf = Vec::new();
        write_plan(&mut buf, 0).unwrap();
        assert_eq!(String::from_utf8(buf).unwrap(), "1::0\n");
    }

    #[test]
    fn passing_test_point() {
        let mut buf = Vec::new();
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
        write_test_point(&mut buf, &result).unwrap();
        assert_eq!(String::from_utf8(buf).unwrap(), "ok 1 - build\n");
    }

    #[test]
    fn passing_test_point_with_output() {
        let mut buf = Vec::new();
        let result = TestResult {
            number: 1,
            name: "build".into(),
            ok: true,
            directive: None,
            error_message: None,
            exit_code: None,
            output: Some("building\n".into()),
            suppress_yaml: false,
        };
        write_test_point(&mut buf, &result).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("ok 1 - build\n"));
        assert!(out.contains("  ---\n"));
        assert!(out.contains("  output: \"building\"\n"));
        assert!(out.contains("  ...\n"));
    }

    #[test]
    fn failing_test_point() {
        let mut buf = Vec::new();
        let result = TestResult {
            number: 2,
            name: "test".into(),
            ok: false,
            directive: None,
            error_message: Some("something failed".into()),
            exit_code: Some(1),
            output: None,
            suppress_yaml: false,
        };
        write_test_point(&mut buf, &result).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("not ok 2 - test\n"));
        assert!(out.contains("  ---\n"));
        assert!(out.contains("  message: \"something failed\"\n"));
        assert!(out.contains("  severity: fail\n"));
        assert!(out.contains("  exitcode: 1\n"));
        assert!(out.contains("  ...\n"));
    }

    #[test]
    fn failing_test_point_with_multiline_output() {
        let mut buf = Vec::new();
        let result = TestResult {
            number: 1,
            name: "multi".into(),
            ok: false,
            directive: None,
            error_message: None,
            exit_code: None,
            output: Some("line one\nline two".into()),
            suppress_yaml: false,
        };
        write_test_point(&mut buf, &result).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("  output: |\n"));
        assert!(out.contains("    line one\n"));
        assert!(out.contains("    line two\n"));
    }

    #[test]
    fn bail_out() {
        let mut buf = Vec::new();
        write_bail_out(&mut buf, "database down").unwrap();
        assert_eq!(String::from_utf8(buf).unwrap(), "Bail out! database down\n");
    }

    #[test]
    fn comment() {
        let mut buf = Vec::new();
        write_comment(&mut buf, "a note").unwrap();
        assert_eq!(String::from_utf8(buf).unwrap(), "# a note\n");
    }

    #[test]
    fn skip_directive() {
        let mut buf = Vec::new();
        write_skip(&mut buf, 3, "optional feature", "not supported").unwrap();
        assert_eq!(
            String::from_utf8(buf).unwrap(),
            "ok 3 - optional feature # SKIP not supported\n"
        );
    }

    #[test]
    fn todo_directive() {
        let mut buf = Vec::new();
        write_todo(&mut buf, 4, "future work", "not implemented").unwrap();
        assert_eq!(
            String::from_utf8(buf).unwrap(),
            "not ok 4 - future work # TODO not implemented\n"
        );
    }

    // --- New free function tests ---

    #[test]
    fn pragma_enable() {
        let mut buf = Vec::new();
        write_pragma(&mut buf, "strict", true).unwrap();
        assert_eq!(String::from_utf8(buf).unwrap(), "pragma +strict\n");
    }

    #[test]
    fn pragma_disable() {
        let mut buf = Vec::new();
        write_pragma(&mut buf, "strict", false).unwrap();
        assert_eq!(String::from_utf8(buf).unwrap(), "pragma -strict\n");
    }

    #[test]
    fn plan_skip_free() {
        let mut buf = Vec::new();
        write_plan_skip(&mut buf, "not supported on this platform").unwrap();
        assert_eq!(
            String::from_utf8(buf).unwrap(),
            "1::0 # SKIP not supported on this platform\n"
        );
    }

    // --- CrapWriter method tests ---

    #[test]
    fn writer_emits_version() {
        let mut buf = Vec::new();
        let _tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        assert_eq!(String::from_utf8(buf).unwrap(), "CRAP-2\n");
    }

    #[test]
    fn writer_ok() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        let n = tw.ok("first test").unwrap();
        assert_eq!(n, 1);
        let n = tw.ok("second test").unwrap();
        assert_eq!(n, 2);
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("ok 1 - first test\n"));
        assert!(out.contains("ok 2 - second test\n"));
    }

    #[test]
    fn writer_not_ok() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        let n = tw.not_ok("broken").unwrap();
        assert_eq!(n, 1);
        assert!(tw.has_failures());
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("not ok 1 - broken\n"));
    }

    #[test]
    fn writer_not_ok_diag() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.not_ok_diag("broken", &[("message", "segfault"), ("severity", "fail")])
            .unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("not ok 1 - broken\n"));
        assert!(out.contains("  ---\n"));
        assert!(out.contains("  message: \"segfault\"\n"));
        assert!(out.contains("  severity: \"fail\"\n"));
        assert!(out.contains("  ...\n"));
    }

    #[test]
    fn writer_warn() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        let n = tw.warn("slow query", "took 3.2s").unwrap();
        assert_eq!(n, 1);
        assert!(!tw.has_failures());
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("ok 1 - slow query # WARN took 3.2s\n"));
    }

    #[test]
    fn writer_warn_not_ok() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        let n = tw.warn_not_ok("flaky", "intermittent").unwrap();
        assert_eq!(n, 1);
        assert!(tw.has_failures());
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("not ok 1 - flaky # WARN intermittent\n"));
    }

    #[test]
    fn warn_free_function() {
        let mut buf = Vec::new();
        write_warn(&mut buf, 7, "database query", "took 3.2s, exceeds 1s threshold").unwrap();
        assert_eq!(
            String::from_utf8(buf).unwrap(),
            "ok 7 - database query # WARN took 3.2s, exceeds 1s threshold\n"
        );
    }

    #[test]
    fn writer_not_ok_diag_multiline() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.not_ok_diag("broken", &[("output", "line one\nline two")])
            .unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("  output: |\n"));
        assert!(out.contains("    line one\n"));
        assert!(out.contains("    line two\n"));
    }

    #[test]
    fn writer_not_ok_diag_empty() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.not_ok_diag("broken", &[]).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("not ok 1 - broken\n"));
        assert!(!out.contains("---"));
    }

    #[test]
    fn writer_skip() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        let n = tw.skip("optional", "not supported").unwrap();
        assert_eq!(n, 1);
        assert!(!tw.has_failures());
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("ok 1 - optional # SKIP not supported\n"));
    }

    #[test]
    fn writer_todo() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        let n = tw.todo("future", "not done").unwrap();
        assert_eq!(n, 1);
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("not ok 1 - future # TODO not done\n"));
    }

    #[test]
    fn writer_bail_out() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.bail_out("on fire").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("Bail out! on fire\n"));
    }

    #[test]
    fn writer_comment() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.comment("a note").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("# a note\n"));
    }

    #[test]
    fn writer_pragma() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.pragma("strict", true).unwrap();
        tw.pragma("strict", false).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("pragma +strict\n"));
        assert!(out.contains("pragma -strict\n"));
    }

    #[test]
    fn writer_trailing_plan() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.ok("one").unwrap();
        tw.ok("two").unwrap();
        tw.plan().unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.ends_with("1::2\n"));
    }

    #[test]
    fn writer_plan_idempotent() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.ok("one").unwrap();
        tw.plan().unwrap();
        tw.plan().unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert_eq!(out.matches("1::1").count(), 1);
    }

    #[test]
    fn writer_plan_ahead() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.plan_ahead(5).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("1::5\n"));
    }

    #[test]
    fn writer_plan_skip() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.plan_skip("missing dependency").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("1::0 # SKIP missing dependency\n"));
    }

    #[test]
    fn writer_counter() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        assert_eq!(tw.count(), 0);
        tw.ok("a").unwrap();
        assert_eq!(tw.count(), 1);
        tw.ok("b").unwrap();
        assert_eq!(tw.count(), 2);
    }

    #[test]
    fn writer_has_failures_tracks_not_ok() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        assert!(!tw.has_failures());
        tw.ok("pass").unwrap();
        assert!(!tw.has_failures());
        tw.not_ok("fail").unwrap();
        assert!(tw.has_failures());
    }

    #[test]
    fn writer_subtest() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.subtest("group", |sub| {
            sub.ok("nested one")?;
            sub.ok("nested two")?;
            sub.plan()
        })
        .unwrap();
        tw.ok("group").unwrap();
        tw.plan().unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("    # Subtest: group\n"));
        assert!(out.contains("    ok 1 - nested one\n"));
        assert!(out.contains("    ok 2 - nested two\n"));
        assert!(out.contains("    1::2\n"));
        assert!(out.contains("ok 1 - group\n"));
        assert!(out.ends_with("1::1\n"));
    }

    #[test]
    fn writer_nested_subtest() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.subtest("outer", |sub| {
            sub.ok("before")?;
            sub.subtest("inner", |inner| {
                inner.ok("deep")?;
                inner.plan()
            })?;
            sub.ok("inner")?;
            sub.plan()
        })
        .unwrap();
        tw.ok("outer").unwrap();
        tw.plan().unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("    # Subtest: outer\n"));
        assert!(out.contains("    ok 1 - before\n"));
        assert!(out.contains("        # Subtest: inner\n"));
        assert!(out.contains("        ok 1 - deep\n"));
        assert!(out.contains("        1::1\n"));
        assert!(out.contains("    ok 2 - inner\n"));
        assert!(out.contains("    1::2\n"));
    }

    #[test]
    fn writer_subtest_with_skip() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.subtest("optional", |sub| {
            sub.skip("feature x", "not available")?;
            sub.plan()
        })
        .unwrap();
        tw.ok("optional").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("    ok 1 - feature x # SKIP not available\n"));
    }

    #[test]
    fn writer_subtest_with_pragma() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.subtest("streaming", |sub| {
            sub.pragma("streamed-output", true)?;
            sub.ok("step one")?;
            sub.plan()
        })
        .unwrap();
        tw.ok("streaming").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("    pragma +streamed-output\n"));
    }

    #[test]
    fn writer_subtest_inherits_streamed_output() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.pragma("streamed-output", true).unwrap();
        tw.subtest("group", |sub| {
            sub.comment("compiling")?;
            sub.ok("build")?;
            sub.plan()
        })
        .unwrap();
        tw.ok("group").unwrap();
        tw.plan().unwrap();
        let out = String::from_utf8(buf).unwrap();
        // Streamed output is inherited internally; no pragma line needed in subtest
        // since it's enabled by default in CRAP-2
        assert!(
            out.contains("    # compiling\n"),
            "expected subtest to contain comment, got:\n{out}"
        );
    }

    // --- Directive/comment on TestResult ---

    #[test]
    fn test_point_with_directive() {
        let mut buf = Vec::new();
        let result = TestResult {
            number: 1,
            name: "optional feature".into(),
            ok: true,
            directive: Some("SKIP not supported".into()),
            error_message: None,
            exit_code: None,
            output: None,
            suppress_yaml: false,
        };
        write_test_point(&mut buf, &result).unwrap();
        assert_eq!(
            String::from_utf8(buf).unwrap(),
            "ok 1 - optional feature # SKIP not supported\n"
        );
    }

    #[test]
    fn test_point_without_directive() {
        let mut buf = Vec::new();
        let result = TestResult {
            number: 1,
            name: "plain".into(),
            ok: true,
            directive: None,
            error_message: None,
            exit_code: None,
            output: None,
            suppress_yaml: false,
        };
        write_test_point(&mut buf, &result).unwrap();
        assert_eq!(String::from_utf8(buf).unwrap(), "ok 1 - plain\n");
    }

    // --- Carriage return stripping ---

    #[test]
    fn yaml_field_strips_cr_lf() {
        let mut buf = Vec::new();
        write_yaml_field(&mut buf, "output", "line one\r\nline two\r\n", false).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(!out.contains('\r'));
        assert!(out.contains("  output: |\n"));
        assert!(out.contains("    line one\n"));
        assert!(out.contains("    line two\n"));
    }

    #[test]
    fn yaml_field_strips_bare_cr() {
        let mut buf = Vec::new();
        write_yaml_field(&mut buf, "message", "hello\rworld", false).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(!out.contains('\r'));
        assert!(out.contains("  message: |\n"));
        assert!(out.contains("    hello\n"));
        assert!(out.contains("    world\n"));
    }

    #[test]
    fn yaml_field_filters_blank_lines() {
        let mut buf = Vec::new();
        write_yaml_field(&mut buf, "output", "line one\n\n\nline two\n  \nline three", false)
            .unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("  output: |\n"));
        assert!(out.contains("    line one\n"));
        assert!(out.contains("    line two\n"));
        assert!(out.contains("    line three\n"));
        assert!(!out.contains("    \n"));
    }

    #[test]
    fn yaml_field_single_line_after_blank_filter() {
        let mut buf = Vec::new();
        write_yaml_field(&mut buf, "output", "\n\nhello\n\n", false).unwrap();
        let out = String::from_utf8(buf).unwrap();
        // After filtering blanks, only "hello" remains — single-line format
        assert!(out.contains("  output:"));
        assert!(out.contains("hello"));
        assert!(!out.contains("|\n"));
    }

    // --- ANSI escape code stripping ---

    #[test]
    fn yaml_field_strips_ansi_sgr() {
        let mut buf = Vec::new();
        write_yaml_field(&mut buf, "message", "\x1b[31merror\x1b[0m happened", false).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert_eq!(out, "  message: \"error happened\"\n");
    }

    #[test]
    fn yaml_field_strips_ansi_csi_non_sgr() {
        let mut buf = Vec::new();
        write_yaml_field(&mut buf, "output", "\x1b[2Jcleared screen", false).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert_eq!(out, "  output: \"cleared screen\"\n");
    }

    #[test]
    fn yaml_field_preserves_plain_text() {
        let mut buf = Vec::new();
        write_yaml_field(&mut buf, "message", "no escapes here", false).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert_eq!(out, "  message: \"no escapes here\"\n");
    }

    #[test]
    fn strip_ansi_function() {
        assert_eq!(strip_ansi("\x1b[32mok\x1b[0m"), "ok");
        assert_eq!(strip_ansi("\x1b[31mnot ok\x1b[0m"), "not ok");
        assert_eq!(strip_ansi("\x1b[2Jafter clear"), "after clear");
        assert_eq!(strip_ansi("no escapes"), "no escapes");
    }

    // --- has_visible_content ---

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

    // --- Suppress YAML block mode ---

    #[test]
    fn test_point_suppress_yaml() {
        let mut buf = Vec::new();
        let result = TestResult {
            number: 1,
            name: "failing".into(),
            ok: false,
            directive: None,
            error_message: Some("bad stuff".into()),
            exit_code: Some(1),
            output: Some("verbose output".into()),
            suppress_yaml: true,
        };
        write_test_point(&mut buf, &result).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert_eq!(out, "not ok 1 - failing\n");
    }

    #[test]
    fn test_point_no_suppress_yaml() {
        let mut buf = Vec::new();
        let result = TestResult {
            number: 1,
            name: "failing".into(),
            ok: false,
            directive: None,
            error_message: Some("bad".into()),
            exit_code: None,
            output: None,
            suppress_yaml: false,
        };
        write_test_point(&mut buf, &result).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("  ---\n"));
        assert!(out.contains("  message: \"bad\"\n"));
    }

    // --- normalize_line_endings ---

    #[test]
    fn normalize_crlf() {
        assert_eq!(normalize_line_endings("a\r\nb\r\n"), "a\nb\n");
    }

    #[test]
    fn normalize_bare_cr() {
        assert_eq!(normalize_line_endings("a\rb"), "a\nb");
    }

    #[test]
    fn normalize_lf_unchanged() {
        assert_eq!(normalize_line_endings("a\nb"), "a\nb");
    }

    // --- Color tests ---

    #[test]
    fn writer_ok_color() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).color(true).build().unwrap();
        tw.ok("pass").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("\x1b[32mok\x1b[0m 1 - pass\n"));
    }

    #[test]
    fn writer_not_ok_color() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).color(true).build().unwrap();
        tw.not_ok("fail").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("\x1b[31mnot ok\x1b[0m 1 - fail\n"));
    }

    #[test]
    fn writer_skip_color() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).color(true).build().unwrap();
        tw.skip("optional", "not supported").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.contains("\x1b[32mok\x1b[0m 1 - optional # \x1b[33mSKIP\x1b[0m not supported\n")
        );
    }

    #[test]
    fn writer_todo_color() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).color(true).build().unwrap();
        tw.todo("future", "not done").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("\x1b[31mnot ok\x1b[0m 1 - future # \x1b[33mTODO\x1b[0m not done\n"));
    }

    #[test]
    fn writer_bail_out_color() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).color(true).build().unwrap();
        tw.bail_out("on fire").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("\x1b[31mBail out!\x1b[0m on fire\n"));
    }

    #[test]
    fn writer_test_point_ok_color() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).color(true).build().unwrap();
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
        assert!(out.contains("\x1b[32mok\x1b[0m 1 - build\n"));
    }

    #[test]
    fn writer_test_point_not_ok_color() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).color(true).build().unwrap();
        let result = TestResult {
            number: 1,
            name: "test".into(),
            ok: false,
            directive: None,
            error_message: Some("boom".into()),
            exit_code: Some(1),
            output: None,
            suppress_yaml: false,
        };
        tw.test_point(&result).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("\x1b[31mnot ok\x1b[0m 1 - test\n"));
        assert!(out.contains("  severity: fail\n"));
    }

    #[test]
    fn writer_bare_no_version() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .build_without_printing()
            .unwrap();
        tw.ok("first").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(!out.contains("CRAP version"));
        assert!(out.contains("ok 1 - first\n"));
    }

    #[test]
    fn writer_subtest_propagates_color() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).color(true).build().unwrap();
        tw.subtest("group", |sub| {
            sub.ok("nested")?;
            sub.plan()
        })
        .unwrap();
        tw.ok("group").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("    \x1b[32mok\x1b[0m 1 - nested\n"));
        assert!(out.contains("\x1b[32mok\x1b[0m 1 - group\n"));
    }

    // --- Locale formatting tests ---

    #[test]
    fn writer_locale_emits_pragma() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .locale(locale)
            .build()
            .unwrap();
        tw.ok("first").unwrap();
        tw.plan().unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.contains("pragma +locale-formatting:en-US\n"),
            "expected locale pragma, got:\n{out}"
        );
    }

    #[test]
    fn writer_locale_formats_large_test_number() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .locale(locale)
            .build()
            .unwrap();
        for _ in 0..1234 {
            tw.ok("test").unwrap();
        }
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.contains("ok 1,234 - test\n"),
            "expected 'ok 1,234', got last 200 chars: {}",
            &out[out.len().saturating_sub(200)..]
        );
    }

    #[test]
    fn writer_locale_formats_plan_count() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .locale(locale)
            .build()
            .unwrap();
        tw.plan_ahead(10000).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.contains("1::10,000\n"),
            "expected '1::10,000', got: {out}"
        );
    }

    #[test]
    fn writer_locale_german_separator() {
        let mut buf = Vec::new();
        let locale: Locale = "de-DE".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .locale(locale)
            .build()
            .unwrap();
        tw.plan_ahead(10000).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.contains("1::10.000\n"),
            "expected '1::10.000', got: {out}"
        );
    }

    #[test]
    fn writer_locale_small_numbers_no_separator() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .locale(locale)
            .build()
            .unwrap();
        tw.ok("test").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("ok 1 - test\n"));
    }

    #[test]
    fn writer_locale_subtest_inherits() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .locale(locale)
            .build()
            .unwrap();
        tw.subtest("nested", |sub| {
            sub.plan_ahead(10000)?;
            sub.plan()
        })
        .unwrap();
        tw.ok("nested").unwrap();
        tw.plan().unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.contains("    pragma +locale-formatting:en-US\n"),
            "expected subtest locale pragma, got:\n{out}"
        );
        assert!(
            out.contains("    1::10,000\n"),
            "expected subtest formatted plan, got:\n{out}"
        );
    }

    #[test]
    fn write_plan_locale_free_fn() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let formatter = DecimalFormatter::try_new(locale.into(), Default::default()).unwrap();
        write_plan_locale(&mut buf, 10000, &formatter).unwrap();
        assert_eq!(String::from_utf8(buf).unwrap(), "1::10,000\n");
    }

    // --- ANSI in YAML Output Blocks amendment tests ---

    #[test]
    fn strip_non_sgr_csi_preserves_sgr() {
        assert_eq!(strip_non_sgr_csi("\x1b[32mok\x1b[0m"), "\x1b[32mok\x1b[0m");
        assert_eq!(
            strip_non_sgr_csi("\x1b[31;1mbold red\x1b[0m"),
            "\x1b[31;1mbold red\x1b[0m"
        );
    }

    #[test]
    fn strip_non_sgr_csi_removes_non_sgr() {
        assert_eq!(strip_non_sgr_csi("\x1b[2Jcleared"), "cleared");
        assert_eq!(strip_non_sgr_csi("\x1b[Hcursor home"), "cursor home");
        assert_eq!(strip_non_sgr_csi("\x1b[3Aup three"), "up three");
    }

    #[test]
    fn strip_non_sgr_csi_handles_mixed() {
        assert_eq!(
            strip_non_sgr_csi("\x1b[2J\x1b[31merror\x1b[0m text"),
            "\x1b[31merror\x1b[0m text"
        );
    }

    #[test]
    fn strip_non_sgr_csi_plain_text() {
        assert_eq!(strip_non_sgr_csi("no escapes"), "no escapes");
    }

    #[test]
    fn yaml_field_preserves_sgr_when_color_enabled() {
        let mut buf = Vec::new();
        write_yaml_field(&mut buf, "message", "\x1b[31merror\x1b[0m text", true).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.contains("\x1b[31merror\x1b[0m text"),
            "expected SGR preserved, got: {out}"
        );
    }

    #[test]
    fn yaml_field_strips_non_sgr_csi_when_color_enabled() {
        let mut buf = Vec::new();
        write_yaml_field(&mut buf, "output", "\x1b[2J\x1b[31merror\x1b[0m", true).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            !out.contains("\x1b[2J"),
            "expected non-SGR stripped, got: {out}"
        );
        assert!(
            out.contains("\x1b[31merror\x1b[0m"),
            "expected SGR preserved, got: {out}"
        );
    }

    #[test]
    fn yaml_field_strips_all_ansi_when_color_disabled() {
        let mut buf = Vec::new();
        write_yaml_field(&mut buf, "message", "\x1b[31merror\x1b[0m text", false).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            !out.contains("\x1b["),
            "expected all ANSI stripped, got: {out}"
        );
        assert!(
            out.contains("error text"),
            "expected clean text, got: {out}"
        );
    }

    #[test]
    fn writer_test_point_preserves_sgr_in_yaml_when_color() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).color(true).build().unwrap();
        let result = TestResult {
            number: 1,
            name: "test".into(),
            ok: false,
            directive: None,
            error_message: Some("\x1b[31mfatal error\x1b[0m".into()),
            exit_code: Some(1),
            output: Some("\x1b[33mwarning\x1b[0m: details".into()),
            suppress_yaml: false,
        };
        tw.test_point(&result).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.contains("\x1b[31mfatal error\x1b[0m"),
            "expected SGR in message, got:\n{out}"
        );
        assert!(
            out.contains("\x1b[33mwarning\x1b[0m"),
            "expected SGR in output, got:\n{out}"
        );
    }

    #[test]
    fn config_format_number_no_locale() {
        let config = CrapConfig {
            color: false,
            locale: None,
            formatter: None,
            streamed_output: false,
            status_line: false,
        };
        assert_eq!(config.format_number(1234), "1234");
    }

    #[test]
    fn config_format_number_with_locale() {
        let locale: Locale = "en-US".parse().unwrap();
        let formatter =
            DecimalFormatter::try_new(locale.clone().into(), Default::default()).unwrap();
        let config = CrapConfig {
            color: true,
            locale: Some(locale),
            formatter: Some(formatter),
            streamed_output: false,
            status_line: false,
        };
        assert_eq!(config.format_number(1234), "1,234");
    }

    #[test]
    fn config_color_accessor() {
        let config = CrapConfig {
            color: true,
            locale: None,
            formatter: None,
            streamed_output: false,
            status_line: false,
        };
        assert!(config.color());
    }

    #[test]
    fn builder_new_defaults() {
        let mut buf = Vec::new();
        let count;
        let color;
        {
            let tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
            count = tw.count();
            color = tw.config.color();
        }
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("CRAP-2\n"));
        assert!(!color);
        assert_eq!(count, 0);
    }

    #[test]
    fn builder_with_color() {
        let mut buf = Vec::new();
        let color;
        {
            let tw = CrapWriterBuilder::new(&mut buf).color(true).build().unwrap();
            color = tw.config.color();
        }
        assert!(color);
    }

    #[test]
    fn builder_with_locale() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .locale(locale)
            .build()
            .unwrap();
        tw.plan_ahead(10000).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("pragma +locale-formatting:en-US\n"));
        assert!(out.contains("1::10,000\n"));
    }

    #[test]
    fn builder_with_color_and_locale() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .color(true)
            .locale(locale)
            .build()
            .unwrap();
        tw.ok("test").unwrap();
        tw.plan_ahead(10000).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("pragma +locale-formatting:en-US\n"));
        assert!(out.contains("\x1b[32mok\x1b[0m 1 - test\n"));
        assert!(out.contains("1::10,000\n"));
    }

    #[test]
    fn builder_build_without_printing() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .color(true)
            .build_without_printing()
            .unwrap();
        tw.ok("first").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(!out.contains("CRAP version"));
        assert!(out.contains("\x1b[32mok\x1b[0m 1 - first\n"));
    }

    #[test]
    fn builder_build_without_printing_with_locale() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .color(true)
            .locale(locale)
            .build_without_printing()
            .unwrap();
        tw.plan_ahead(10000).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(!out.contains("CRAP version"));
        assert!(!out.contains("pragma"));
        assert!(out.contains("1::10,000\n"));
    }

    #[test]
    fn builder_no_locale_clears() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .locale(locale)
            .no_locale()
            .build()
            .unwrap();
        tw.plan_ahead(10000).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(!out.contains("pragma"));
        assert!(out.contains("1::10000\n"));
    }

    #[test]
    fn builder_auto_no_color_when_set() {
        let _lock = ENV_MUTEX.lock().unwrap();
        let original = std::env::var("NO_COLOR").ok();
        std::env::set_var("NO_COLOR", "1");

        let mut buf = Vec::new();
        let tw = CrapWriterBuilder::auto(&mut buf).build().unwrap();
        assert!(!tw.config.color());

        match original {
            Some(v) => std::env::set_var("NO_COLOR", v),
            None => std::env::remove_var("NO_COLOR"),
        }
    }

    #[test]
    fn builder_auto_color_when_no_color_absent() {
        let _lock = ENV_MUTEX.lock().unwrap();
        let original = std::env::var("NO_COLOR").ok();
        std::env::remove_var("NO_COLOR");

        let mut buf = Vec::new();
        let tw = CrapWriterBuilder::auto(&mut buf).build().unwrap();
        assert!(tw.config.color());

        if let Some(v) = original {
            std::env::set_var("NO_COLOR", v);
        }
    }

    #[test]
    fn builder_auto_override_color() {
        let _lock = ENV_MUTEX.lock().unwrap();
        let original = std::env::var("NO_COLOR").ok();
        std::env::remove_var("NO_COLOR");

        let mut buf = Vec::new();
        let tw = CrapWriterBuilder::auto(&mut buf)
            .color(false)
            .build()
            .unwrap();
        assert!(!tw.config.color());

        if let Some(v) = original {
            std::env::set_var("NO_COLOR", v);
        }
    }

    #[test]
    fn builder_default_locale_ignores_c_locale() {
        let _lock = ENV_MUTEX.lock().unwrap();
        let orig_all = std::env::var("LC_ALL").ok();
        let orig_num = std::env::var("LC_NUMERIC").ok();
        let orig_lang = std::env::var("LANG").ok();
        std::env::set_var("LANG", "C");
        std::env::remove_var("LC_ALL");
        std::env::remove_var("LC_NUMERIC");

        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .default_locale()
            .build()
            .unwrap();
        tw.plan_ahead(10000).unwrap();
        let out = String::from_utf8(buf).unwrap();
        // C locale should not parse as ICU locale, so no formatting
        assert!(!out.contains("pragma"));
        assert!(out.contains("1::10000\n"));

        // Restore
        match orig_all {
            Some(v) => std::env::set_var("LC_ALL", v),
            None => std::env::remove_var("LC_ALL"),
        }
        match orig_num {
            Some(v) => std::env::set_var("LC_NUMERIC", v),
            None => std::env::remove_var("LC_NUMERIC"),
        }
        match orig_lang {
            Some(v) => std::env::set_var("LANG", v),
            None => std::env::remove_var("LANG"),
        }
    }

    #[test]
    fn builder_default_locale_normalizes_posix_underscores() {
        let _lock = ENV_MUTEX.lock().unwrap();
        let orig_all = std::env::var("LC_ALL").ok();
        let orig_num = std::env::var("LC_NUMERIC").ok();
        let orig_lang = std::env::var("LANG").ok();
        std::env::set_var("LANG", "en_US.UTF-8");
        std::env::remove_var("LC_ALL");
        std::env::remove_var("LC_NUMERIC");

        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .default_locale()
            .build()
            .unwrap();
        tw.plan_ahead(10000).unwrap();
        let out = String::from_utf8(buf).unwrap();
        // en_US should parse after underscore-to-hyphen normalization,
        // producing formatted output with grouping separators
        assert!(out.contains("pragma"));
        assert!(out.contains("1::10,000\n"));

        // Restore
        match orig_all {
            Some(v) => std::env::set_var("LC_ALL", v),
            None => std::env::remove_var("LC_ALL"),
        }
        match orig_num {
            Some(v) => std::env::set_var("LC_NUMERIC", v),
            None => std::env::remove_var("LC_NUMERIC"),
        }
        match orig_lang {
            Some(v) => std::env::set_var("LANG", v),
            None => std::env::remove_var("LANG"),
        }
    }

    // --- Color + locale combined tests ---

    #[test]
    fn writer_color_and_locale_combined() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .color(true)
            .locale(locale)
            .build()
            .unwrap();
        for _ in 0..1234 {
            tw.ok("test").unwrap();
        }
        tw.plan().unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.starts_with("CRAP-2\n"));
        assert!(out.contains("pragma +locale-formatting:en-US\n"));
        assert!(out.contains("\x1b[32mok\x1b[0m 1,234 - test\n"));
        assert!(out.contains("1::1,234\n"));
    }

    #[test]
    fn writer_color_and_locale_subtest_inheritance() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .color(true)
            .locale(locale)
            .build()
            .unwrap();
        tw.subtest("nested", |sub| {
            sub.plan_ahead(10000)?;
            sub.ok("inner")?;
            sub.plan()
        })
        .unwrap();
        tw.ok("nested").unwrap();
        tw.plan().unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.contains("    pragma +locale-formatting:en-US\n"),
            "expected subtest locale pragma, got:\n{out}"
        );
        assert!(
            out.contains("    \x1b[32mok\x1b[0m 1 - inner\n"),
            "expected subtest color, got:\n{out}"
        );
        assert!(
            out.contains("    1::10,000\n"),
            "expected subtest locale plan, got:\n{out}"
        );
        assert!(out.contains("\x1b[32mok\x1b[0m 1 - nested\n"));
    }

    #[test]
    fn writer_test_point_formats_number_with_locale() {
        let mut buf = Vec::new();
        let locale: Locale = "en-US".parse().unwrap();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .locale(locale)
            .build()
            .unwrap();
        let result = TestResult {
            number: 1234,
            name: "big number".into(),
            ok: true,
            directive: None,
            error_message: None,
            exit_code: None,
            output: None,
            suppress_yaml: false,
        };
        tw.test_point(&result).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.contains("ok 1,234 - big number\n"),
            "expected locale-formatted number, got:\n{out}"
        );
    }

    #[test]
    fn writer_status_line_enabled_by_default() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .status_line(true)
            .build()
            .unwrap();
        tw.ok("test").unwrap();
        let out = String::from_utf8(buf).unwrap();
        // Status-line is enabled by default in CRAP-2; no pragma +status-line emitted
        assert!(
            !out.contains("pragma +status-line"),
            "should not emit pragma +status-line (enabled by default), got:\n{out}"
        );
    }

    #[test]
    fn writer_status_line_not_emitted_by_default() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf).build().unwrap();
        tw.ok("test").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            !out.contains("status-line"),
            "should not emit status-line by default, got:\n{out}"
        );
    }

    #[test]
    fn writer_update_last_line() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .status_line(true)
            .build()
            .unwrap();
        tw.update_last_line("building... 1/3").unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.contains("\r\x1b[2K# building... 1/3\n"),
            "expected cursor control + comment prefix + trailing newline, got:\n{out}"
        );
    }

    #[test]
    fn writer_finish_last_line_erases() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .status_line(true)
            .build()
            .unwrap();
        tw.update_last_line("building...").unwrap();
        tw.finish_last_line().unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(
            out.ends_with("\x1b[A\r\x1b[2K"),
            "finish_last_line should move up + erase, got:\n{out}"
        );
    }

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
            out.contains("\r\x1b[2K\x1b[?7l# long line here\x1b[?7h\n"),
            "expected DECAWM wrapping with trailing newline, got:\n{out}"
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

    #[test]
    fn writer_subtest_does_not_inherit_status_line() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .status_line(true)
            .build()
            .unwrap();
        tw.subtest("child", |child| {
            child.ok("inner").unwrap();
            child.plan().unwrap();
            Ok(())
        })
        .unwrap();
        let out = String::from_utf8(buf).unwrap();
        let indented_pragma = "    pragma +status-line";
        assert!(
            !out.contains(indented_pragma),
            "subtest should not inherit status-line, got:\n{out}"
        );
    }

    // --- Auto-clear before test points ---

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
        assert!(
            !out.contains("\r\x1b[2K"),
            "should not emit clear when no status line active, got:\n{out}"
        );
    }

    #[test]
    fn ok_auto_clears_status_line() {
        let mut buf = Vec::new();
        let mut tw = CrapWriterBuilder::new(&mut buf)
            .status_line(true)
            .build()
            .unwrap();
        tw.update_last_line("compiling...").unwrap();
        tw.ok("compile").unwrap();
        let out = String::from_utf8(buf).unwrap();
        let ok_pos = out.rfind("ok 1 - compile").unwrap();
        let clear_before = &out[..ok_pos];
        assert!(
            clear_before.ends_with("\r\x1b[2K"),
            "ok should auto-clear status line, got:\n{out}"
        );
    }

    // --- StatusLineProcessor ---

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

    // --- feed_status_bytes ---

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

    #[test]
    fn spinner_advances_on_prefix() {
        let mut s = Spinner::new();
        let f1 = s.prefix();
        // Rate-limited, so immediate second call returns same frame
        let f2 = s.prefix();
        assert_eq!(f1, f2);
    }

    #[test]
    fn spinner_current_prefix_does_not_advance() {
        let mut s = Spinner::new();
        let f1 = s.prefix();
        let f2 = s.current_prefix();
        assert_eq!(f1, f2);
    }

    #[test]
    fn spinner_disabled_returns_empty() {
        let mut s = Spinner::disabled();
        assert_eq!(s.prefix(), "");
        assert_eq!(s.current_prefix(), "");
        assert_eq!(s.formatted_prefix(), "");
        assert_eq!(s.formatted_current_prefix(), "");
    }

    #[test]
    fn spinner_not_sleeping_initially() {
        let s = Spinner::new();
        assert!(!s.is_sleeping());
    }

    #[test]
    fn spinner_not_sleeping_after_touch() {
        let mut s = Spinner::new();
        s.touch();
        assert!(!s.is_sleeping());
    }

    #[test]
    fn spinner_sleeping_detection() {
        let mut s = Spinner::new();
        // Simulate an old touch by setting last_content to the past
        s.last_content = Some(Instant::now() - Duration::from_secs(10));
        assert!(s.is_sleeping());
    }

    #[test]
    fn spinner_formatted_prefix_includes_zzz_when_sleeping() {
        let mut s = Spinner::new();
        s.last_content = Some(Instant::now() - Duration::from_secs(10));
        let p = s.formatted_prefix();
        assert!(p.contains("💤"), "expected 💤 in prefix, got: {p}");
    }

    #[test]
    fn spinner_formatted_prefix_no_zzz_when_active() {
        let mut s = Spinner::new();
        s.touch();
        let p = s.formatted_prefix();
        assert!(!p.contains("💤"), "unexpected 💤 in prefix: {p}");
        assert!(p.ends_with(' '), "prefix should end with space: {p:?}");
    }
}
