package features

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ============================================================================
// ConfigMap Validation Steps
// ============================================================================

func (tc *testContext) configMapShouldBeCreatedWithNamePattern(pattern string) error {
	// Convert pattern to regex
	regexPattern := strings.ReplaceAll(pattern, "{id}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{guid}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{resource_guid}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{benchmark_id}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{provider_id}", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "{hash}", ".*")
	regex := regexp.MustCompile(regexPattern)

	// List ConfigMaps with job_id label if we have it
	listOptions := metav1.ListOptions{}
	if tc.lastJobID != "" {
		listOptions.LabelSelector = fmt.Sprintf("job_id=%s", tc.lastJobID)
	}

	configMaps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), listOptions)
	if err != nil {
		return fmt.Errorf("failed to list ConfigMaps: %w", err)
	}

	for i := range configMaps.Items {
		cm := &configMaps.Items[i]
		if regex.MatchString(cm.Name) {
			tc.currentConfigMap = cm
			tc.configMaps = append(tc.configMaps, cm)

			// Extract job ID from labels if we don't have it yet
			if tc.lastJobID == "" {
				if jobID, ok := cm.Labels["job_id"]; ok {
					tc.lastJobID = jobID
				}
			}

			return nil
		}
	}

	return fmt.Errorf("no ConfigMap found matching pattern %s (searched %d ConfigMaps)", pattern, len(configMaps.Items))
}

func (tc *testContext) configMapShouldHaveLabel(label, value string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	actualValue, exists := tc.currentConfigMap.Labels[label]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have label %s", tc.currentConfigMap.Name, label)
	}
	if actualValue != value {
		return fmt.Errorf("ConfigMap %s label %s expected %s, got %s", tc.currentConfigMap.Name, label, value, actualValue)
	}
	return nil
}

func (tc *testContext) configMapShouldHaveLabelMatchingJobID(label string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	actualValue, exists := tc.currentConfigMap.Labels[label]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have label %s", tc.currentConfigMap.Name, label)
	}

	if tc.lastJobID != "" && actualValue != tc.lastJobID {
		return fmt.Errorf("ConfigMap %s label %s expected %s, got %s", tc.currentConfigMap.Name, label, tc.lastJobID, actualValue)
	}
	tc.lastJobID = actualValue
	return nil
}

func (tc *testContext) configMapShouldHaveLabelMatchingProviderID(label string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	actualValue, exists := tc.currentConfigMap.Labels[label]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have label %s", tc.currentConfigMap.Name, label)
	}

	tc.lastProviderID = actualValue
	return nil
}

func (tc *testContext) configMapShouldHaveLabelMatchingBenchmarkID(label string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	actualValue, exists := tc.currentConfigMap.Labels[label]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have label %s", tc.currentConfigMap.Name, label)
	}

	tc.lastBenchmarkID = actualValue
	return nil
}

func (tc *testContext) configMapShouldContainDataKey(key string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	if _, exists := tc.currentConfigMap.Data[key]; !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, key)
	}
	return nil
}

func (tc *testContext) configMapDataShouldBeValidJSON(dataKey string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	data, exists := tc.currentConfigMap.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, dataKey)
	}

	var js interface{}
	if err := json.Unmarshal([]byte(data), &js); err != nil {
		return fmt.Errorf("ConfigMap %s data %s is not valid JSON: %w", tc.currentConfigMap.Name, dataKey, err)
	}
	return nil
}

func (tc *testContext) configMapDataShouldContainFieldWithJobID(dataKey, field string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	data, exists := tc.currentConfigMap.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse job.json: %w", err)
	}

	value, exists := jobSpec[field]
	if !exists {
		return fmt.Errorf("job.json does not contain field %s", field)
	}

	tc.lastJobID = fmt.Sprintf("%v", value)
	return nil
}

func (tc *testContext) configMapDataShouldContainField(dataKey, field string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	data, exists := tc.currentConfigMap.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	// Handle nested fields like "model.url"
	parts := strings.Split(field, ".")
	current := jobSpec
	for i, part := range parts {
		if i == len(parts)-1 {
			if _, exists := current[part]; !exists {
				return fmt.Errorf("%s does not contain field %s", dataKey, field)
			}
		} else {
			next, ok := current[part].(map[string]interface{})
			if !ok {
				return fmt.Errorf("%s field %s is not an object", dataKey, part)
			}
			current = next
		}
	}

	return nil
}

func (tc *testContext) configMapDataShouldContainFieldAsObject(dataKey, field string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	data, exists := tc.currentConfigMap.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	value, exists := jobSpec[field]
	if !exists {
		return fmt.Errorf("%s does not contain field %s", dataKey, field)
	}

	if _, ok := value.(map[string]interface{}); !ok {
		return fmt.Errorf("%s field %s is not an object", dataKey, field)
	}

	return nil
}

func (tc *testContext) configMapDataShouldContainFieldFromParams(dataKey, field string) error {
	return tc.configMapDataShouldContainField(dataKey, field)
}

func (tc *testContext) configMapDataShouldContainFieldFromParamsForBenchmark(benchmark, dataKey, field string) error {
	cm, err := tc.findConfigMapForBenchmark(benchmark, dataKey)
	if err != nil {
		return err
	}
	return tc.configMapDataShouldContainFieldWithConfigMap(cm, dataKey, field)
}

func (tc *testContext) configMapDataFieldShouldNotContain(dataKey, field, subfield string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}
	return tc.configMapDataFieldShouldNotContainWithConfigMap(tc.currentConfigMap, dataKey, field, subfield)
}

func (tc *testContext) configMapDataFieldShouldNotContainForBenchmark(benchmark, dataKey, field, subfield string) error {
	cm, err := tc.findConfigMapForBenchmark(benchmark, dataKey)
	if err != nil {
		return err
	}
	return tc.configMapDataFieldShouldNotContainWithConfigMap(cm, dataKey, field, subfield)
}

func (tc *testContext) configMapDataShouldContainEmptyObject(benchmark, dataKey, field string) error {
	cm, err := tc.findConfigMapForBenchmark(benchmark, dataKey)
	if err != nil {
		return err
	}
	data, exists := cm.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", cm.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	value, exists := jobSpec[field]
	if !exists {
		return fmt.Errorf("%s does not contain field %s", dataKey, field)
	}

	fieldMap, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%s field %s is not an object", dataKey, field)
	}

	if len(fieldMap) != 0 {
		return fmt.Errorf("%s field %s is not empty, has %d keys", dataKey, field, len(fieldMap))
	}

	return nil
}

func (tc *testContext) findConfigMapForBenchmark(benchmark, dataKey string) (*corev1.ConfigMap, error) {
	// Find ConfigMap for specific benchmark (retry for async creation)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var configMaps []corev1.ConfigMap
		if tc.lastJobID != "" {
			maps, err := tc.listConfigMapsByJobIDFresh()
			if err != nil {
				return nil, err
			}
			configMaps = maps
		} else {
			maps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("benchmark_id=%s", benchmark),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to list ConfigMaps: %w", err)
			}
			configMaps = maps.Items
		}

		for i := range configMaps {
			candidate := &configMaps[i]
			if candidate.Labels["benchmark_id"] == benchmark {
				return candidate, nil
			}
			if dataKey != "" {
				if data, exists := candidate.Data[dataKey]; exists {
					var jobSpec map[string]interface{}
					if json.Unmarshal([]byte(data), &jobSpec) == nil {
						if jobBenchmark, ok := jobSpec["benchmark_id"].(string); ok && jobBenchmark == benchmark {
							return candidate, nil
						}
					}
				}
			}
		}
		if tc.lastJobID != "" {
			// Fallback: try by benchmark_id label in case job_id mismatch.
			maps, err := tc.k8sClient.CoreV1().ConfigMaps(tc.namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("benchmark_id=%s", benchmark),
			})
			if err == nil {
				for i := range maps.Items {
					candidate := &maps.Items[i]
					if candidate.Labels["benchmark_id"] == benchmark {
						return candidate, nil
					}
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	if os.Getenv("K8S_TEST_DEBUG") == "true" {
		status := 0
		if tc.response != nil {
			status = tc.response.StatusCode
		}
		fmt.Printf("[DEBUG] no ConfigMap found for benchmark %s (job_id=%s, namespace=%s)\n", benchmark, tc.lastJobID, tc.namespace)
		fmt.Printf("[DEBUG] last request status=%d\n", status)
		if tc.lastRequestBody != "" {
			fmt.Printf("[DEBUG] last request body: %s\n", tc.lastRequestBody)
		}
		if len(tc.body) > 0 {
			fmt.Printf("[DEBUG] last response body: %s\n", string(tc.body))
		}
	}
	return nil, fmt.Errorf("no ConfigMap found for benchmark %s", benchmark)
}

func (tc *testContext) configMapDataShouldContainFieldWithConfigMap(cm *corev1.ConfigMap, dataKey, field string) error {
	if cm == nil {
		return fmt.Errorf("no ConfigMap provided")
	}
	data, exists := cm.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", cm.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	// Handle nested fields like "model.url"
	parts := strings.Split(field, ".")
	current := jobSpec
	for i, part := range parts {
		if i == len(parts)-1 {
			if _, exists := current[part]; !exists {
				return fmt.Errorf("%s does not contain field %s", dataKey, field)
			}
		} else {
			next, ok := current[part].(map[string]interface{})
			if !ok {
				return fmt.Errorf("%s field %s is not an object", dataKey, part)
			}
			current = next
		}
	}

	return nil
}

func (tc *testContext) configMapDataFieldShouldNotContainWithConfigMap(cm *corev1.ConfigMap, dataKey, field, subfield string) error {
	if cm == nil {
		return fmt.Errorf("no ConfigMap provided")
	}

	data, exists := cm.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", cm.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	fieldValue, exists := jobSpec[field]
	if !exists {
		return fmt.Errorf("%s does not contain field %s", dataKey, field)
	}

	fieldMap, ok := fieldValue.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%s field %s is not an object", dataKey, field)
	}

	if _, exists := fieldMap[subfield]; exists {
		return fmt.Errorf("%s field %s should not contain %s", dataKey, field, subfield)
	}

	return nil
}

func (tc *testContext) configMapShouldHaveOwnerReference(kind string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	if len(tc.currentConfigMap.OwnerReferences) == 0 {
		return fmt.Errorf("ConfigMap %s has no owner references", tc.currentConfigMap.Name)
	}

	for _, ref := range tc.currentConfigMap.OwnerReferences {
		if ref.Kind == kind {
			return nil
		}
	}

	return fmt.Errorf("ConfigMap %s has no owner reference of kind %s", tc.currentConfigMap.Name, kind)
}

func (tc *testContext) configMapOwnerReferenceShouldHaveController() error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	if len(tc.currentConfigMap.OwnerReferences) == 0 {
		return fmt.Errorf("ConfigMap %s has no owner references", tc.currentConfigMap.Name)
	}

	for _, ref := range tc.currentConfigMap.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			return nil
		}
	}

	return fmt.Errorf("ConfigMap %s has no owner reference with controller=true", tc.currentConfigMap.Name)
}

func (tc *testContext) configMapOwnerReferenceShouldReferenceJob() error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}
	if tc.currentJob == nil {
		return fmt.Errorf("no current Job")
	}

	for _, ref := range tc.currentConfigMap.OwnerReferences {
		if ref.Kind == "Job" && ref.Name == tc.currentJob.Name {
			return nil
		}
	}

	return fmt.Errorf("ConfigMap %s does not reference Job %s", tc.currentConfigMap.Name, tc.currentJob.Name)
}

func (tc *testContext) configMapDataShouldContainFieldAsArray(dataKey, field string) error {
	if tc.currentConfigMap == nil {
		return fmt.Errorf("no current ConfigMap")
	}

	data, exists := tc.currentConfigMap.Data[dataKey]
	if !exists {
		return fmt.Errorf("ConfigMap %s does not have data key %s", tc.currentConfigMap.Name, dataKey)
	}

	var jobSpec map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jobSpec); err != nil {
		return fmt.Errorf("failed to parse %s: %w", dataKey, err)
	}

	value, exists := jobSpec[field]
	if !exists {
		return fmt.Errorf("%s does not contain field %s", dataKey, field)
	}

	if _, ok := value.([]interface{}); !ok {
		return fmt.Errorf("%s field %s is not an array", dataKey, field)
	}

	return nil
}
