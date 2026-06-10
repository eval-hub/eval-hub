package k8s

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	hardwareProfileAPIGroup   = "infrastructure.opendatahub.io"
	hardwareProfileAPIVersion = "v1"
	hardwareProfileResource   = "hardwareprofiles"
)

var standardHardwareProfileResources = map[string]struct{}{
	"cpu":               {},
	"memory":            {},
	"ephemeral-storage": {},
}

// hardwareProfileResources holds resource values extracted from a HardwareProfile CR.
// Empty strings and zero counts mean the field was not set in the profile.
type hardwareProfileResources struct {
	cpuRequest    string
	cpuLimit      string
	memoryRequest string
	memoryLimit   string
	gpuResource   string
	gpuCount      int
}

func parseHardwareProfileResources(profile *unstructured.Unstructured) (*hardwareProfileResources, error) {
	if profile == nil {
		return nil, fmt.Errorf("hardware profile is required")
	}
	identifiers, found, err := unstructured.NestedSlice(profile.Object, "spec", "identifiers")
	if err != nil {
		return nil, fmt.Errorf("read hardware profile identifiers: %w", err)
	}
	if !found || len(identifiers) == 0 {
		return &hardwareProfileResources{}, nil
	}

	out := &hardwareProfileResources{}
	for _, raw := range identifiers {
		identifierMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		identifier := strings.TrimSpace(stringFromUnstructured(identifierMap["identifier"]))
		resourceType := strings.TrimSpace(stringFromUnstructured(identifierMap["resourceType"]))
		defaultCount, hasDefault := quantityStringFromUnstructured(identifierMap["defaultCount"])
		maxCount, hasMax := quantityStringFromUnstructured(identifierMap["maxCount"])

		switch {
		case resourceType == "CPU" || identifier == "cpu":
			if hasDefault {
				out.cpuRequest = defaultCount
			}
			if hasMax {
				out.cpuLimit = maxCount
			}
		case resourceType == "Memory" || identifier == "memory":
			if hasDefault {
				out.memoryRequest = defaultCount
			}
			if hasMax {
				out.memoryLimit = maxCount
			}
		case resourceType == "Accelerator" || (identifier != "" && !isStandardHardwareProfileResource(identifier)):
			if identifier == "" {
				continue
			}
			out.gpuResource = identifier
			if hasDefault {
				count, err := strconv.Atoi(defaultCount)
				if err != nil {
					return nil, fmt.Errorf("parse accelerator count for %q: %w", identifier, err)
				}
				out.gpuCount = count
			}
		}
	}
	return out, nil
}

func applyHardwareProfileResources(cfg *jobConfig, profile *hardwareProfileResources) {
	if cfg == nil || profile == nil {
		return
	}
	if profile.cpuRequest != "" {
		cfg.cpuRequest = profile.cpuRequest
	}
	if profile.cpuLimit != "" {
		cfg.cpuLimit = profile.cpuLimit
	}
	if profile.memoryRequest != "" {
		cfg.memoryRequest = profile.memoryRequest
	}
	if profile.memoryLimit != "" {
		cfg.memoryLimit = profile.memoryLimit
	}
	if profile.gpuResource != "" {
		cfg.gpuResource = profile.gpuResource
	}
	if profile.gpuCount > 0 {
		cfg.gpuCount = profile.gpuCount
	}
}

func isStandardHardwareProfileResource(identifier string) bool {
	_, ok := standardHardwareProfileResources[identifier]
	return ok
}

func stringFromUnstructured(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func quantityStringFromUnstructured(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	case int:
		return strconv.Itoa(typed), true
	case int32:
		return strconv.FormatInt(int64(typed), 10), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case float64:
		return strconv.FormatInt(int64(typed), 10), true
	default:
		return "", false
	}
}
