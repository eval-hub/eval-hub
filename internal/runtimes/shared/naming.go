package shared

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
)

const (
	MaxK8sNameLength = 63
	JobPrefix        = "eval-job-"
)

var resourceNameSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizeDNS1123Label(value string) string {
	safe := strings.ToLower(value)
	safe = resourceNameSanitizer.ReplaceAllString(safe, "-")
	safe = strings.Trim(safe, "-")
	if safe == "" {
		return "x"
	}
	return safe
}

func shortHash(value string, length int) string {
	sum := sha1.Sum([]byte(value))
	hexValue := hex.EncodeToString(sum[:])
	if length <= 0 || length > len(hexValue) {
		return hexValue
	}
	return hexValue[:length]
}

func shortenJobID(jobID string, length int) string {
	safe := sanitizeDNS1123Label(jobID)
	if length <= 0 || len(safe) <= length {
		return safe
	}
	return strings.Trim(safe[:length], "-")
}

// BuildK8sName returns a DNS-1123-safe name for Jobs and ConfigMaps:
// base = "eval-job-<provider>-<benchmark>-<jobID8>", then "-<hash>" for uniqueness,
// and optional suffix (e.g. "-spec" for ConfigMaps), all kept within 63 chars.
func BuildK8sName(jobID, providerID, benchmarkID, suffix string) string {
	shortJobID := shortenJobID(jobID, 8)
	base := JobPrefix +
		sanitizeDNS1123Label(providerID) + "-" +
		sanitizeDNS1123Label(benchmarkID) + "-" +
		shortJobID

	hash := shortHash(jobID+"|"+providerID+"|"+benchmarkID, 8)
	maxBase := MaxK8sNameLength - len(suffix) - len(hash) - 1
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = strings.Trim(base[:maxBase], "-")
	}
	name := base + "-" + hash + suffix
	if len(name) > MaxK8sNameLength {
		name = strings.Trim(name[:MaxK8sNameLength], "-")
	}
	return name
}

// JobName returns the DNS-1123-safe name for an evaluation job.
func JobName(jobID, providerID, benchmarkID string) string {
	return BuildK8sName(jobID, providerID, benchmarkID, "")
}
