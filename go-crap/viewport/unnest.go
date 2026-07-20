package viewport

import (
	"fmt"
	"strings"

	"code.linenisgreat.com/crap/go-crap/v2/ndjsoncrap"
)

// maxUnnestDepth bounds recursive unwrapping so a pathological (or cyclic)
// crap-within-crap stream cannot recurse without limit. A `:: exec` cascade
// nests one or two levels in practice; 8 is generous headroom.
const maxUnnestDepth = 8

// unnestOutput expands an output record's (already base64-decoded) text into
// display lines, recursively unwrapping any nested ndjson-crap records so a
// crap-within-crap stream — a `:: exec` node whose child itself emits
// ndjson-crap — renders as legible text instead of escaped JSON soup
// (crap#34). Each line that decodes to a recognized crap record is replaced
// by that record's human rendering (a nested output recurses into its own
// text; a command shows "$ cmd"; a node/test/item shows its name, verdict, or
// label); a line that is not a recognizable crap record is kept verbatim.
func unnestOutput(text string) []string { return unnestLines(text, 0) }

// unnestLines splits text into lines and unwraps each. depth tracks the
// nesting level for the recursion guard.
func unnestLines(text string, depth int) []string {
	var out []string
	for _, line := range splitLines(text) {
		out = append(out, unnestLine(line, depth)...)
	}
	return out
}

// unnestLine renders one line: if it decodes to a recognized crap record it
// is expanded (recursively, for nested output); otherwise it is kept
// verbatim. A line that is not a JSON object, does not decode, or decodes to
// an Unknown type (a JSON line the child tool merely printed) is verbatim —
// only records this build actually understands are unwrapped, bounding false
// positives.
func unnestLine(line string, depth int) []string {
	trimmed := strings.TrimSpace(line)
	if depth >= maxUnnestDepth || !strings.HasPrefix(trimmed, "{") {
		return []string{line}
	}
	rec, err := ndjsoncrap.Decode([]byte(trimmed))
	if err != nil {
		return []string{line}
	}
	if _, unknown := rec.(ndjsoncrap.Unknown); unknown {
		return []string{line}
	}
	return recordLines(rec, depth)
}

// recordLines renders a decoded nested record as display lines, using the
// same ✓/✗/↷/$ conventions the top-level presenter uses. Purely structural
// records (node_end, summary, plan, meta, operation_start/end) add nothing
// beyond the outer node's own verdict, so they render nothing.
func recordLines(rec ndjsoncrap.Record, depth int) []string {
	switch r := rec.(type) {
	case ndjsoncrap.Output:
		return unnestLines(decodeOutput(r), depth+1)
	case ndjsoncrap.Command:
		return []string{"$ " + r.Command}
	case ndjsoncrap.NodeStart:
		if name := nodeStartName(r); name != "" {
			return []string{name}
		}
		return nil
	case ndjsoncrap.Test:
		return nestedTestLines(r, depth)
	case ndjsoncrap.Bailout:
		return []string{"✗ Bail out! " + r.Message}
	case ndjsoncrap.Item:
		return []string{nestedItemLine(r)}
	case ndjsoncrap.Progress:
		if r.Label != "" {
			return []string{r.Label}
		}
		return nil
	default:
		return nil
	}
}

// nestedTestLines renders a nested result-family test: a one-line verdict,
// plus (on a non-directive failure) the test's own captured output, itself
// unwrapped so a doubly-nested failure still surfaces its text.
func nestedTestLines(t ndjsoncrap.Test, depth int) []string {
	var lines []string
	switch {
	case t.Directive != nil:
		lines = append(lines, fmt.Sprintf("↷ %s # %s %s",
			t.Description, strings.ToUpper(t.Directive.Kind), t.Directive.Reason))
	case t.OK:
		lines = append(lines, "✓ "+t.Description)
	default:
		lines = append(lines, "✗ "+t.Description)
		if t.Output != nil {
			lines = append(lines, unnestLines(*t.Output, depth+1)...)
		}
	}
	return lines
}

// nestedItemLine renders one nested operation item by its state.
func nestedItemLine(it ndjsoncrap.Item) string {
	switch it.State {
	case ndjsoncrap.ItemFailed:
		return "✗ " + it.Label
	case ndjsoncrap.ItemSkipped:
		return "↷ " + it.Label
	default:
		return it.Label
	}
}

// nodeStartName resolves a node_start's display name (name, else namepath).
func nodeStartName(n ndjsoncrap.NodeStart) string {
	if n.Name != "" {
		return n.Name
	}
	return n.Namepath
}
