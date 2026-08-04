package k8s

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	hardwareProfileAPIGroup           = "infrastructure.opendatahub.io"
	hardwareProfileAPIVersion         = "v1"
	hardwareProfileResource           = "hardwareprofiles"
	hardwareProfilesNamespaceEnv      = "EVALHUB_HARDWARE_PROFILES_NAMESPACE"
	hardwareProfileDisabledAnnotation = "opendatahub.io/disabled"
	hardwareProfileSchedulingNode     = "Node"
	hardwareProfileSchedulingQueue    = "Queue"
	hardwareProfilePriorityNone       = "None"
)

var standardHardwareProfileResources = map[string]struct{}{
	"cpu":               {},
	"memory":            {},
	"ephemeral-storage": {},
}

// hardwareProfileResources holds resource and scheduling values extracted from a HardwareProfile CR.
// Empty strings and zero counts mean the field was not set in the profile.
type hardwareProfileResources struct {
	cpuRequest    string
	cpuLimit      string
	memoryRequest string
	memoryLimit   string
	gpuResource   string
	gpuCount      int

	schedulingType    string // "Node", "Queue", or empty
	nodeSelector      map[string]string
	tolerations       []corev1.Toleration
	queueName         string
	priorityClassName string
}

func parseHardwareProfileResources(profile *unstructured.Unstructured) (*hardwareProfileResources, error) {
	if profile == nil {
		return nil, fmt.Errorf("hardware profile is required")
	}
	out := &hardwareProfileResources{}
	if err := parseHardwareProfileIdentifiers(profile, out); err != nil {
		return nil, err
	}
	if err := parseHardwareProfileScheduling(profile, out); err != nil {
		return nil, err
	}
	return out, nil
}

func parseHardwareProfileIdentifiers(profile *unstructured.Unstructured, out *hardwareProfileResources) error {
	identifiers, found, err := unstructured.NestedSlice(profile.Object, "spec", "identifiers")
	if err != nil {
		return fmt.Errorf("read hardware profile identifiers: %w", err)
	}
	if !found || len(identifiers) == 0 {
		return nil
	}

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
					return fmt.Errorf("parse accelerator count for %q: %w", identifier, err)
				}
				out.gpuCount = count
			}
		}
	}
	return nil
}

func parseHardwareProfileScheduling(profile *unstructured.Unstructured, out *hardwareProfileResources) error {
	scheduling, found, err := unstructured.NestedMap(profile.Object, "spec", "scheduling")
	if err != nil {
		return fmt.Errorf("read hardware profile scheduling: %w", err)
	}
	if !found || len(scheduling) == 0 {
		return nil
	}

	schedulingType := strings.TrimSpace(stringFromUnstructured(scheduling["type"]))
	out.schedulingType = schedulingType

	switch schedulingType {
	case hardwareProfileSchedulingNode:
		node, _, err := unstructured.NestedMap(scheduling, "node")
		if err != nil {
			return fmt.Errorf("read hardware profile scheduling.node: %w", err)
		}
		if len(node) == 0 {
			return nil
		}
		selector, _, err := unstructured.NestedStringMap(node, "nodeSelector")
		if err != nil {
			return fmt.Errorf("read hardware profile nodeSelector: %w", err)
		}
		if len(selector) > 0 {
			out.nodeSelector = selector
		}
		tolerationsRaw, _, err := unstructured.NestedSlice(node, "tolerations")
		if err != nil {
			return fmt.Errorf("read hardware profile tolerations: %w", err)
		}
		tolerations, err := parseHardwareProfileTolerations(tolerationsRaw)
		if err != nil {
			return err
		}
		out.tolerations = tolerations
	case hardwareProfileSchedulingQueue:
		kueue, _, err := unstructured.NestedMap(scheduling, "kueue")
		if err != nil {
			return fmt.Errorf("read hardware profile scheduling.kueue: %w", err)
		}
		if len(kueue) == 0 {
			return nil
		}
		out.queueName = strings.TrimSpace(stringFromUnstructured(kueue["localQueueName"]))
		priority := strings.TrimSpace(stringFromUnstructured(kueue["priorityClass"]))
		if priority != "" && !strings.EqualFold(priority, hardwareProfilePriorityNone) {
			out.priorityClassName = priority
		}
	}
	return nil
}

func parseHardwareProfileTolerations(raw []any) ([]corev1.Toleration, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]corev1.Toleration, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		toleration := corev1.Toleration{
			Key:               strings.TrimSpace(stringFromUnstructured(m["key"])),
			Operator:          corev1.TolerationOperator(strings.TrimSpace(stringFromUnstructured(m["operator"]))),
			Value:             strings.TrimSpace(stringFromUnstructured(m["value"])),
			Effect:            corev1.TaintEffect(strings.TrimSpace(stringFromUnstructured(m["effect"]))),
			TolerationSeconds: int64PtrFromUnstructured(m["tolerationSeconds"]),
		}
		out = append(out, toleration)
	}
	return out, nil
}

func int64PtrFromUnstructured(value any) *int64 {
	switch typed := value.(type) {
	case int64:
		v := typed
		return &v
	case int32:
		v := int64(typed)
		return &v
	case int:
		v := int64(typed)
		return &v
	case float64:
		v := int64(typed)
		return &v
	default:
		return nil
	}
}

func isHardwareProfileDisabled(profile *unstructured.Unstructured) bool {
	if profile == nil {
		return false
	}
	annotations := profile.GetAnnotations()
	if len(annotations) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(annotations[hardwareProfileDisabledAnnotation]), "true")
}

// hardwareProfilesNamespace returns the platform namespace used to fetch HardwareProfiles.
// Profiles always live in EVALHUB_HARDWARE_PROFILES_NAMESPACE (ODH or RHOAI platform NS).
func hardwareProfilesNamespace() (string, error) {
	if ns := strings.TrimSpace(os.Getenv(hardwareProfilesNamespaceEnv)); ns != "" {
		return ns, nil
	}
	return "", fmt.Errorf("%s is not set", hardwareProfilesNamespaceEnv)
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

	switch profile.schedulingType {
	case hardwareProfileSchedulingQueue:
		if profile.queueName != "" {
			cfg.queueKind = "kueue"
			cfg.queueName = profile.queueName
			// Kueue ResourceFlavors govern placement; drop provider/profile node constraints
			// only when a LocalQueue is actually configured.
			cfg.nodeSelector = nil
			cfg.tolerations = nil
		}
		cfg.priorityClassName = profile.priorityClassName
	case hardwareProfileSchedulingNode:
		if len(profile.nodeSelector) > 0 {
			cfg.nodeSelector = copyStringMap(profile.nodeSelector)
		}
		if len(profile.tolerations) > 0 {
			cfg.tolerations = append([]corev1.Toleration(nil), profile.tolerations...)
		}
	}
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
