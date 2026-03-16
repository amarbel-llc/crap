package crap

import (
	"context"
	"fmt"
	"io"
	"strings"
)

func FindBrew(selfExe string) (string, error) {
	return findBinary(selfExe, "brew")
}

func brewParserForSubcommand(args []string) *PhaseParser {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "install":
		return NewBrewInstallParser()
	case "upgrade":
		return NewBrewUpgradeParser()
	case "update":
		return NewBrewUpdateParser()
	case "tap":
		return NewBrewTapParser()
	default:
		return nil
	}
}

func ConvertBrew(ctx context.Context, selfExe string, args []string, w io.Writer, stdin io.Reader, stderrW io.Writer, verbose bool, color bool) int {
	brewPath, err := FindBrew(selfExe)
	if err != nil {
		fmt.Fprintf(stderrW, "::brew: %s\n", err)
		return 1
	}

	parser := brewParserForSubcommand(args)
	if parser == nil {
		return execPassthrough(ctx, brewPath, args, w, stdin, stderrW)
	}

	return convertWithPhases(ctx, brewPath, args, w, parser, color, "brew")
}

func NewBrewInstallParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"download", "install", "link", "caveats"},
		classify:   classifyBrewInstallLine,
		phases:     make(map[string]*Phase),
	}
}

func NewBrewUpgradeParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"download", "install", "link", "caveats"},
		classify:   classifyBrewInstallLine,
		phases:     make(map[string]*Phase),
	}
}

func NewBrewUpdateParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"fetch", "update"},
		classify:   classifyBrewUpdateLine,
		phases:     make(map[string]*Phase),
	}
}

func NewBrewTapParser() *PhaseParser {
	return &PhaseParser{
		phaseOrder: []string{"clone", "install"},
		classify:   classifyBrewTapLine,
		phases:     make(map[string]*Phase),
	}
}

func classifyBrewInstallLine(line string) string {
	trimmed := strings.TrimSpace(line)

	if strings.HasPrefix(trimmed, "==> Downloading") ||
		strings.HasPrefix(trimmed, "==> Fetching") ||
		strings.HasPrefix(trimmed, "Already downloaded:") {
		return "download"
	}

	if strings.HasPrefix(trimmed, "==> Installing") ||
		strings.HasPrefix(trimmed, "==> Pouring") {
		return "install"
	}

	if strings.HasPrefix(trimmed, "==> Linking") {
		return "link"
	}

	if strings.HasPrefix(trimmed, "==> Caveats") {
		return "caveats"
	}

	return ""
}

func classifyBrewUpdateLine(line string) string {
	trimmed := strings.TrimSpace(line)

	if strings.HasPrefix(trimmed, "==> Fetching") ||
		strings.HasPrefix(trimmed, "remote: ") ||
		strings.HasPrefix(trimmed, "Receiving objects:") ||
		strings.HasPrefix(trimmed, "Resolving deltas:") {
		return "fetch"
	}

	if strings.HasPrefix(trimmed, "==> Updated") ||
		strings.HasPrefix(trimmed, "==> New") ||
		strings.HasPrefix(trimmed, "==> Deleted") ||
		strings.HasPrefix(trimmed, "==> Renamed") {
		return "update"
	}

	return ""
}

func classifyBrewTapLine(line string) string {
	trimmed := strings.TrimSpace(line)

	if strings.HasPrefix(trimmed, "==> Tapping") ||
		strings.HasPrefix(trimmed, "Cloning into") ||
		strings.HasPrefix(trimmed, "remote: ") ||
		strings.HasPrefix(trimmed, "Receiving objects:") ||
		strings.HasPrefix(trimmed, "Resolving deltas:") {
		return "clone"
	}

	if strings.HasPrefix(trimmed, "==> Tapped") {
		return "install"
	}

	return ""
}
