package features

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Jeffail/gabs/v2"
	"github.com/PaesslerAG/jsonpath"
	"github.com/xeipuuv/gojsonschema"

	"github.com/cucumber/godog"
)

func (tc *scenarioConfig) theResponseStatusShouldBe(status int) error {
	if tc.response.StatusCode != status {
		return tc.logError(fmt.Errorf("expected status %d, got %d for request %s %s with response %s", status, tc.response.StatusCode, tc.lastMethod, tc.lastURL, string(tc.body)))
	}
	return nil
}

func (tc *scenarioConfig) theResponseStatusShouldBeOr(status1, status2 int) error {
	if (tc.response.StatusCode != status1) && (tc.response.StatusCode != status2) {
		return tc.logError(fmt.Errorf("expected status %d or %d, got %d for request %s %s with response %s", status1, status2, tc.response.StatusCode, tc.lastMethod, tc.lastURL, string(tc.body)))
	}
	return nil
}

func (tc *scenarioConfig) theResponseContentTypeShouldBe(contentType string) error {
	expected, err := tc.getValue(contentType)
	if err != nil {
		return err
	}
	actual := tc.response.Header.Get("Content-Type")
	if !strings.HasPrefix(actual, expected) {
		return tc.logError(fmt.Errorf("expected Content-Type to start with %q, got %q for request %s %s", expected, actual, tc.lastMethod, tc.lastURL))
	}
	return nil
}

func (tc *scenarioConfig) theResponseBodyShouldContain(text string) error {
	expected, err := tc.getValue(text)
	if err != nil {
		return err
	}
	body := string(tc.body)
	if !strings.Contains(body, expected) {
		return tc.logError(fmt.Errorf("expected response body to contain %q for request %s %s, got %q", expected, tc.lastMethod, tc.lastURL, body))
	}
	return nil
}

func (tc *scenarioConfig) theResponseShouldContainWithValue(key, value string) error {
	var data map[string]interface{}
	if err := json.Unmarshal(tc.body, &data); err != nil {
		return tc.logError(err)
	}

	v, err := tc.getValue(value)
	if err != nil {
		return err
	}

	if data[key] != v {
		return tc.logError(fmt.Errorf("expected %s to be %s, got %v in %s", key, v, data[key], asPrettyJson(string(tc.body))))
	}

	return nil
}

func (tc *scenarioConfig) theResponseShouldContain(key string) error {
	var data map[string]interface{}
	if err := json.Unmarshal(tc.body, &data); err != nil {
		return tc.logError(err)
	}

	k, err := tc.getValue(key)
	if err != nil {
		return err
	}

	if _, ok := data[k]; !ok {
		return tc.logError(fmt.Errorf("response does not contain key: %s in %s", k, asPrettyJson(string(tc.body))))
	}

	return nil
}

func (tc *scenarioConfig) theResponseShouldNotContain(key string) error {
	var data map[string]interface{}
	if err := json.Unmarshal(tc.body, &data); err != nil {
		return tc.logError(err)
	}

	k, err := tc.getValue(key)
	if err != nil {
		return err
	}

	if _, ok := data[k]; ok {
		return tc.logError(fmt.Errorf("response should not contain key %q but it exists in %s", k, asPrettyJson(string(tc.body))))
	}

	return nil
}

func (tc *scenarioConfig) theResponseShouldContainPrometheusMetrics() error {
	bodyStr := string(tc.body)
	if !strings.Contains(bodyStr, "# HELP") || !strings.Contains(bodyStr, "# TYPE") {
		return tc.logError(fmt.Errorf("response does not appear to be Prometheus metrics format"))
	}
	return nil
}

func (tc *scenarioConfig) theResponseShouldBeJSON() error {
	var data interface{}
	if err := json.Unmarshal(tc.body, &data); err != nil {
		return tc.logError(err)
	}
	return nil
}

func (tc *scenarioConfig) theMetricsShouldInclude(metricName string) error {
	bodyStr := string(tc.body)
	if !strings.Contains(bodyStr, metricName) {
		return tc.logError(fmt.Errorf("metrics do not include %s", metricName))
	}
	return nil
}

func (tc *scenarioConfig) theMetricsShouldShowRequestCountFor(path string) error {
	bodyStr := string(tc.body)
	// Check if metrics contain the path
	if !strings.Contains(bodyStr, path) {
		return tc.logError(fmt.Errorf("metrics do not show requests for path %s", path))
	}
	return nil
}

func asPrettyJson(s string) string {
	js := make(map[string]interface{})
	err := json.Unmarshal([]byte(s), &js)
	if err != nil {
		return s
	}
	ns, err := json.MarshalIndent(js, "", "  ")
	if err != nil {
		return s
	}
	return string(ns)
}

func (tc *scenarioConfig) compareJSONSchema(expectedSchema string, actualResponse string) error {
	expectedSchemaLoader := gojsonschema.NewStringLoader(expectedSchema)
	return tc.validateJSONSchema(expectedSchemaLoader, actualResponse)
}

func (tc *scenarioConfig) compareJSONSchemaFile(schemaFile string, actualResponse string) error {
	schemaContent, err := tc.getFile(schemaFile)
	if err != nil {
		return tc.logError(fmt.Errorf("schema file %s: %w", schemaFile, err))
	}
	return tc.compareJSONSchema(schemaContent, actualResponse)
}

func (tc *scenarioConfig) validateJSONSchema(expectedSchemaLoader gojsonschema.JSONLoader, actualResponse string) error {
	actualResultLoader := gojsonschema.NewStringLoader(actualResponse)
	result, validateErr := gojsonschema.Validate(expectedSchemaLoader, actualResultLoader)
	if validateErr != nil {
		fmt.Printf("The actual response %s does not match expected schema with error:\n", asPrettyJson(actualResponse))
		if result != nil {
			for _, err := range result.Errors() {
				fmt.Printf("- %s value = %s\n", err, err.Value())
			}
		}
		fmt.Printf("- error %s\n", validateErr.Error())
		return validateErr
	}
	if len(result.Errors()) > 0 {
		fmt.Printf("The actual response %s does not match expected schema with error:\n", asPrettyJson(actualResponse))
		for _, err := range result.Errors() {
			fmt.Printf("- %s value = %s\n", err, err.Value())
		}
		return fmt.Errorf("the response does not match the expected JSON schema")
	}
	if result.Valid() {
		return nil
	}
	return fmt.Errorf("failed to validate the response %s but no error detected", asPrettyJson(actualResponse))
}

func (tc *scenarioConfig) theResponseShouldHaveSchemaAs(body *godog.DocString) error {
	return tc.compareJSONSchema(body.Content, string(tc.body))
}

func (tc *scenarioConfig) theResponseShouldHaveSchemaFromFile(filePath string) error {
	filePath = strings.TrimPrefix(filePath, "file:/")
	return tc.compareJSONSchemaFile(filePath, string(tc.body))
}

func (tc *scenarioConfig) unquoteJsonPath(jsonPath string) string {
	s := strings.ReplaceAll(jsonPath, "&quot;", "\"")
	// s = strings.ReplaceAll(jsonPath, "&#39;", "'")
	return s
}

func (tc *scenarioConfig) getJsonPath(jsonPath string) (string, error) {
	jsonPath = tc.unquoteJsonPath(jsonPath)

	// first check the jsonpath is valid
	_, err := jsonpath.New(jsonPath)
	if err != nil {
		return "", fmt.Errorf("failed to validate JSON path %s: %w : %s", jsonPath, err, asPrettyJson(string(tc.body))) // logging of the error is done by the caller
	}

	raw, err := tc.getJsonPathValue(jsonPath)
	if err != nil {
		return "", err
	}
	raw = unwrapIfFilterResult(raw, jsonPath)
	return fmt.Sprintf("%v", raw), nil
}

func (tc *scenarioConfig) getJsonPathValue(jsonPath string) (interface{}, error) {
	var respMap map[string]interface{}
	err := json.Unmarshal(tc.body, &respMap)
	if err != nil {
		return "", err // logging of the error is done by the caller
	}
	path := jsonPath
	if !strings.HasPrefix(path, "$") {
		path = "$." + path
	}
	foundValue, err := jsonpath.Get(path, respMap)
	if err != nil {
		return "", fmt.Errorf("failed to get JSON path %s in %s: %w", jsonPath, asPrettyJson(string(tc.body)), err) // logging of the error is done by the caller
	}
	return foundValue, nil
}

func (tc *scenarioConfig) theResponseShouldContainAtJSONPath(expectedValue string, jsonPath string) error {
	_, _, err := tc.theResponseShouldContainAtJSONPathImpl(expectedValue, jsonPath, "contains")
	return err
}

func (tc *scenarioConfig) theResponseShouldEqualAtJSONPath(expectedValue string, jsonPath string) error {
	_, _, err := tc.theResponseShouldContainAtJSONPathImpl(expectedValue, jsonPath, "==")
	return err
}

func (tc *scenarioConfig) theResponseShouldContainAtJSONPathAtLeast(expectedValue string, jsonPath string) error {
	_, _, err := tc.theResponseShouldContainAtJSONPathImpl(expectedValue, jsonPath, ">=")
	return err
}

func (tc *scenarioConfig) theResponseShouldMatchAtJSONPath(expectedValue string, jsonPath string) error {
	_, _, err := tc.theResponseShouldContainAtJSONPathImpl(expectedValue, jsonPath, "matches")
	return err
}

func (tc *scenarioConfig) theResponseShouldContainAtJSONPathImpl(expectedValue string, jsonPath string, match string) (bool, string, error) {
	expanded, err := tc.substituteValues(expectedValue)
	if err != nil {
		return false, "", err
	}
	expectedValue = expanded

	foundValue, err := tc.getJsonPath(jsonPath)
	if err != nil {
		// true because the path is not found
		return true, foundValue, tc.logError(err)
	}

	if rawExpr, ok := strings.CutPrefix(expectedValue, regexpPrefix); ok {
		expr, err := regexp.Compile(rawExpr)
		if err != nil {
			return false, foundValue, tc.logError(fmt.Errorf("invalid regex %q: %w", rawExpr, err))
		}
		if expr.MatchString(foundValue) {
			tc.logDebug("Value %s matches regex %s in path %s", foundValue, rawExpr, jsonPath)
			return false, foundValue, nil
		}
	}

	values := strings.SplitSeq(expectedValue, "|")
	for value := range values {
		switch match {
		case "==", "equals":
			// first try an exact string match
			if foundValue == strings.TrimSpace(value) {
				return false, foundValue, nil
			}
			// then try a float match
			if fv, err := strconv.ParseFloat(foundValue, 64); err == nil {
				if ex, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					// compare the floats to 15 decimal places
					if math.Abs(fv-ex) < 0.0000000000000001 {
						return false, foundValue, nil
					}
				}
			}
		case "<=":
			fv, err := strconv.ParseFloat(foundValue, 64)
			if err != nil {
				return false, foundValue, tc.logError(fmt.Errorf("failed to parse found value %s as float: %w", foundValue, err))
			}
			ex, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				return false, foundValue, tc.logError(fmt.Errorf("failed to parse expected value %s as float: %w", value, err))
			}
			if fv <= ex {
				return false, foundValue, nil
			}
		case ">=":
			fv, err := strconv.ParseFloat(foundValue, 64)
			if err != nil {
				return false, foundValue, tc.logError(fmt.Errorf("failed to parse found value %s as float: %w", foundValue, err))
			}
			ex, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				return false, foundValue, tc.logError(fmt.Errorf("failed to parse expected value %s as float: %w", value, err))
			}
			if fv >= ex {
				return false, foundValue, nil
			}
		case "contains":
			if strings.Contains(foundValue, strings.TrimSpace(value)) {
				return false, foundValue, nil
			}
		case "matches":
			expr, err := regexp.Compile(strings.TrimSpace(value))
			if err != nil {
				return false, foundValue, tc.logError(fmt.Errorf("invalid regex %q: %w", strings.TrimSpace(value), err))
			}
			if expr.MatchString(foundValue) {
				return false, foundValue, nil
			}
		}
	}

	return true, foundValue, tc.logError(fmt.Errorf("expected %s to be %s but was %s in %s", jsonPath, expectedValue, foundValue, asPrettyJson(string(tc.body))))
}

func (tc *scenarioConfig) theResponseShouldNotContainAtJSONPath(expectedValue string, jsonPath string) error {
	_, found, _ := tc.theResponseShouldContainAtJSONPathImpl(expectedValue, jsonPath, "contains")
	expanded, err := tc.substituteValues(expectedValue)
	if err != nil {
		return err
	}
	if strings.Contains(strings.TrimSpace(found), strings.TrimSpace(expanded)) {
		return tc.logError(fmt.Errorf("expected %s to not contain %s but found %s in %s", jsonPath, expectedValue, found, asPrettyJson(string(tc.body))))
	}
	return nil
}

func (tc *scenarioConfig) theResponseShouldNotEqualAtJSONPath(expectedValue string, jsonPath string) error {
	_, found, _ := tc.theResponseShouldContainAtJSONPathImpl(expectedValue, jsonPath, "==")
	expanded, err := tc.substituteValues(expectedValue)
	if err != nil {
		return err
	}
	if strings.TrimSpace(found) == strings.TrimSpace(expanded) {
		return tc.logError(fmt.Errorf("expected %s to not equal %s but found %s in %s", jsonPath, expectedValue, found, asPrettyJson(string(tc.body))))
	}
	return nil
}

func (tc *scenarioConfig) theArrayAtPathInResponseShouldHaveLength(jsonPath string, lengthStr string) error {
	value, add, err := tc.getValueExpression(lengthStr)
	if err != nil {
		return err
	}
	value, err = tc.getValue(value)
	if err != nil {
		return tc.logError(err)
	}
	length, err := strconv.Atoi(value)
	if err != nil {
		return tc.logError(fmt.Errorf("expected integer length, got %q: %w", value, err))
	}
	length += add
	raw, err := tc.getJsonPathValue(jsonPath)
	if err != nil {
		return err
	}
	arr, ok := raw.([]any)
	if !ok {
		return tc.logError(fmt.Errorf("value at path %s is not an array, got %T", jsonPath, raw))
	}
	if len(arr) != length {
		return tc.logError(fmt.Errorf("expected array at path %s to have length %d, got %d in %s", jsonPath, length, len(arr), asPrettyJson(string(tc.body))))
	}
	return nil
}

func (tc *scenarioConfig) theArrayAtPathInResponseShouldHaveLengthAtLeast(jsonPath string, minLengthStr string) error {
	value, add, err := tc.getValueExpression(minLengthStr)
	if err != nil {
		return err
	}
	value, err = tc.getValue(value)
	if err != nil {
		return err
	}
	minLength, err := strconv.Atoi(value)
	if err != nil {
		return tc.logError(fmt.Errorf("expected integer min length, got %q: %w", value, err))
	}
	minLength += add
	raw, err := tc.getJsonPathValue(jsonPath)
	if err != nil {
		return err
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return tc.logError(fmt.Errorf("value at path %s is not an array, got %T", jsonPath, raw))
	}
	if len(arr) < minLength {
		return tc.logError(fmt.Errorf("expected array at path %s to have length >= %d, got %d in %s", jsonPath, minLength, len(arr), asPrettyJson(string(tc.body))))
	}
	return nil
}

func getJsonPointer(path string) string {
	// Strip JSONPath root indicators: $. or $
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")

	// Ensure it starts with / for JSON Pointer spec
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// Convert dot notation to slash notation
	path = strings.ReplaceAll(path, ".", "/")

	// Convert array notation [N] to /N/ for JSON Pointer spec
	// e.g., /benchmarks[0]/id becomes /benchmarks/0/id
	re := regexp.MustCompile(`\[(\d+)\]`)
	path = re.ReplaceAllString(path, "/$1")

	return path
}

func (tc *scenarioConfig) theFieldShouldBeSaved(path string, name string) error {
	jsonParsed, err := gabs.ParseJSON(tc.body)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to parse JSON response: %w", err))
	}
	// This directly uses a JSON pointer path
	pathObj, err := jsonParsed.JSONPointer(getJsonPointer(path))
	if err != nil {
		return tc.logError(fmt.Errorf("path %v does not exist in \n%s", path, string(tc.body)))
	}
	finalResult, ok := pathObj.Data().(string)
	if !ok {
		if floatResult, ok := pathObj.Data().(float64); ok {
			finalResult = strconv.FormatFloat(floatResult, 'f', -1, 64)
		} else {
			return tc.logError(fmt.Errorf("expected %s to be a string or float64 but got %T", path, pathObj.Data()))
		}
	}
	if strings.HasPrefix(name, valuePrefix) {
		realName := strings.TrimPrefix(name, valuePrefix)
		tc.saveValue(realName, finalResult)
		tc.logDebug("Saved value %s as %s\n", realName, finalResult)
	} else {
		return tc.logError(fmt.Errorf("unexpected value %s, should start with '%s'", name, valuePrefix))
	}
	return nil
}

func (tc *scenarioConfig) theBenchmarkShouldHaveMetric(benchmarkID, metricName string) error {
	raw, err := tc.getJsonPathValue("$.results.benchmarks")
	if err != nil {
		return tc.logError(err)
	}
	benchmarks, ok := raw.([]any)
	if !ok {
		return tc.logError(fmt.Errorf("$.results.benchmarks is not an array, got %T", raw))
	}
	for _, b := range benchmarks {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		id, _ := bm["id"].(string)
		if id != benchmarkID {
			continue
		}
		metrics, _ := bm["metrics"].(map[string]any)
		if _, exists := metrics[metricName]; exists {
			return nil
		}
		keys := make([]string, 0, len(metrics))
		for k := range metrics {
			keys = append(keys, k)
		}
		return tc.logError(fmt.Errorf("benchmark %q does not have metric %q, available: %v in %s",
			benchmarkID, metricName, keys, asPrettyJson(string(tc.body))))
	}
	return tc.logError(fmt.Errorf("benchmark %q not found in $.results.benchmarks in %s", benchmarkID, asPrettyJson(string(tc.body))))
}

func (tc *scenarioConfig) theAllBenchmarksHaveMetrics() error {
	raw, err := tc.getJsonPathValue("$.results.benchmarks")
	if err != nil {
		return tc.logError(err)
	}
	benchmarks, ok := raw.([]any)
	if !ok {
		return tc.logError(fmt.Errorf("$.results.benchmarks is not an array, got %T", raw))
	}
	var missing []string
	for i, b := range benchmarks {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		id, _ := bm["id"].(string)
		if id == "" {
			id = fmt.Sprintf("<unnamed@index %d>", i)
		}
		metrics, _ := bm["metrics"].(map[string]any)
		if len(metrics) == 0 {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return tc.logError(fmt.Errorf("expected all benchmarks to have metrics, but these had none: %s in %s",
			strings.Join(missing, ", "), asPrettyJson(string(tc.body))))
	}
	return nil
}

func (tc *scenarioConfig) theAllBenchmarksInStatusShouldBe(expectedStatus string) error {
	raw, err := tc.getJsonPathValue("$.status.benchmarks")
	if err != nil {
		return tc.logError(err)
	}
	benchmarks, ok := raw.([]any)
	if !ok {
		return tc.logError(fmt.Errorf("$.status.benchmarks is not an array, got %T", raw))
	}
	if len(benchmarks) == 0 {
		return tc.logError(fmt.Errorf("$.status.benchmarks is empty; cannot assert status %q", expectedStatus))
	}
	var failures []string
	for i, b := range benchmarks {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		id, _ := bm["id"].(string)
		if id == "" {
			id = fmt.Sprintf("<unnamed@index %d>", i)
		}
		status, _ := bm["status"].(string)
		if status != expectedStatus {
			failures = append(failures, fmt.Sprintf("%s: %s", id, status))
		}
	}
	if len(failures) > 0 {
		return tc.logError(fmt.Errorf("expected all benchmarks to have status %q, but found: %s in %s",
			expectedStatus, strings.Join(failures, ", "), asPrettyJson(string(tc.body))))
	}
	return nil
}

func (tc *scenarioConfig) fixThisStep() error {
	tc.logDebug("TODO: fix this step")
	return godog.ErrSkip
}

var (
	providerCacheMu sync.RWMutex
	// providerCache caches declared metrics per provider and benchmark, fetched from the API.
	// key: providerID → benchmarkID → []metricName
	providerCache = map[string]map[string][]string{}
)

// fetchProviderMetrics returns the declared metric names for a benchmark from the eval-hub
// providers API. Results are cached per provider to avoid redundant API calls. The request
// is made independently of tc.body / tc.response so the current scenario state is preserved.
func (tc *scenarioConfig) fetchProviderMetrics(providerID, benchmarkID string) ([]string, error) {
	providerCacheMu.RLock()
	if cached, ok := providerCache[providerID]; ok {
		providerCacheMu.RUnlock()
		metrics, found := cached[benchmarkID]
		if !found {
			return nil, fmt.Errorf("benchmark %q not found in provider %q", benchmarkID, providerID)
		}
		return metrics, nil
	}
	providerCacheMu.RUnlock()

	endpoint := joinBaseURL(tc.apiFeature.baseURL, "/api/v1/evaluations/providers/"+providerID)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider config request: %w", err)
	}
	if authToken := os.Getenv("AUTH_TOKEN"); authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	for k, v := range tc.reqHeaders {
		req.Header.Set(k, v)
	}
	resp, err := tc.apiFeature.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch provider config for %s: %w", providerID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider config API returned %d for %s", resp.StatusCode, providerID)
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read provider config response: %w", err)
	}

	var providerResp struct {
		Benchmarks []struct {
			ID      string   `json:"id"`
			Metrics []string `json:"metrics"`
		} `json:"benchmarks"`
	}
	if err := json.Unmarshal(respBody, &providerResp); err != nil {
		return nil, fmt.Errorf("failed to parse provider config response: %w", err)
	}

	benchmarkMetrics := make(map[string][]string, len(providerResp.Benchmarks))
	for _, b := range providerResp.Benchmarks {
		benchmarkMetrics[b.ID] = b.Metrics
	}
	providerCacheMu.Lock()
	if _, ok := providerCache[providerID]; !ok {
		providerCache[providerID] = benchmarkMetrics
	}
	providerCacheMu.Unlock()

	metrics, found := benchmarkMetrics[benchmarkID]
	if !found {
		return nil, fmt.Errorf("benchmark %q not found in provider %q", benchmarkID, providerID)
	}
	return metrics, nil
}

// theAllBenchmarksHaveMetricsMatchingProviderConfig asserts that every metric declared in the
// provider config for a benchmark is present in the API response. This forward check catches
// metrics that have silently stopped being reported.
func (tc *scenarioConfig) theAllBenchmarksHaveMetricsMatchingProviderConfig() error {
	raw, err := tc.getJsonPathValue("$.results.benchmarks")
	if err != nil {
		return tc.logError(err)
	}
	benchmarks, ok := raw.([]any)
	if !ok {
		return tc.logError(fmt.Errorf("$.results.benchmarks is not an array, got %T", raw))
	}
	if len(benchmarks) == 0 {
		return tc.logError(fmt.Errorf("$.results.benchmarks is empty"))
	}
	var failures []string
	for i, b := range benchmarks {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		id, _ := bm["id"].(string)
		if id == "" {
			id = fmt.Sprintf("<unnamed@index %d>", i)
		}
		providerID, _ := bm["provider_id"].(string)
		if providerID == "" {
			failures = append(failures, fmt.Sprintf("%s: has no provider_id in job response", id))
			continue
		}
		actualMetrics, _ := bm["metrics"].(map[string]any)
		if len(actualMetrics) == 0 {
			failures = append(failures, fmt.Sprintf("%s: no metrics returned", id))
			continue
		}
		declaredMetrics, err := tc.fetchProviderMetrics(providerID, id)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: config lookup failed: %v", id, err))
			continue
		}
		if len(declaredMetrics) == 0 {
			failures = append(failures, fmt.Sprintf("%s: provider config declares no metrics for this benchmark", id))
			continue
		}
		declared := make(map[string]struct{}, len(declaredMetrics))
		for _, m := range declaredMetrics {
			declared[m] = struct{}{}
		}
		for _, declaredMetric := range declaredMetrics {
			if _, present := actualMetrics[declaredMetric]; !present {
				failures = append(failures, fmt.Sprintf("%s: declared metric %q not returned by API", id, declaredMetric))
			}
		}
	}
	if len(failures) > 0 {
		return tc.logError(fmt.Errorf("benchmark metric mismatches:\n%s\nin %s",
			strings.Join(failures, "\n"), asPrettyJson(string(tc.body))))
	}
	return nil
}
