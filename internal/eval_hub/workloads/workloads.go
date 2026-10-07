// Package workloads registers server-managed workloads for runtime provider resolution.
package workloads

import (
	"fmt"
	"sync"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// Type identifies how a benchmark's execution provider is resolved.
type Type string

const BenchmarkEvaluation Type = "benchmark-evaluation"

// Definition describes a workload that supplies its own runtime provider.
type Definition struct {
	Type            Type
	ProviderID      string
	BenchmarkID     string
	Internal        bool
	MatchesJob      func(*api.EvaluationJobConfig) bool
	RuntimeProvider func() *api.ProviderResource
}

var registry = struct {
	sync.RWMutex
	definitions []Definition
}{}

// Register adds a workload during package initialization. Duplicate IDs or
// types are programming errors, so registration fails immediately.
func Register(definition Definition) {
	if definition.Type == "" || definition.Type == BenchmarkEvaluation || definition.ProviderID == "" || definition.BenchmarkID == "" ||
		definition.MatchesJob == nil || definition.RuntimeProvider == nil {
		panic("incomplete workload registration")
	}
	registry.Lock()
	defer registry.Unlock()
	for _, registered := range registry.definitions {
		if registered.Type == definition.Type ||
			(registered.ProviderID == definition.ProviderID && registered.BenchmarkID == definition.BenchmarkID) {
			panic(fmt.Sprintf("duplicate workload registration: %s", definition.Type))
		}
	}
	registry.definitions = append(registry.definitions, definition)
}

// ForJob returns the registered workload that owns a stored job.
func ForJob(job *api.EvaluationJobConfig) (Definition, bool) {
	registry.RLock()
	definitions := append([]Definition(nil), registry.definitions...)
	registry.RUnlock()
	for _, definition := range definitions {
		if definition.MatchesJob(job) {
			return definition, true
		}
	}
	return Definition{}, false
}

// ByType returns a registered workload by type.
func ByType(workloadType Type) (Definition, bool) {
	registry.RLock()
	defer registry.RUnlock()
	for _, definition := range registry.definitions {
		if definition.Type == workloadType {
			return definition, true
		}
	}
	return Definition{}, false
}

// IsInternalProviderID reports whether an internal workload registered an ID.
func IsInternalProviderID(id string) bool {
	registry.RLock()
	defer registry.RUnlock()
	for _, definition := range registry.definitions {
		if definition.Internal && definition.ProviderID == id {
			return true
		}
	}
	return false
}
