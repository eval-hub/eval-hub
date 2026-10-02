package collectiondesign

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	DefaultMaxBenchmarks = 12

	StrictnessLenient  = "lenient"
	StrictnessModerate = "moderate"
	StrictnessStrict   = "strict"

	DefaultStrictness = StrictnessModerate
)

var validStrictness = [...]string{StrictnessLenient, StrictnessModerate, StrictnessStrict}

// Options describes a collection-design request after input normalization.
// ProviderFilter retains the caller's trimmed text for the existing MCP summary;
// ProviderIDs contains its nonempty, deduplicated IDs for membership checks.
type Options struct {
	Goal           string
	ProviderFilter string
	ProviderIDs    map[string]struct{}
	MaxBenchmarks  int
	Strictness     string
}

func ValidStrictness() []string {
	return append([]string(nil), validStrictness[:]...)
}

// ParseOptions accepts the MCP prompt's string arguments and returns typed options.
// A nonempty filter containing only commas still matches no providers, as before.
func ParseOptions(goal, providerFilter, maxBenchmarksRaw, strictness string) (Options, error) {
	goal = strings.TrimSpace(goal)
	providerFilter = strings.TrimSpace(providerFilter)
	maxBenchmarksRaw = strings.TrimSpace(maxBenchmarksRaw)
	strictness = strings.TrimSpace(strictness)

	if goal == "" {
		return Options{}, fmt.Errorf("evaluation_goal is required")
	}

	maxBenchmarks := DefaultMaxBenchmarks
	if maxBenchmarksRaw != "" {
		n, err := strconv.Atoi(maxBenchmarksRaw)
		if err != nil || n <= 0 {
			return Options{}, fmt.Errorf("invalid max_benchmarks %q; must be a positive integer", maxBenchmarksRaw)
		}
		maxBenchmarks = n
	}

	if strictness == "" {
		strictness = DefaultStrictness
	} else if !isValidStrictness(strictness) {
		return Options{}, fmt.Errorf("invalid strictness %q; valid values: %s", strictness, strings.Join(validStrictness[:], ", "))
	}

	var providerIDs map[string]struct{}
	if providerFilter != "" {
		providerIDs = make(map[string]struct{})
		for id := range strings.SplitSeq(providerFilter, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			providerIDs[id] = struct{}{}
		}
	}

	return Options{
		Goal:           goal,
		ProviderFilter: providerFilter,
		ProviderIDs:    providerIDs,
		MaxBenchmarks:  maxBenchmarks,
		Strictness:     strictness,
	}, nil
}

func isValidStrictness(value string) bool {
	for _, valid := range validStrictness {
		if value == valid {
			return true
		}
	}
	return false
}

func (o Options) allowsProvider(id string) bool {
	if o.ProviderFilter == "" {
		return true
	}
	_, allowed := o.ProviderIDs[id]
	return allowed
}

// Summary preserves the option text in the existing MCP prompt and tool.
func (o Options) Summary() string {
	parts := make([]string, 0, 3)
	if o.ProviderFilter != "" {
		parts = append(parts, "Provider filter: "+o.ProviderFilter)
	}
	parts = append(parts, "Max benchmarks: "+strconv.Itoa(o.MaxBenchmarks))
	parts = append(parts, "Strictness: "+o.Strictness)
	return strings.Join(parts, " | ")
}
