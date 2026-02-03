package features

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jeffail/gabs/v2"
	"github.com/PaesslerAG/jsonpath"
	"github.com/eval-hub/eval-hub/cmd/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/runtimes"
	"github.com/eval-hub/eval-hub/internal/storage"
	"github.com/eval-hub/eval-hub/internal/validation"
	"github.com/xeipuuv/gojsonschema"

	"github.com/cucumber/godog"
)

const (
	valuePrefix = "value:"
)

var (
	// testConfig to be used throughout all the test suites
	// for the global configuration
	api *apiFeature

	once   sync.Once
	logger *log.Logger
)

type apiFeature struct {
	baseURL    *url.URL
	server     *server.Server
	httpServer *http.Server
	client     *http.Client
}

// this is used for a scenario to ensure that scenarios do not overwrite
// data from other scenarios...
type scenarioConfig struct {
	scenarioName string
	apiFeature   *apiFeature
	response     *http.Response
	body         []byte

	lastURL string
	lastId  string

	assets map[string][]string

	values map[string]string
}

func getLogger() *log.Logger {
	once.Do(func() {
		if logger == nil {
			path := filepath.Join("..", "..", "bin", "tests.log")
			path, err := filepath.Abs(path)
			if err != nil {
				panic(logError(fmt.Errorf("Failed to get absolute path: %v", err)))
			}
			logOutput, err := os.Create(path)
			if err != nil {
				panic(logError(fmt.Errorf("Failed to create log file: %v", err)))
			}
			logger = log.New(logOutput, "", log.LstdFlags)
		}
	})
	return logger
}

func logDebug(format string, a ...any) {
	fmt.Printf(format, a...)
	getLogger().Printf(format, a...)
}

func logError(err error) error {
	getLogger().Printf("Error:%s\n%s\n", err.Error(), string(debug.Stack()))
	return err
}

func checkBaseURL(uri *url.URL, from string) {
	if uri == nil {
		panic("Invalid baseURL: nil from " + from)
	}
	if uri.String() == "" {
		panic("Empty baseURL from  " + from)
	}
}

func createApiFeature() (*apiFeature, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	if serverURL := os.Getenv("SERVER_URL"); serverURL != "" {
		uri, err := url.Parse(serverURL)
		if err != nil {
			return nil, logError(fmt.Errorf("Invalid SERVER_URL: %v", err))
		}
		checkBaseURL(uri, serverURL)
		return &apiFeature{client: client, baseURL: uri}, nil
	}

	port := 8080
	if sport := os.Getenv("PORT"); sport != "" {
		if eport, err := strconv.Atoi(sport); err != nil {
			logDebug("Invalid PORT: %v\n", err.Error())
		} else {
			port = eport
		}
	}

	uri := fmt.Sprintf("http://localhost:%d", port)
	baseURL, err := url.Parse(uri)
	if err != nil {
		panic(logError(fmt.Errorf("Invalid baseURL: %v", err)))
	}
	checkBaseURL(baseURL, uri)

	api := &apiFeature{
		client:  client,
		baseURL: baseURL,
	}
	api.startLocalServer(port)
	return api, nil
}

func (a *apiFeature) startLocalServer(port int) error {
	logger, _, err := logging.NewLogger()
	if err != nil {
		return err
	}
	validate, err := validation.NewValidator()
	if err != nil {
		return logError(fmt.Errorf("failed to create validator: %w", err))
	}
	serviceConfig, err := config.LoadConfig(logger, "0.0.1", "local", time.Now().Format(time.RFC3339))
	if err != nil {
		return logError(fmt.Errorf("failed to load service config: %w", err))
	}
	serviceConfig.Service.Port = port

	storage, err := storage.NewStorage(serviceConfig.Database, logger)
	if err != nil {
		return logError(fmt.Errorf("failed to create storage: %w", err))
	}
	logger.Info("Storage created.")

	// set up the provider configs
	providerConfigs, err := config.LoadProviderConfigs(logger, "../../../config/providers", "../../../../config/providers", "../../../../../config/providers")
	if err != nil {
		// we do this as no point trying to continue
		return logError(fmt.Errorf("failed to load provider configs: %w", err))
	}

	logger.Info("Providers loaded.")

	serviceConfig.Service.LocalMode = true // set local mode for testing
	runtime, err := runtimes.NewRuntime(logger, serviceConfig, providerConfigs)
	if err != nil {
		return logError(fmt.Errorf("failed to create runtime: %w", err))
	}

	a.server, err = server.NewServer(logger, serviceConfig, providerConfigs, storage, validate, runtime)
	if err != nil {
		return err
	}

	// Create a test server
	handler, err := a.server.SetupRoutes()
	a.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}

	// Start server in background
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}

	go func() {
		a.httpServer.Serve(listener)
	}()

	return nil
}

func (a *apiFeature) cleanup(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
	if a.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		a.httpServer.Shutdown(ctx)
	}
	return ctx, nil
}

func (tc *scenarioConfig) theServiceIsRunning(ctx context.Context) error {
	// Check that the server is actually running by sending a request to the health endpoint
	for range 10 {
		if err := tc.checkHealthEndpoint(); err != nil {
			logDebug("Error checking health endpoint: %v\n", err.Error())
			time.Sleep(1 * time.Second)
		} else {
			break
		}
	}

	return nil
}

func (tc *scenarioConfig) checkHealthEndpoint() error {
	if err := tc.iSendARequestTo("GET", "/api/v1/health"); err != nil {
		return logError(fmt.Errorf("failed to send health check request: %w for URL %s", err, tc.apiFeature.baseURL.String()))
	}
	if tc.response.StatusCode != 200 {
		return logError(fmt.Errorf("expected status 200, got %d", tc.response.StatusCode))
	}

	match := "\"status\":\"healthy\""
	if !strings.Contains(string(tc.body), match) {
		return logError(fmt.Errorf("expected body to contain %s, got %s", match, string(tc.body)))
	}

	return nil
}

func (tc *scenarioConfig) iSendARequestTo(method, path string) error {
	return tc.iSendARequestToWithBody(method, path, "")
}

func (tc *scenarioConfig) findFile(fileName string) (string, error) {
	file := filepath.Join("test_data", fileName)
	if _, err := os.Stat(file); os.IsNotExist(err) {
		path, _ := os.Getwd()
		return "", logError(fmt.Errorf("test file %s not found in directory %s", fileName, path))
	}
	return file, nil
}

func (tc *scenarioConfig) getFile(fileName string) (io.ReadCloser, error) {
	filePath, err := tc.findFile(fileName)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (tc *scenarioConfig) getRequestBody(body string) (io.Reader, error) {
	if body == "" {
		return nil, nil
	}
	// this can be an inline body or a test file
	if strings.HasPrefix(body, "file:/") {
		return tc.getFile(strings.TrimPrefix(body, "file:/"))
	}
	return strings.NewReader(body), nil
}

func (sc *scenarioConfig) addAsset(assetName, id string) {
	sc.assets[assetName] = append(sc.assets[assetName], id)
	logDebug("Added asset id %s for %s\n", id, assetName)
}

func (sc *scenarioConfig) removeAsset(assetName, id string) {
	ids := sc.assets[assetName]
	if slices.Contains(ids, id) {
		sc.assets[assetName] = slices.DeleteFunc(ids, func(s string) bool {
			if s == id {
				logDebug("Removed asset id %s for %s\n", id, assetName)
				return true
			}
			return false
		})
	}
}

func extractId(body []byte) (string, error) {
	obj := make(map[string]interface{})
	err := json.Unmarshal(body, &obj)
	if err != nil {
		return "", logError(fmt.Errorf("failed to unmarshal body %s: %w", string(body), err))
	}
	if id, ok := obj["resource"].(map[string]any)["id"].(string); ok {
		return id, nil
	}
	return "", nil
}

func extractIdFromPath(path string) string {
	if _, after, found := strings.Cut(path, "/api/v1/evaluations/jobs/"); found {
		if after != "" {
			if id, _, found := strings.Cut(after, "/"); found {
				return id
			}
			if id, _, found := strings.Cut(after, "?"); found {
				return id
			}
			return after
		}
	}
	return ""
}

// firstPathSegment matches the first path segment after /api/v1/
var firstPathSegment = regexp.MustCompile(`^.*/api/v1/([^/]+).*$`)

func getAssetName(path string) (string, error) {
	if matches := firstPathSegment.FindStringSubmatch(path); len(matches) >= 2 {
		return matches[1], nil
	}
	return "", logError(fmt.Errorf("no first path segment found in path %s", path))
}

func (tc *scenarioConfig) getId(id string) (string, error) {
	if strings.HasPrefix(id, valuePrefix) {
		n := strings.TrimPrefix(id, valuePrefix)
		v := tc.values[n]
		if v == "" {
			return "", logError(fmt.Errorf("failed to find value %s", n))
		}
		return v, nil
	}
	return id, nil
}

func (tc *scenarioConfig) getEndpoint(path string) (string, error) {
	check := true
	for check {
		if strings.Contains(path, fmt.Sprintf("{{%s", valuePrefix)) {
			re := regexp.MustCompile(`\{\{([^}]*)\}\}`)
			match := re.FindStringSubmatch(path)
			if len(match) > 1 {
				v, err := tc.getId(match[1])
				if err != nil {
					return "", logError(fmt.Errorf("failed to substitute value: %s", err.Error()))
				}
				path = strings.ReplaceAll(path, fmt.Sprintf("{{%s}}", match[1]), v)
			} else {
				// no more matches found
				check = false
			}
		} else {
			check = false
		}
	}

	if strings.Contains(path, "{id}") {
		if tc.lastId == "" {
			return "", logError(fmt.Errorf("last ID is not set"))
		}
		path = strings.Replace(path, "{id}", tc.lastId, 1)
	}

	endpoint := path
	if !strings.HasPrefix(endpoint, tc.apiFeature.baseURL.String()) {
		endpoint = fmt.Sprintf("%s%s", tc.apiFeature.baseURL.String(), path)
	}

	return endpoint, nil
}

func (tc *scenarioConfig) iSendARequestToWithBody(method, path, body string) error {
	endpoint, err := tc.getEndpoint(path)
	if err != nil {
		return err
	}
	tc.lastURL = endpoint
	entity, err := tc.getRequestBody(body)
	if err != nil {
		return err
	}
	logDebug("Sending %s request to %s\n", method, endpoint)
	req, err := http.NewRequest(method, endpoint, entity)
	if err != nil {
		logDebug("Failed to create request: %v\n", err)
		return err
	}

	tc.response, err = tc.apiFeature.client.Do(req)
	if err != nil {
		logDebug("Failed to send request: %v\n", err)
		return err
	}

	tc.body, err = io.ReadAll(tc.response.Body)
	if err != nil {
		return err
	}
	defer tc.response.Body.Close()

	logDebug("Response status %d for %s\n", tc.response.StatusCode, endpoint)

	// this is just for a create evaluation job request
	if method == http.MethodPost && tc.response.StatusCode == http.StatusAccepted {
		assetName, err := getAssetName(endpoint)
		if err != nil {
			return err
		}
		switch assetName {
		case "evaluations":
			tc.lastId, err = extractId(tc.body)
			if err != nil {
				return err
			}
			if tc.lastId == "" {
				return logError(fmt.Errorf("response does not contain an ID in response %s", string(tc.body)))
			}
			tc.addAsset(assetName, tc.lastId)
		default:
			// nothing to do here
		}
	}

	if method == http.MethodDelete {
		assetName, err := getAssetName(endpoint)
		if err != nil {
			return err
		}
		switch assetName {
		case "evaluations":
			id := extractIdFromPath(endpoint)
			if id == "" {
				return logError(fmt.Errorf("no ID found in path %s", endpoint))
			}
			tc.removeAsset(assetName, id)
		default:
			// nothing to do here
		}
	}

	return nil
}

func (tc *scenarioConfig) theResponseStatusShouldBe(status int) error {
	if tc.response.StatusCode != status {
		return logError(fmt.Errorf("expected status %d, got %d with response %s", status, tc.response.StatusCode, string(tc.body)))
	}
	return nil
}

func (tc *scenarioConfig) theResponseShouldBeJSON() error {
	contentType := tc.response.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return logError(fmt.Errorf("expected JSON content type, got %s", contentType))
	}

	var js interface{}
	if err := json.Unmarshal(tc.body, &js); err != nil {
		return logError(fmt.Errorf("response is not valid JSON: %v", err))
	}

	return nil
}

func (tc *scenarioConfig) theResponseShouldContainWithValue(key, value string) error {
	var data map[string]interface{}
	if err := json.Unmarshal(tc.body, &data); err != nil {
		return logError(err)
	}

	if data[key] != value {
		return logError(fmt.Errorf("expected %s to be %s, got %v", key, value, data[key]))
	}

	return nil
}

func (tc *scenarioConfig) theResponseShouldContain(key string) error {
	var data map[string]interface{}
	if err := json.Unmarshal(tc.body, &data); err != nil {
		return logError(err)
	}

	if _, ok := data[key]; !ok {
		return logError(fmt.Errorf("response does not contain key: %s", key))
	}

	return nil
}

func (tc *scenarioConfig) theResponseShouldContainPrometheusMetrics() error {
	bodyStr := string(tc.body)
	if !strings.Contains(bodyStr, "# HELP") || !strings.Contains(bodyStr, "# TYPE") {
		return logError(fmt.Errorf("response does not appear to be Prometheus metrics format"))
	}
	return nil
}

func (tc *scenarioConfig) theMetricsShouldInclude(metricName string) error {
	bodyStr := string(tc.body)
	if !strings.Contains(bodyStr, metricName) {
		return logError(fmt.Errorf("metrics do not include %s", metricName))
	}
	return nil
}

func (tc *scenarioConfig) theMetricsShouldShowRequestCountFor(path string) error {
	bodyStr := string(tc.body)
	// Check if metrics contain the path
	if !strings.Contains(bodyStr, path) {
		return logError(fmt.Errorf("metrics do not show requests for path %s", path))
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

func compareJSONSchema(expectedSchema string, actualResponse string) error {
	expectedSchemaLoader := gojsonschema.NewStringLoader(expectedSchema)
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
		return logError(fmt.Errorf("the response %s does not match %s", asPrettyJson(actualResponse), expectedSchema))
	}
	if result.Valid() {
		return nil
	}
	return logError(fmt.Errorf("failed to validate the response %s but no error detected when expecting %s", asPrettyJson(actualResponse), expectedSchema))
}

func (tc *scenarioConfig) theResponseShouldHaveSchemaAs(body *godog.DocString) error {
	return compareJSONSchema(body.Content, string(tc.body))
}

func (tc *scenarioConfig) getJsonPath(jsonPath string) (string, error) {
	var respMap map[string]interface{}
	err := json.Unmarshal(tc.body, &respMap)
	if err != nil {
		return "", logError(err)
	}

	foundValue, err := jsonpath.Get(jsonPath, respMap)
	if err != nil {
		return "", logError(fmt.Errorf("failed to get JSON path %s in %s: %w", jsonPath, string(tc.body), err))
	}

	return fmt.Sprintf("%v", foundValue), nil
}

func (tc *scenarioConfig) theResponseShouldContainAtJSONPath(expectedValue string, jsonPath string) error {
	foundValue, err := tc.getJsonPath(jsonPath)
	if err != nil {
		return logError(err)
	}

	// make this contains and not equals
	// if foundValue == strings.TrimSpace(expectedValue) {
	values := strings.Split(expectedValue, "|")
	for _, value := range values {
		if strings.Contains(foundValue, value) {
			return nil
		}
	}

	return logError(fmt.Errorf("expected %s to be %s but was %s", jsonPath, expectedValue, foundValue))
}

func getJsonPointer(path string) string {
	if !strings.HasPrefix(path, "/") {
		return strings.ReplaceAll(fmt.Sprintf("/%s", path), ".", "/")
	}
	return strings.ReplaceAll(path, ".", "/")
}

func (tc *scenarioConfig) theFieldShouldBeSaved(path string, name string) error {
	jsonParsed, err := gabs.ParseJSON(tc.body)
	if err != nil {
		return logError(fmt.Errorf("failed to parse JSON response: %w", err))
	}
	// This directly uses a JSON pointer path
	pathObj, err := jsonParsed.JSONPointer(getJsonPointer(path))
	if err != nil {
		return logError(fmt.Errorf("path %v does not exist in \n%s", path, string(tc.body)))
	}
	finalResult, ok := pathObj.Data().(string)
	if !ok {
		return logError(fmt.Errorf("expected %s to be a string but got %T", path, pathObj.Data()))
	}
	if strings.HasPrefix(name, valuePrefix) {
		tc.values[strings.TrimPrefix(name, valuePrefix)] = finalResult
	} else {
		return logError(fmt.Errorf("unexpected value %s, should start with '%s'", name, valuePrefix))
	}
	return nil
}

func (tc *scenarioConfig) saveScenarioName(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	tc.scenarioName = sc.Name
	return ctx, nil
}

func (tc *scenarioConfig) assetCleanup(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
	for assetName, ids := range tc.assets {
		url := assetName
		switch assetName {
		case "evaluations":
			url = "evaluations/jobs"
		}
		ids := slices.Clone(ids)
		for _, id := range ids {
			path := fmt.Sprintf("/api/v1/%s/%s?hard_delete=true", url, id)
			err := tc.iSendARequestTo("DELETE", path)
			if err != nil {
				return ctx, logError(fmt.Errorf("failed to delete asset %s with id '%s': %w", assetName, id, err))
			}
			err = tc.theResponseStatusShouldBe(204)
			if err != nil {
				return ctx, logError(fmt.Errorf("failed to delete asset %s expected status %d but got %d: %w", tc.lastURL, 204, tc.response.StatusCode, err))
			}
			logDebug("Deleted asset %s with status %d\n", path, tc.response.StatusCode)
		}
	}
	tc.assets = nil
	return ctx, nil
}

func createScenarioConfig(apiConfig *apiFeature) *scenarioConfig {
	conf := new(scenarioConfig)
	conf.assets = make(map[string][]string)
	conf.values = make(map[string]string)
	conf.apiFeature = apiConfig

	return conf
}

func setUpTestConf() {
	apiFeature, err := createApiFeature()
	if err != nil {
		panic(logError(fmt.Errorf("failed to create API feature: %v", err)))
	}
	api = apiFeature
}

func waitForService() {
	tc := createScenarioConfig(api)
	for range 10 {
		if err := tc.checkHealthEndpoint(); err != nil {
			logDebug("Error checking health endpoint: %v\n", err.Error())
			time.Sleep(1 * time.Second)
		} else {
			return
		}
	}
	panic("Stopped API Tests. Service is not ready for testing.\n")
}

func tidyUpTests() {
	if api != nil {
		api.cleanup(context.Background(), nil, nil)
	}
	if s, ok := logger.Writer().(*os.File); ok {
		err := s.Close()
		if err != nil {
			panic(fmt.Sprintf("Failed to close logger file: %v\n", err))
		}
	}
}

// A bit of a hack to have some checks that the regexes are working as expected
func checkRegexes() {
	paths := [][]string{
		{"/api/v1/evaluations", "evaluations"},
		{"/api/v1/evaluations/jobs", "evaluations"},
		{"/api/v1/evaluations/jobs/{id}", "evaluations"},
		{"/api/v1/evaluations/jobs/{id}/update", "evaluations"},
		{"/api/v1/collections", "collections"},
		{"/api/v1/collections/{id}", "collections"},
	}
	for _, path := range paths {
		name, err := getAssetName(path[0])
		if err != nil {
			panic(logError(fmt.Errorf("failed to get asset name for path %s: %v", path, err)))
		}
		if name != path[1] {
			panic(logError(fmt.Errorf("expected asset name %s for path %s, got %s", path[1], path[0], name)))
		}
	}
}

func InitializeTestSuite(ctx *godog.TestSuiteContext) {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		//nolint:gosec
		InsecureSkipVerify: true,
	}

	ctx.BeforeSuite(checkRegexes)

	ctx.BeforeSuite(setUpTestConf)
	ctx.BeforeSuite(waitForService)
	ctx.AfterSuite(tidyUpTests)
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	tc := createScenarioConfig(api)

	ctx.Before(tc.saveScenarioName)
	ctx.After(tc.assetCleanup)

	ctx.Step(`^the service is running$`, tc.theServiceIsRunning)
	ctx.Step(`^I send a (GET|DELETE|POST) request to "([^"]*)"$`, tc.iSendARequestTo)
	ctx.Step(`^I send a (POST|PUT|PATCH) request to "([^"]*)" with body "([^"]*)"$`, tc.iSendARequestToWithBody)
	ctx.Step(`^the response code should be (\d+)$`, tc.theResponseStatusShouldBe)
	ctx.Step(`^the response should be JSON$`, tc.theResponseShouldBeJSON)
	ctx.Step(`^the response should contain "([^"]*)" with value "([^"]*)"$`, tc.theResponseShouldContainWithValue)
	ctx.Step(`^the response should contain "([^"]*)"$`, tc.theResponseShouldContain)
	ctx.Step(`^the response should contain Prometheus metrics$`, tc.theResponseShouldContainPrometheusMetrics)
	ctx.Step(`^the metrics should include "([^"]*)"$`, tc.theMetricsShouldInclude)
	ctx.Step(`^the metrics should show request count for "([^"]*)"$`, tc.theMetricsShouldShowRequestCountFor)
	// Responses
	ctx.Step(`^the response should have schema as:$`, tc.theResponseShouldHaveSchemaAs)
	ctx.Step(`^the "([^"]*)" field in the response should be saved as "([^"]*)"$`, tc.theFieldShouldBeSaved)
	ctx.Step(`^the response should contain the value "([^"]*)" at path "([^"]*)"$`, tc.theResponseShouldContainAtJSONPath)
}
