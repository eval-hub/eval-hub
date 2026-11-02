package k8s

import (
	"strconv"
	"strings"
)

func sanitizeDNS1123Label(value string) string {
	safe := strings.ToLower(value)
	safe = k8sResourceNameSanitizer.ReplaceAllString(safe, "-")
	safe = strings.Trim(safe, "-")
	if safe == "" {
		return "x"
	}
	return safe
}

func sanitizeLabelValue(value string) string {
	safe := strings.ToLower(value)
	safe = k8sLabelValueSanitizer.ReplaceAllString(safe, "-")
	if len(safe) > maxK8sLabelValueLength {
		safe = safe[:maxK8sLabelValueLength]
	}
	safe = strings.Trim(safe, "-_.")
	if safe == "" {
		return "x"
	}
	return safe
}

// buildK8sName returns a DNS-1123-safe name:
// base = "<jobID>-<guid>", plus optional suffix, all kept within 63 chars.
// Truncation reserves room for suffix so the suffix is never chopped off.
func buildK8sName(jobID, resourceGUID, suffix string) string {
	return buildK8sNameWithReserve(jobID, resourceGUID, suffix, len(suffix))
}

// buildK8sNameWithReserve truncates the "<jobID>-<guid>" base so that
// base+reserveBytes fits in maxK8sNameLength, then appends suffix.
// Job and ConfigMap both reserve len(specSuffix) so they share one base:
// configMapName == jobName + "-spec".
func buildK8sNameWithReserve(jobID, resourceGUID, suffix string, reserveBytes int) string {
	if reserveBytes < len(suffix) {
		reserveBytes = len(suffix)
	}
	safeJobID := sanitizeDNS1123Label(jobID)
	safeGUID := sanitizeDNS1123Label(resourceGUID)
	maxJobID := maxK8sNameLength - reserveBytes - len(safeGUID) - 1
	if maxJobID < 1 {
		maxJobID = 1
	}
	if len(safeJobID) > maxJobID {
		safeJobID = strings.Trim(safeJobID[:maxJobID], "-")
		if safeJobID == "" {
			safeJobID = "x"
		}
	}
	base := safeJobID + "-" + safeGUID
	maxBase := maxK8sNameLength - reserveBytes
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = strings.Trim(base[:maxBase], "-")
		if base == "" {
			base = "x"
		}
	}
	name := base + suffix
	if len(name) > maxK8sNameLength {
		// Last resort: keep suffix, shrink base further.
		keep := maxK8sNameLength - len(suffix)
		if keep < 1 {
			keep = 1
		}
		base = strings.Trim(base[:min(len(base), keep)], "-")
		if base == "" {
			base = "x"
		}
		name = base + suffix
		if len(name) > maxK8sNameLength {
			name = strings.Trim(name[:maxK8sNameLength], "-")
		}
	}
	return name
}

func jobName(jobID, resourceGUID string) string {
	// Reserve space for "-spec" so Job and job-spec ConfigMap share one truncated base.
	return buildK8sNameWithReserve(jobID, resourceGUID, "", len(specSuffix))
}

func configMapName(jobID, resourceGUID string) string {
	return buildK8sNameWithReserve(jobID, resourceGUID, specSuffix, len(specSuffix))
}

func jobLabels(cfg *jobConfig) map[string]string {
	if cfg == nil {
		return map[string]string{}
	}
	m := map[string]string{
		labelAppKey:             labelAppValue,
		labelComponentKey:       labelComponentValue,
		labelJobIDKey:           sanitizeLabelValue(cfg.jobID),
		labelProviderIDKey:      sanitizeLabelValue(cfg.providerID),
		labelBenchmarkIDKey:     sanitizeLabelValue(cfg.benchmarkID),
		labelBenchmarkIndexKey:  sanitizeLabelValue(strconv.Itoa(cfg.benchmarkIndex)),
		labelEvaluationPhaseKey: EvaluationPhasePending,
	}
	if cfg.resourceGUID != "" {
		m[labelExecutionIDKey] = sanitizeLabelValue(cfg.resourceGUID)
	}
	if cfg.evalHubInstanceName != "" && cfg.evalHubCRNamespace != "" {
		m[labelEvalHubInstanceNameKey] = sanitizeLabelValue(cfg.evalHubInstanceName)
		m[labelEvalHubInstanceNamespaceKey] = sanitizeLabelValue(cfg.evalHubCRNamespace)
	}
	if cfg.queueKind == "kueue" && cfg.queueName != "" {
		m[labelKueueQueueNameKey] = cfg.queueName
		if cfg.priorityClassName != "" {
			m[labelKueuePriorityClassKey] = sanitizeLabelValue(cfg.priorityClassName)
		}
	}
	return m
}

func jobAnnotations(jobID, providerID, benchmarkID string) map[string]string {
	return map[string]string{
		annotationJobIDKey:       jobID,
		annotationProviderIDKey:  providerID,
		annotationBenchmarkIDKey: benchmarkID,
	}
}
