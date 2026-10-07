package collectiondesign

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed guidance.md
var guidanceMarkdown string

// Requirements describes the design task. The caller names its catalog source;
// the MCP prompt names its inline catalog, while the REST agent can name its
// read-only catalog tools without loading every benchmark into the prompt.
func Requirements(catalogDescription string) string {
	return strings.ReplaceAll(guidanceSection("requirements"), "{catalog_source}", catalogDescription)
}

// CalibrationGuidelines is shared text, not a rule for clamping every metric.
// RHAI-3804 will add deterministic, metric-aware validation and calibration.
func CalibrationGuidelines(strictness string) string {
	return strings.ReplaceAll(guidanceSection("calibration"), "{strictness}", strictness)
}

func DomainSignalMapping() string {
	return guidanceSection("domain_signal_mapping")
}

func OutputFormat() string {
	return guidanceSection("output_format")
}

// NeutralGuidance is suitable for an in-service agent: it contains neither
// MCP tool names nor an instruction to persist or embed the full catalog.
func NeutralGuidance(options Options) string {
	return strings.Join([]string{
		Requirements("the live provider catalog"),
		CalibrationGuidelines(options.Strictness),
		DomainSignalMapping(),
		OutputFormat(),
	}, "\n\n")
}

func guidanceSection(name string) string {
	return extractGuidanceSection(guidanceMarkdown, name)
}

func extractGuidanceSection(markdown, name string) string {
	startMarker := "<!-- BEGIN " + name + " -->\n"
	endMarker := "\n<!-- END " + name + " -->"
	start := strings.Index(markdown, startMarker)
	if start < 0 {
		panic(fmt.Sprintf("collection design guidance is missing section %q", name))
	}
	start += len(startMarker)
	end := strings.Index(markdown[start:], endMarker)
	if end < 0 {
		panic(fmt.Sprintf("collection design guidance section %q is not terminated", name))
	}
	return markdown[start : start+end]
}
