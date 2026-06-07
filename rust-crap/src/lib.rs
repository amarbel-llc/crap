//! rust-crap: an ndjson-crap writer.
//!
//! ndjson-crap is CRAP-2's canonical wire format: newline-delimited JSON, one
//! record per line. This crate writes the result family (plan / test /
//! bailout / summary), which is field-compatible with tap-dancer's
//! `tap-ndjson(7)`. Presentation (color, spinner, layout) is the viewport's
//! job (the `crap-present` binary), not this library's.
//!
//! ```
//! use rust_crap::NdjsonCrapWriter;
//! let mut buf = Vec::new();
//! {
//!     let mut w = NdjsonCrapWriter::new(&mut buf);
//!     w.header("my suite", "example").unwrap();
//!     w.plan_ahead(2).unwrap();
//!     w.ok("first").unwrap();
//!     w.not_ok_diag("second", &[("message", "boom")]).unwrap();
//!     w.finish().unwrap();
//! }
//! assert!(String::from_utf8(buf).unwrap().contains("\"type\":\"summary\""));
//! ```

use serde::Serialize;
use serde_json::{Map, Value};
use std::io::{self, Write};

/// CRAP major version emitted in the header record.
pub const CRAP_VERSION: u32 = 2;
/// ndjson-crap schema version emitted in the header record.
pub const NDJSON_VERSION: u32 = 1;

#[derive(Serialize)]
struct Meta<'a> {
    #[serde(rename = "type")]
    ty: &'static str,
    version: u32,
    ndjson: u32,
    #[serde(skip_serializing_if = "str::is_empty")]
    title: &'a str,
    #[serde(skip_serializing_if = "str::is_empty")]
    source: &'a str,
}

#[derive(Serialize)]
struct Plan {
    #[serde(rename = "type")]
    ty: &'static str,
    count: usize,
}

#[derive(Serialize)]
struct Directive<'a> {
    kind: &'static str,
    reason: &'a str,
}

#[derive(Serialize)]
struct Test<'a> {
    #[serde(rename = "type")]
    ty: &'static str,
    n: usize,
    description: &'a str,
    ok: bool,
    directive: Option<Directive<'a>>,
    diagnostic: Option<Map<String, Value>>,
    output: Option<String>,
    subtest: Option<Vec<Value>>,
    line: usize,
}

#[derive(Serialize)]
struct Bailout<'a> {
    #[serde(rename = "type")]
    ty: &'static str,
    message: &'a str,
    line: usize,
}

#[derive(Serialize)]
struct Summary {
    #[serde(rename = "type")]
    ty: &'static str,
    passed: usize,
    failed: usize,
    skipped: usize,
    todo: usize,
    total: usize,
    plan_count: usize,
    bailed: bool,
    valid: bool,
    diagnostics: Vec<Value>,
}

/// A direct producer of result-family ndjson-crap.
pub struct NdjsonCrapWriter<'a> {
    w: &'a mut dyn Write,
    n: usize,
    passed: usize,
    failed: usize,
    skipped: usize,
    todo: usize,
    plan_count: usize,
    bailed: bool,
}

impl<'a> NdjsonCrapWriter<'a> {
    /// Create a writer over `w`.
    pub fn new(w: &'a mut dyn Write) -> Self {
        Self {
            w,
            n: 0,
            passed: 0,
            failed: 0,
            skipped: 0,
            todo: 0,
            plan_count: 0,
            bailed: false,
        }
    }

    fn write_line<T: Serialize>(&mut self, rec: &T) -> io::Result<()> {
        let s = serde_json::to_string(rec).map_err(io::Error::other)?;
        self.w.write_all(s.as_bytes())?;
        self.w.write_all(b"\n")
    }

    fn diag_map(diagnostics: &[(&str, &str)]) -> Option<Map<String, Value>> {
        if diagnostics.is_empty() {
            return None;
        }
        let mut m = Map::new();
        for (k, v) in diagnostics {
            m.insert((*k).to_string(), Value::String((*v).to_string()));
        }
        Some(m)
    }

    /// Emit the optional `crap` header with the current schema versions.
    pub fn header(&mut self, title: &str, source: &str) -> io::Result<()> {
        self.write_line(&Meta {
            ty: "crap",
            version: CRAP_VERSION,
            ndjson: NDJSON_VERSION,
            title,
            source,
        })
    }

    /// Emit a leading plan record. Records `count` for the summary's plan_count.
    pub fn plan_ahead(&mut self, count: usize) -> io::Result<()> {
        self.plan_count = count;
        self.write_line(&Plan { ty: "plan", count })
    }

    fn test(
        &mut self,
        description: &str,
        ok: bool,
        directive: Option<Directive>,
        diagnostic: Option<Map<String, Value>>,
    ) -> io::Result<usize> {
        self.n += 1;
        let n = self.n;
        self.write_line(&Test {
            ty: "test",
            n,
            description,
            ok,
            directive,
            diagnostic,
            output: None,
            subtest: None,
            line: 0,
        })?;
        Ok(n)
    }

    /// Emit a passing test.
    pub fn ok(&mut self, description: &str) -> io::Result<usize> {
        self.passed += 1;
        self.test(description, true, None, None)
    }

    /// Emit a failing test.
    pub fn not_ok(&mut self, description: &str) -> io::Result<usize> {
        self.failed += 1;
        self.test(description, false, None, None)
    }

    /// Emit a passing test with a diagnostic.
    pub fn ok_diag(
        &mut self,
        description: &str,
        diagnostics: &[(&str, &str)],
    ) -> io::Result<usize> {
        self.passed += 1;
        self.test(description, true, None, Self::diag_map(diagnostics))
    }

    /// Emit a failing test with a diagnostic.
    pub fn not_ok_diag(
        &mut self,
        description: &str,
        diagnostics: &[(&str, &str)],
    ) -> io::Result<usize> {
        self.failed += 1;
        self.test(description, false, None, Self::diag_map(diagnostics))
    }

    /// Emit a skipped test (ok=true, skip directive).
    pub fn skip(&mut self, description: &str, reason: &str) -> io::Result<usize> {
        self.skipped += 1;
        self.test(
            description,
            true,
            Some(Directive {
                kind: "skip",
                reason,
            }),
            None,
        )
    }

    /// Emit a todo test (ok=false, todo directive).
    pub fn todo(&mut self, description: &str, reason: &str) -> io::Result<usize> {
        self.todo += 1;
        self.test(
            description,
            false,
            Some(Directive {
                kind: "todo",
                reason,
            }),
            None,
        )
    }

    /// Emit a bailout record. Marks the stream bailed for the summary.
    pub fn bail_out(&mut self, message: &str) -> io::Result<()> {
        self.bailed = true;
        self.write_line(&Bailout {
            ty: "bailout",
            message,
            line: 0,
        })
    }

    /// Emit the terminal summary record. Call exactly once, last.
    pub fn finish(&mut self) -> io::Result<()> {
        let total = self.passed + self.failed + self.skipped + self.todo;
        self.write_line(&Summary {
            ty: "summary",
            passed: self.passed,
            failed: self.failed,
            skipped: self.skipped,
            todo: self.todo,
            total,
            plan_count: self.plan_count,
            bailed: self.bailed,
            valid: true,
            diagnostics: Vec::new(),
        })
    }

    /// Number of test records emitted so far.
    pub fn count(&self) -> usize {
        self.n
    }

    /// Whether any test failed.
    pub fn has_failures(&self) -> bool {
        self.failed > 0
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn render(f: impl FnOnce(&mut NdjsonCrapWriter)) -> String {
        let mut buf = Vec::new();
        {
            let mut w = NdjsonCrapWriter::new(&mut buf);
            f(&mut w);
        }
        String::from_utf8(buf).unwrap()
    }

    #[test]
    fn header_has_versions() {
        let out = render(|w| {
            w.header("suite", "src").unwrap();
        });
        assert_eq!(
            out.trim(),
            r#"{"type":"crap","version":2,"ndjson":1,"title":"suite","source":"src"}"#
        );
    }

    #[test]
    fn passing_test_emits_all_fields() {
        let out = render(|w| {
            w.ok("loads config").unwrap();
        });
        assert_eq!(
            out.trim(),
            r#"{"type":"test","n":1,"description":"loads config","ok":true,"directive":null,"diagnostic":null,"output":null,"subtest":null,"line":0}"#
        );
    }

    #[test]
    fn failing_test_with_diagnostic() {
        let out = render(|w| {
            w.not_ok_diag("parses", &[("message", "boom")]).unwrap();
        });
        assert!(out.contains(r#""ok":false"#));
        assert!(out.contains(r#""diagnostic":{"message":"boom"}"#));
    }

    #[test]
    fn skip_and_todo_directives() {
        let out = render(|w| {
            w.skip("net", "offline").unwrap();
            w.todo("later", "not yet").unwrap();
        });
        assert!(out.contains(r#""directive":{"kind":"skip","reason":"offline"}"#));
        assert!(out.contains(r#""directive":{"kind":"todo","reason":"not yet"}"#));
    }

    #[test]
    fn summary_counts_and_array_diagnostics() {
        let out = render(|w| {
            w.plan_ahead(3).unwrap();
            w.ok("a").unwrap();
            w.not_ok("b").unwrap();
            w.skip("c", "why").unwrap();
            w.finish().unwrap();
        });
        let summary = out.lines().last().unwrap();
        assert!(summary.contains(r#""passed":1"#));
        assert!(summary.contains(r#""failed":1"#));
        assert!(summary.contains(r#""skipped":1"#));
        assert!(summary.contains(r#""total":3"#));
        assert!(summary.contains(r#""plan_count":3"#));
        assert!(summary.contains(r#""diagnostics":[]"#));
    }

    #[test]
    fn bailout_marks_summary() {
        let out = render(|w| {
            w.ok("a").unwrap();
            w.bail_out("db unreachable").unwrap();
            w.finish().unwrap();
        });
        assert!(out.contains(r#"{"type":"bailout","message":"db unreachable","line":0}"#));
        assert!(out.lines().last().unwrap().contains(r#""bailed":true"#));
    }

    #[test]
    fn counters() {
        let mut buf = Vec::new();
        let mut w = NdjsonCrapWriter::new(&mut buf);
        w.ok("a").unwrap();
        w.not_ok("b").unwrap();
        assert_eq!(w.count(), 2);
        assert!(w.has_failures());
    }
}
