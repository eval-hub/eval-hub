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

// buildK8sName returns a DNS-1123-safe name for Jobs and ConfigMaps:
// base = "<jobID>-<guid>", plus optional suffix (e.g. "-spec" for ConfigMaps),
// all kept within 63 chars.
func buildK8sName(jobID, resourceGUID, suffix string) string {
	safeJobID := sanitizeDNS1123Label(jobID)
	safeGUID := sanitizeDNS1123Label(resourceGUID)
	maxJobID := maxK8sNameLength - len(suffix) - len(safeGUID) - 1
	if maxJobID < 1 {
		maxJobID = 1
	}
	if len(safeJobID) > maxJobID {
		safeJobID = strings.Trim(safeJobID[:maxJobID], "-")
	}
	name := safeJobID + "-" + safeGUID + suffix
	if len(name) > maxK8sNameLength {
		name = strings.Trim(name[:maxK8sNameLength], "-")
	}
	return name
}

func jobName(jobID, resourceGUID string) string {
	return buildK8sName(jobID, resourceGUID, "")
}

func configMapName(jobID, resourceGUID string) string {
	return buildK8sName(jobID, resourceGUID, specSuffix)
}

func jobLabels(cfg *jobConfig) map[string]string {
	if cfg == nil {
		return map[string]string{}
	}
	m := map[string]string{
		labelAppKey:            labelAppValue,
		labelComponentKey:      labelComponentValue,
		labelJobIDKey:          sanitizeLabelValue(cfg.jobID),
		labelProviderIDKey:     sanitizeLabelValue(cfg.providerID),
		labelBenchmarkIDKey:    sanitizeLabelValue(cfg.benchmarkID),
		labelBenchmarkIndexKey: sanitizeLabelValue(strconv.Itoa(cfg.benchmarkIndex)),
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
