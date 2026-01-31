package features

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/cmd/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/runtimes"
	"github.com/eval-hub/eval-hub/internal/storage"
	"github.com/eval-hub/eval-hub/internal/validation"

	"github.com/cucumber/godog"
)

var (
	// testConfig to be used throughout all the test suites
	// for the global configuration
	api *apiFeature
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

	lastId string

	assets map[string][]string
}

func logDebug(format string, a ...any) {
	fmt.Printf(format, a...)
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
			return nil, fmt.Errorf("Invalid SERVER_URL: %v", err)
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
		panic(fmt.Errorf("Invalid baseURL: %v", err))
	}
	checkBaseURL(baseURL, uri)

	api := &apiFeature{client: client, baseURL: baseURL}
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
		return fmt.Errorf("failed to create validator: %w", err)
	}
	serviceConfig, err := config.LoadConfig(logger, "0.0.1", "local", time.Now().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to load service config: %w", err)
	}
	serviceConfig.Service.Port = port

	storage, err := storage.NewStorage(serviceConfig.Database, logger)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}
	logger.Info("Storage created.")

	// set up the provider configs
	providerConfigs, err := config.LoadProviderConfigs(logger, "../../../config/providers", "../../../../config/providers", "../../../../../config/providers")
	if err != nil {
		// we do this as no point trying to continue
		return fmt.Errorf("failed to load provider configs: %w", err)
	}

	logger.Info("Providers loaded.")

	serviceConfig.Service.LocalMode = true // set local mode for testing
	runtime, err := runtimes.NewRuntime(logger, serviceConfig, providerConfigs)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
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
		return fmt.Errorf("failed to send health check request: %w for URL %s", err, tc.apiFeature.baseURL.String())
	}
	if tc.response.StatusCode != 200 {
		return fmt.Errorf("expected status 200, got %d", tc.response.StatusCode)
	}

	match := "\"status\":\"healthy\""
	if !strings.Contains(string(tc.body), match) {
		return fmt.Errorf("expected body to contain %s, got %s", match, string(tc.body))
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
		return "", fmt.Errorf("test file %s not found in directory %s", fileName, path)
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
	logDebug("Added asset id %s for id %s\n", id, assetName)
}

func (sc *scenarioConfig) removeAsset(assetName, id string) {
	ids := sc.assets[assetName]
	if slices.Contains(ids, id) {
		sc.assets[assetName] = slices.DeleteFunc(ids, func(s string) bool {
			return s == id
		})
	}
	logDebug("Removed asset id %s for id %s\n", id, assetName)
}

func extractId(body []byte) (string, error) {
	obj := make(map[string]interface{})
	err := json.Unmarshal(body, &obj)
	if err != nil {
		return "", err
	}
	if id, ok := obj["id"]; ok {
		return id.(string), nil
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
var firstPathSegment = regexp.MustCompile(`^/api/v1/([^/]+).*$`)

func getAssetName(path string) (string, error) {
	if matches := firstPathSegment.FindStringSubmatch(path); len(matches) >= 2 {
		return matches[1], nil
	}
	return "", fmt.Errorf("no first path segment found in path %s", path)
}

func (tc *scenarioConfig) iSendARequestToWithBody(method, path, body string) error {
	if strings.Contains(path, "{id}") {
		if tc.lastId == "" {
			return fmt.Errorf("last ID is not set")
		}
		path = strings.Replace(path, "{id}", tc.lastId, 1)
	}

	url := fmt.Sprintf("%s%s", tc.apiFeature.baseURL.String(), path)
	entity, err := tc.getRequestBody(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, url, entity)
	if err != nil {
		return err
	}

	tc.response, err = tc.apiFeature.client.Do(req)
	if err != nil {
		return err
	}

	tc.body, err = io.ReadAll(tc.response.Body)
	if err != nil {
		return err
	}
	defer tc.response.Body.Close()

	if method == http.MethodPost {
		assetName, err := getAssetName(path)
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
				return fmt.Errorf("response does not contain an ID in response %s", string(tc.body))
			}
			tc.addAsset(assetName, tc.lastId)
		default:
			// nothing to do here
		}
	}

	if method == http.MethodDelete {
		assetName, err := getAssetName(path)
		if err != nil {
			return err
		}
		switch assetName {
		case "evaluations":
			id := extractIdFromPath(path)
			if id == "" {
				return fmt.Errorf("no ID found in path %s", path)
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
		return fmt.Errorf("expected status %d, got %d", status, tc.response.StatusCode)
	}
	return nil
}

func (tc *scenarioConfig) theResponseShouldBeJSON() error {
	contentType := tc.response.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return fmt.Errorf("expected JSON content type, got %s", contentType)
	}

	var js interface{}
	if err := json.Unmarshal(tc.body, &js); err != nil {
		return fmt.Errorf("response is not valid JSON: %v", err)
	}

	return nil
}

func (tc *scenarioConfig) theResponseShouldContainWithValue(key, value string) error {
	var data map[string]interface{}
	if err := json.Unmarshal(tc.body, &data); err != nil {
		return err
	}

	if data[key] != value {
		return fmt.Errorf("expected %s to be %s, got %v", key, value, data[key])
	}

	return nil
}

func (tc *scenarioConfig) theResponseShouldContain(key string) error {
	var data map[string]interface{}
	if err := json.Unmarshal(tc.body, &data); err != nil {
		return err
	}

	if _, ok := data[key]; !ok {
		return fmt.Errorf("response does not contain key: %s", key)
	}

	return nil
}

func (tc *scenarioConfig) theResponseShouldContainPrometheusMetrics() error {
	bodyStr := string(tc.body)
	if !strings.Contains(bodyStr, "# HELP") || !strings.Contains(bodyStr, "# TYPE") {
		return fmt.Errorf("response does not appear to be Prometheus metrics format")
	}
	return nil
}

func (tc *scenarioConfig) theMetricsShouldInclude(metricName string) error {
	bodyStr := string(tc.body)
	if !strings.Contains(bodyStr, metricName) {
		return fmt.Errorf("metrics do not include %s", metricName)
	}
	return nil
}

func (tc *scenarioConfig) theMetricsShouldShowRequestCountFor(path string) error {
	bodyStr := string(tc.body)
	// Check if metrics contain the path
	if !strings.Contains(bodyStr, path) {
		return fmt.Errorf("metrics do not show requests for path %s", path)
	}
	return nil
}

func (tc *scenarioConfig) saveScenarioName(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	tc.scenarioName = sc.Name
	return ctx, nil
}

func (tc *scenarioConfig) assetCleanup(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
	for assetName, ids := range tc.assets {
		switch assetName {
		case "evaluations":
			assetName = "evaluation/jobs"
		}
		for _, id := range ids {
			path := fmt.Sprintf("/api/v1/%s/%s?hard_delete=true", assetName, id)
			err := tc.iSendARequestTo("DELETE", path)
			if err != nil {
				return ctx, fmt.Errorf("failed to delete asset with id '%s' %s: %w", assetName, id, err)
			}
			if tc.response.StatusCode != 204 {
				return ctx, fmt.Errorf("expected status 204, got %d for asset id '%s' with path %s", tc.response.StatusCode, id, path)
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

	conf.apiFeature = apiConfig

	return conf
}

func setUpTestConf() {
	apiFeature, err := createApiFeature()
	if err != nil {
		panic(fmt.Errorf("failed to create API feature: %v", err))
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
			panic(fmt.Errorf("failed to get asset name for path %s: %v", path, err))
		}
		if name != path[1] {
			panic(fmt.Errorf("expected asset name %s for path %s, got %s", path[1], path[0], name))
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
	// steps for entities
	//ctx.Step(`^the entity should be created with ID "([^"]*)"$`, tc.theEntityShouldBeCreatedWithID)
	//ctx.Step(`^the entity should be deleted with ID "([^"]*)"$`, tc.theEntityShouldBeDeletedWithID)
	//ctx.Step(`^the entity should be updated with ID "([^"]*)"$`, tc.theEntityShouldBeUpdatedWithID)
	//ctx.Step(`^the entity should be retrieved with ID "([^"]*)"$`, tc.theEntityShouldBeRetrievedWithID)
	//ctx.Step(`^the entity should be listed with ID "([^"]*)"$`, tc.theEntityShouldBeListedWithID)
	//ctx.Step(`^the entity should be counted with ID "([^"]*)"$`, tc.theEntityShouldBeCountedWithID)
}
