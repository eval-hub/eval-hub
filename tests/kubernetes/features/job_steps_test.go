package features

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ============================================================================
// Job Validation Steps
// ============================================================================

func (tc *testContext) jobShouldBeCreatedWithNamePattern(pattern string) error {
	// Convert pattern to regex
	regexPattern := strings.ReplaceAll(pattern, "{id}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{guid}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{resource_guid}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{benchmark_id}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{provider_id}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{hash}", ".*")
	regex, err := regexp.Compile("^" + regexPattern + "$")
	if err != nil {
		return fmt.Errorf("invalid Job name pattern %q: %w", pattern, err)
	}

	// List Jobs with job_id label if we have it
	listOptions := metav1.ListOptions{}
	if tc.lastJobID != "" {
		listOptions.LabelSelector = fmt.Sprintf("job_id=%s", tc.lastJobID)
	}

	jobs, err := tc.k8sClient.BatchV1().Jobs(tc.namespace).List(context.Background(), listOptions)
	if err != nil {
		return fmt.Errorf("failed to list Jobs: %w", err)
	}

	for i := range jobs.Items {
		job := &jobs.Items[i]
		if regex.MatchString(job.Name) {
			tc.currentJob = job
			tc.jobs = append(tc.jobs, job)

			// Extract job ID from labels if we don't have it yet
			if tc.lastJobID == "" {
				if jobID, ok := job.Labels["job_id"]; ok {
					tc.lastJobID = jobID
				}
			}

			return nil
		}
	}

	return fmt.Errorf("no Job found matching pattern %s (searched %d Jobs)", pattern, len(jobs.Items))
}

// matchLabel checks that labels[label] exists and optionally matches *target.
// On success it updates *target to the actual label value.
func (tc *testContext) matchLabel(labels map[string]string, label, entity string, target *string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	actualValue, exists := labels[label]
	if !exists {
		return fmt.Errorf("%s does not have label %s", entity, label)
	}
	if target != nil && *target != "" && actualValue != *target {
		return fmt.Errorf("%s label %s expected %s, got %s", entity, label, *target, actualValue)
	}
	if target != nil {
		*target = actualValue
	}
	return nil
}

func (tc *testContext) jobShouldHaveLabel(label, value string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	actualValue, exists := tc.currentJob.Labels[label]
	if !exists {
		return fmt.Errorf("Job %s does not have label %s", tc.currentJob.Name, label)
	}
	if actualValue != value {
		return fmt.Errorf("Job %s label %s expected %s, got %s", tc.currentJob.Name, label, value, actualValue)
	}
	return nil
}

func (tc *testContext) jobShouldHaveLabelMatchingJobID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	return tc.matchLabel(tc.currentJob.Labels, label, fmt.Sprintf("Job %s", tc.currentJob.Name), &tc.lastJobID)
}

func (tc *testContext) jobShouldHaveLabelMatchingProviderID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	return tc.matchLabel(tc.currentJob.Labels, label, fmt.Sprintf("Job %s", tc.currentJob.Name), &tc.lastProviderID)
}

func (tc *testContext) jobShouldHaveLabelMatchingBenchmarkID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	return tc.matchLabel(tc.currentJob.Labels, label, fmt.Sprintf("Job %s", tc.currentJob.Name), &tc.lastBenchmarkID)
}

func (tc *testContext) jobPodTemplateShouldHaveLabel(label, value string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	actualValue, exists := tc.currentJob.Spec.Template.Labels[label]
	if !exists {
		return fmt.Errorf("Job %s pod template does not have label %s", tc.currentJob.Name, label)
	}
	if actualValue != value {
		return fmt.Errorf("Job %s pod template label %s expected %s, got %s", tc.currentJob.Name, label, value, actualValue)
	}
	return nil
}

func (tc *testContext) jobPodTemplateShouldHaveLabelMatchingJobID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	return tc.matchLabel(tc.currentJob.Spec.Template.Labels, label, fmt.Sprintf("Job %s pod template", tc.currentJob.Name), &tc.lastJobID)
}

func (tc *testContext) jobPodTemplateShouldHaveLabelMatchingProviderID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	return tc.matchLabel(tc.currentJob.Spec.Template.Labels, label, fmt.Sprintf("Job %s pod template", tc.currentJob.Name), &tc.lastProviderID)
}

func (tc *testContext) jobPodTemplateShouldHaveLabelMatchingBenchmarkID(label string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}
	return tc.matchLabel(tc.currentJob.Spec.Template.Labels, label, fmt.Sprintf("Job %s pod template", tc.currentJob.Name), &tc.lastBenchmarkID)
}

func (tc *testContext) jobSpecShouldHaveRetryAttempts(field string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if field == "backoffLimit" {
		if tc.currentJob.Spec.BackoffLimit == nil {
			return fmt.Errorf("Job %s has no backoffLimit set", tc.currentJob.Name)
		}
		if *tc.currentJob.Spec.BackoffLimit < 0 {
			return fmt.Errorf("Job %s backoffLimit is negative: %d", tc.currentJob.Name, *tc.currentJob.Spec.BackoffLimit)
		}
		return nil
	}

	return fmt.Errorf("unknown field %s for retry attempts", field)
}

func (tc *testContext) jobSpecShouldHaveValue(field string, value int) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if field == "ttlSecondsAfterFinished" {
		if tc.currentJob.Spec.TTLSecondsAfterFinished == nil {
			return fmt.Errorf("Job %s has no %s set", tc.currentJob.Name, field)
		}
		if int(*tc.currentJob.Spec.TTLSecondsAfterFinished) != value {
			return fmt.Errorf("Job %s %s expected %d, got %d", tc.currentJob.Name, field, value, *tc.currentJob.Spec.TTLSecondsAfterFinished)
		}
		return nil
	}

	return fmt.Errorf("unknown field %s", field)
}

func (tc *testContext) jobTemplateSpecShouldHaveValue(field, value string) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if field == "restartPolicy" {
		actualValue := string(tc.currentJob.Spec.Template.Spec.RestartPolicy)
		if actualValue != value {
			return fmt.Errorf("Job %s restartPolicy expected %s, got %s", tc.currentJob.Name, value, actualValue)
		}
		return nil
	}

	return fmt.Errorf("unknown template spec field %s", field)
}

func (tc *testContext) jobNameShouldBeLowercase() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if tc.currentJob.Name != strings.ToLower(tc.currentJob.Name) {
		return fmt.Errorf("Job name %s is not lowercase", tc.currentJob.Name)
	}
	return nil
}

func (tc *testContext) jobNameShouldNotExceedLength(maxLength int) error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if len(tc.currentJob.Name) > maxLength {
		return fmt.Errorf("Job name %s exceeds %d characters (has %d)", tc.currentJob.Name, maxLength, len(tc.currentJob.Name))
	}
	return nil
}

func (tc *testContext) jobNameShouldBeAlphanumericAndHyphens() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	regex := regexp.MustCompile(`^[a-z0-9-]+$`)
	if !regex.MatchString(tc.currentJob.Name) {
		return fmt.Errorf("Job name %s contains invalid characters (must be alphanumeric and hyphens)", tc.currentJob.Name)
	}
	return nil
}

func (tc *testContext) jobNameShouldNotStartOrEndWithHyphen() error {
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	if strings.HasPrefix(tc.currentJob.Name, "-") || strings.HasSuffix(tc.currentJob.Name, "-") {
		return fmt.Errorf("Job name %s starts or ends with hyphen", tc.currentJob.Name)
	}
	return nil
}
