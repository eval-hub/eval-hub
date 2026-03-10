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
	"github.com/eval-hub/eval-hub/internal/mlflow"
	"github.com/eval-hub/eval-hub/internal/runtimes"
	"github.com/eval-hub/eval-hub/internal/storage"
	"github.com/eval-hub/eval-hub/internal/validation"
	pkgapi "github.com/eval-hub/eval-hub/pkg/api"
	"github.com/xeipuuv/gojsonschema"

	"github.com/cucumber/godog"
)

const (
	valuePrefix  = "value:"
	mlflowPrefix = "mlflow:"
	envPrefix    = "env:"
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

	reqHeaders map[string]string

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

func logError(err error, withStack ...bool) error {
	if len(withStack) > 0 && withStack[0] {
		getLogger().Printf("Error: %v\n%s\n", err, string(debug.Stack()))
	} else {
		getLogger().Printf("Error: %v\n", err)
	}
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
	timeout := 60 * time.Second
	if timeoutStr := os.Getenv("TEST_TIMEOUT"); timeoutStr != "" {
		if eTimeout, err := strconv.Atoi(timeoutStr); err != nil {
			logDebug("Invalid TEST_TIMEOUT: %v\n", err.Error())
		} else {
			timeout = time.Duration(eTimeout) * time.Second
		}
	}
	client := &http.Client{
		Timeout: timeout,
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
	serviceConfig, err := config.LoadConfig(logger, "0.2.0", "local", time.Now().Format(time.RFC3339))
	if err != nil {
		return logError(fmt.Errorf("failed to load service config: %w", err))
	}
	serviceConfig.Service.Port = port
	serviceConfig.Service.LocalMode = true // set local mode for testing

	storage, err := storage.NewStorage(serviceConfig.Database, serviceConfig.IsOTELEnabled(), serviceConfig.IsAuthenticationEnabled(), logger)
	if err != nil {
		return logError(fmt.Errorf("failed to create storage: %w", err))
	}
	logger.Info("Storage created.")

	// set up the provider configs
	providerConfigs, err := config.LoadProviderConfigs(logger)
	if err != nil {
		// we do this as no point trying to continue
		return logError(fmt.Errorf("failed to load provider configs: %w", err))
	}

	if len(providerConfigs) == 0 {
		return logError(fmt.Errorf("no provider configs loaded"))
	}

	logger.Info("Providers loaded.")
	// Override local runtime commands for testing so subprocesses
	// Exit cleanly instead of failing with "command not found".
	for key := range providerConfigs {
		providerCfg := providerConfigs[key]
		if providerCfg.Runtime == nil {
			return logError(fmt.Errorf("provider %q has no runtime configuration", providerCfg.Resource.ID))
		}
		if providerCfg.Runtime.Local == nil {
			providerCfg.Runtime.Local = &pkgapi.LocalRuntime{}
		}
		providerCfg.Runtime.Local.Command = "true"
		providerConfigs[key] = providerCfg
	}

	runtime, err := runtimes.NewRuntime(logger, serviceConfig, providerConfigs)
	if err != nil {
		return logError(fmt.Errorf("failed to create runtime: %w", err))
	}

	mlflowClient, err := mlflow.NewMLFlowClient(serviceConfig, logger)
	if err != nil {
		return logError(fmt.Errorf("failed to create MLFlow client: %w", err))
	}

	a.server, err = server.NewServer(logger,
		serviceConfig,
		providerConfigs,
		nil,
		storage,
		validate,
		runtime,
		mlflowClient)
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

func (tc *scenarioConfig) logDebug(format string, a ...any) {
	if v, exists := tc.reqHeaders[server.TRANSACTION_ID_HEADER]; exists && v != "" {
		format = fmt.Sprintf("(%s) %s", v, format)
	}
	fmt.Printf(format, a...)
	getLogger().Printf(format, a...)
}

func (tc *scenarioConfig) logError(err error, withStack ...bool) error {
	var sb = strings.Builder{}
	sb.WriteString("Error")
	if reqId, exists := tc.reqHeaders[server.TRANSACTION_ID_HEADER]; exists && reqId != "" {
		sb.WriteString(fmt.Sprintf(" (%s)", reqId))
	}
	sb.WriteString(": ")
	if len(withStack) > 0 && withStack[0] {
		getLogger().Printf("%s%v\n%s\n", sb.String(), err, string(debug.Stack()))
	} else {
		getLogger().Printf("%s%v\n", sb.String(), err)
	}
	return fmt.Errorf("%s%v", sb.String(), err)
}

func (tc *scenarioConfig) theServiceIsRunning(ctx context.Context) error {
	// Check that the server is actually running by sending a request to the health endpoint
	for range 10 {
		if err := tc.checkHealthEndpoint(); err != nil {
			tc.logDebug("Error checking health endpoint: %v\n", err.Error())
			time.Sleep(1 * time.Second)
		} else {
			break
		}
	}

	return nil
}

func (tc *scenarioConfig) thereAreNoUserProviders(ctx context.Context) error {
	if err := tc.iSendARequestTo("GET", "/api/v1/evaluations/providers?system_defined=false&limit=100"); err != nil {
		return err
	}
	if tc.response.StatusCode != 200 {
		return tc.logError(fmt.Errorf("expected 200 listing user providers, got %d: %s", tc.response.StatusCode, string(tc.body)))
	}
	var resp struct {
		Items []struct {
			Resource struct {
				ID string `json:"id"`
			} `json:"resource"`
		} `json:"items"`
	}
	if err := json.Unmarshal(tc.body, &resp); err != nil {
		return tc.logError(fmt.Errorf("failed to parse providers list: %w", err))
	}
	for _, item := range resp.Items {
		if item.Resource.ID != "" {
			if err := tc.iSendARequestTo("DELETE", "/api/v1/evaluations/providers/"+item.Resource.ID); err != nil {
				return err
			}
			if tc.response != nil && tc.response.StatusCode != 204 {
				return tc.logError(fmt.Errorf("failed to delete provider %s: status %d", item.Resource.ID, tc.response.StatusCode))
			}
		}
	}
	return nil
}

func (tc *scenarioConfig) thereAreNoEvaluationJobs(ctx context.Context) error {
	if err := tc.iSendARequestTo("GET", "/api/v1/evaluations/jobs?limit=100"); err != nil {
		return err
	}
	if tc.response.StatusCode != 200 {
		return tc.logError(fmt.Errorf("expected 200 listing evaluation jobs, got %d: %s", tc.response.StatusCode, string(tc.body)))
	}
	var resp struct {
		Items []struct {
			Resource struct {
				ID string `json:"id"`
			} `json:"resource"`
		} `json:"items"`
	}
	if err := json.Unmarshal(tc.body, &resp); err != nil {
		return tc.logError(fmt.Errorf("failed to parse evaluation jobs list: %w", err))
	}
	for _, item := range resp.Items {
		if item.Resource.ID != "" {
			if err := tc.iSendARequestTo("DELETE", "/api/v1/evaluations/jobs/"+item.Resource.ID+"?hard_delete=true"); err != nil {
				return err
			}
			if tc.response != nil && tc.response.StatusCode != 204 {
				return tc.logError(fmt.Errorf("failed to delete evaluation job %s: status %d", item.Resource.ID, tc.response.StatusCode))
			}
		}
	}
	return nil
}

func (tc *scenarioConfig) checkHealthEndpoint() error {
	if err := tc.iSendARequestTo("GET", "/api/v1/health"); err != nil {
		return tc.logError(fmt.Errorf("failed to send health check request: %w for URL %s", err, tc.apiFeature.baseURL.String()))
	}
	if tc.response.StatusCode != 200 {
		return tc.logError(fmt.Errorf("expected status 200, got %d", tc.response.StatusCode))
	}

	match := "\"status\":\"healthy\""
	if !strings.Contains(string(tc.body), match) {
		return tc.logError(fmt.Errorf("expected body to contain %s, got %s", match, string(tc.body)))
	}

	return nil
}

func (tc *scenarioConfig) iSetHeaderTo(paramName, paramValue string) error {
	value, err := tc.getId(paramValue)
	if err != nil {
		return err
	}
	tc.reqHeaders[paramName] = value
	return nil
}

func (tc *scenarioConfig) iUnsetHeader(paramName string) error {
	delete(tc.reqHeaders, paramName)
	return nil
}

func (tc *scenarioConfig) iSetTransactionIdTo(paramValue string) error {
	return tc.iSetHeaderTo(server.TRANSACTION_ID_HEADER, paramValue)
}

func (tc *scenarioConfig) iSendARequestTo(method, path string) error {
	return tc.iSendARequestToWithBody(method, path, "")
}

// isLocalOrCIMode returns true when the test cannot reliably wait for evaluation job
// completion (e.g. GitHub Actions, localhost, or in-process server). Scenarios that
// require waiting for job completion should use the explicit step "When the mode is local or CI then skip this scenario" at the start.
func (tc *scenarioConfig) isLocalOrCIMode() bool {
	githubActions := os.Getenv("GITHUB_ACTIONS")
	serverURL := os.Getenv("SERVER_URL")
	isLocalhost := false
	if serverURL != "" {
		host := serverURL
		if parsed, err := url.Parse(serverURL); err == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
		}
		isLocalhost = host == "localhost" || host == "127.0.0.1" || host == "::1"
	}
	isLocalMode := serverURL == "" || (tc.apiFeature != nil && tc.apiFeature.server != nil)
	return githubActions == "true" || isLocalhost || isLocalMode
}

// whenTheModeIsLocalOrCIThenSkipThisScenario skips the scenario when running in local or CI
// mode so that scenarios requiring job completion are explicitly skipped instead of failing or timing out.
func (tc *scenarioConfig) whenTheModeIsLocalOrCIThenSkipThisScenario() error {
	if tc.isLocalOrCIMode() {
		tc.logDebug("Skipping scenario: mode is local or CI (cannot wait for job completion)\n")
		return godog.ErrSkip
	}
	return nil
}

func (tc *scenarioConfig) iWaitForEvaluationJobStatus(expectedStatus string) error {
	deadline := time.Now().Add(2 * time.Minute)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := tc.iSendARequestTo(http.MethodGet, "/api/v1/evaluations/jobs/{id}"); err != nil {
			lastErr = err
			time.Sleep(1 * time.Second)
			continue
		}
		if tc.response != nil && tc.response.StatusCode == http.StatusOK {
			status, err := tc.getJsonPath("$.status.state")
			if err != nil {
				lastErr = err
			} else if status == expectedStatus {
				return nil
			} else {
				lastErr = fmt.Errorf("expected status %q but got %q", expectedStatus, status)
			}
		} else if tc.response != nil {
			lastErr = tc.logError(fmt.Errorf("unexpected response status %d", tc.response.StatusCode))
		}
		time.Sleep(1 * time.Second)
	}
	if lastErr != nil {
		return tc.logError(lastErr)
	}
	return tc.logError(fmt.Errorf("timed out waiting for status %q", expectedStatus))
}

func (tc *scenarioConfig) findFile(fileName string) (string, error) {
	file := filepath.Join("test_data", fileName)
	if _, err := os.Stat(file); os.IsNotExist(err) {
		path, _ := os.Getwd()
		return "", tc.logError(fmt.Errorf("test file %s not found in directory %s", fileName, path))
	}
	return file, nil
}

func (tc *scenarioConfig) getFile(fileName string) (string, error) {
	filePath, err := tc.findFile(fileName)
	if err != nil {
		return "", err
	}
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(contents), nil
}

func (tc *scenarioConfig) isMLFlow() bool {
	return os.Getenv("MLFLOW_TRACKING_URI") != ""
}

func (tc *scenarioConfig) substituteValues(body string) (string, error) {
	re := regexp.MustCompile(`\{\{([^}]*)\}\}`)
	for strings.Contains(body, "{{") {
		match := re.FindStringSubmatch(body)
		if len(match) > 1 {
			if strings.HasPrefix(match[1], mlflowPrefix) {
				// Use the literal after mlflow: as the experiment name. When MLflow is configured,
				// it could be resolved from MLflow; for tests without MLflow, this allows name-based
				// search to match stored jobs.
				v := strings.TrimPrefix(match[1], mlflowPrefix)
				body = strings.ReplaceAll(body, fmt.Sprintf("{{%s}}", match[1]), v)
			} else if strings.HasPrefix(match[1], envPrefix) {
				raw := strings.TrimPrefix(match[1], envPrefix)
				envName, fallback, hasFallback := strings.Cut(raw, "|")
				value, ok := os.LookupEnv(envName)
				if !ok {
					if hasFallback {
						value = fallback
					} else {
						value = ""
					}
				}
				body = strings.ReplaceAll(body, fmt.Sprintf("{{%s}}", match[1]), value)
			} else if strings.HasPrefix(match[1], valuePrefix) {
				n := strings.TrimPrefix(match[1], valuePrefix)
				v := tc.values[n]
				body = strings.ReplaceAll(body, fmt.Sprintf("{{%s}}", match[1]), v)
			} else {
				return "", tc.logError(fmt.Errorf("unknown substitutionvalue: %s", match[1]))
			}
		}
	}
	return body, nil
}

func (tc *scenarioConfig) getRequestBody(body string) (io.Reader, error) {
	var err error
	if body == "" {
		return nil, nil
	}
	// this can be an inline body or a test file
	if strings.HasPrefix(body, "file:/") {
		// this returns the contents of the file as a string
		body, err = tc.getFile(strings.TrimPrefix(body, "file:/"))
		if err != nil {
			return nil, err
		}
	}
	// now do any substitution
	body, err = tc.substituteValues(body)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(body), nil
}

func (tc *scenarioConfig) addAsset(assetName, id string) {
	tc.assets[assetName] = append(tc.assets[assetName], id)
	tc.logDebug("Added asset id %s for %s\n", id, assetName)
}

func (tc *scenarioConfig) removeAsset(assetName, id string) {
	ids := tc.assets[assetName]
	if slices.Contains(ids, id) {
		tc.assets[assetName] = slices.DeleteFunc(ids, func(s string) bool {
			if s == id {
				tc.logDebug("Removed asset id %s for %s\n", id, assetName)
				return true
			}
			return false
		})
	}
}

func (tc *scenarioConfig) extractId(body []byte) (string, error) {
	if len(body) > 0 {
		obj := make(map[string]interface{})
		err := json.Unmarshal(body, &obj)
		if err != nil {
			return "", tc.logError(fmt.Errorf("failed to unmarshal body %s: %w", string(body), err))
		}
		resource, ok := obj["resource"].(map[string]any)
		if !ok {
			return "", tc.logError(fmt.Errorf("response does not contain resource object: %s", string(body)))
		}
		id, ok := resource["id"].(string)
		if !ok || id == "" {
			return "", tc.logError(fmt.Errorf("response does not contain resource.id: %s", string(body)))
		}
		return id, nil
	}
	return "", nil
}

// pathDetails extracts the details from the path
// the first match is the asset name
// the second match is the asset type
// the third match is the asset id
// Handles: /api/v1/{name}, /api/v1/{name}/{asset}, /api/v1/{name}/{asset}/{id}
// Uses [^/?]+ to stop at query strings
var pathDetails = regexp.MustCompile(`^.*/api/v1/([^/?]+)(?:/([^/?]+))?(?:/([^/?]+))?.*$`)

func (tc *scenarioConfig) getAssetDetails(path string) (string, string, string, error) {
	if matches := pathDetails.FindStringSubmatch(path); len(matches) >= 4 {
		return matches[1], matches[2], matches[3], nil
	}
	return "", "", "", tc.logError(fmt.Errorf("no first path segment found in path %s", path))
}

func (tc *scenarioConfig) getId(id string) (string, error) {
	if strings.HasPrefix(id, valuePrefix) {
		n := strings.TrimPrefix(id, valuePrefix)
		v := tc.values[n]
		if v == "" {
			return "", tc.logError(fmt.Errorf("failed to find value %s", n))
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
					return "", tc.logError(fmt.Errorf("failed to substitute value: %s", err.Error()))
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
			return "", tc.logError(fmt.Errorf("last ID is not set"))
		}
		path = strings.Replace(path, "{id}", tc.lastId, 1)
	}

	endpoint := path
	if !strings.HasPrefix(endpoint, tc.apiFeature.baseURL.String()) {
		endpoint = fmt.Sprintf("%s%s", tc.apiFeature.baseURL.String(), path)
	}

	return endpoint, nil
}

func (tc *scenarioConfig) iSendARequestToWithInlineBody(method, path string, body *godog.DocString) error {
	if body == nil {
		return tc.logError(fmt.Errorf("inline body is missing"))
	}
	return tc.iSendARequestToWithBody(method, path, body.Content)
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
	tc.logDebug("Sending %s request to %s\n", method, endpoint)
	req, err := http.NewRequest(method, endpoint, entity)
	if err != nil {
		tc.logDebug("Failed to create request: %v\n", err)
		return err
	}
	if authToken := os.Getenv("AUTH_TOKEN"); authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	for k, v := range tc.reqHeaders {
		req.Header.Set(k, v)
	}

	tc.response, err = tc.apiFeature.client.Do(req)
	if err != nil {
		tc.logDebug("Failed to send request: %v\n", err)
		return err
	}

	defer func() {
		// we do this for now as request ids are supposed to be unique per request
		tc.iUnsetHeader(server.TRANSACTION_ID_HEADER)
	}()

	tc.body, err = io.ReadAll(tc.response.Body)
	if err != nil {
		return err
	}
	defer tc.response.Body.Close()

	if len(tc.body) > 0 && len(tc.body) < 1024*5 {
		tc.logDebug("Response status %d for %s %s with body %s\n", tc.response.StatusCode, method, endpoint, string(tc.body))
	} else {
		tc.logDebug("Response status %d for %s %s\n", tc.response.StatusCode, method, endpoint)
	}

	// capture resource id for create (evaluation job or collection)
	if method == http.MethodPost && (tc.response.StatusCode == http.StatusAccepted || tc.response.StatusCode == http.StatusCreated) {
		_, assetName, _, err := tc.getAssetDetails(endpoint)
		if err != nil {
			return err
		}
		if assetName != "" {
			tc.lastId, err = tc.extractId(tc.body)
			if err != nil {
				return err
			}
			if tc.lastId == "" {
				return tc.logError(fmt.Errorf("response does not contain an ID in response %s", string(tc.body)))
			}
			tc.addAsset(assetName, tc.lastId)
		}
	}

	if method == http.MethodDelete {
		_, assetName, _, err := tc.getAssetDetails(endpoint)
		if err != nil {
			return err
		}
		if assetName != "" {
			_, _, id, err := tc.getAssetDetails(endpoint)
			if err != nil {
				return err
			}
			if id == "" {
				return tc.logError(fmt.Errorf("no ID found in path %s", endpoint))
			}
			parsedURL, err := url.Parse(endpoint)
			if err != nil {
				return tc.logError(fmt.Errorf("failed to parse endpoint %s: %w", endpoint, err))
			}
			if parsedURL.Query().Get("hard_delete") == "true" {
				tc.removeAsset(assetName, id)
			}
		}
	}

	return nil
}

func (tc *scenarioConfig) theResponseStatusShouldBe(status int) error {
	if tc.response.StatusCode != status {
		return tc.logError(fmt.Errorf("expected status %d, got %d with response %s", status, tc.response.StatusCode, string(tc.body)))
	}
	return nil
}

func (tc *scenarioConfig) theResponseShouldContainWithValue(key, value string) error {
	var data map[string]interface{}
	if err := json.Unmarshal(tc.body, &data); err != nil {
		return tc.logError(err)
	}

	if data[key] != value {
		return tc.logError(fmt.Errorf("expected %s to be %s, got %v", key, value, data[key]))
	}

	return nil
}

func (tc *scenarioConfig) theResponseShouldContain(key string) error {
	var data map[string]interface{}
	if err := json.Unmarshal(tc.body, &data); err != nil {
		return tc.logError(err)
	}

	if _, ok := data[key]; !ok {
		return tc.logError(fmt.Errorf("response does not contain key: %s", key))
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
		return tc.logError(fmt.Errorf("the response %s does not match %s", asPrettyJson(actualResponse), expectedSchema))
	}
	if result.Valid() {
		return nil
	}
	return tc.logError(fmt.Errorf("failed to validate the response %s but no error detected when expecting %s", asPrettyJson(actualResponse), expectedSchema))
}

func (tc *scenarioConfig) theResponseShouldHaveSchemaAs(body *godog.DocString) error {
	return tc.compareJSONSchema(body.Content, string(tc.body))
}

func (tc *scenarioConfig) getJsonPath(jsonPath string) (string, error) {
	// first check the jsonpath is valid
	_, err := jsonpath.New(jsonPath)
	if err != nil {
		return "", fmt.Errorf("failed to validate JSON path %s: %w", jsonPath, err) // logging of the error is done by the caller
	}

	raw, err := tc.getJsonPathValue(jsonPath)
	if err != nil {
		return "", err
	}
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
		return "", fmt.Errorf("failed to get JSON path %s in %s: %w", jsonPath, string(tc.body), err) // logging of the error is done by the caller
	}
	return foundValue, nil
}

func (tc *scenarioConfig) theResponseShouldContainAtJSONPath(expectedValue string, jsonPath string) error {
	if strings.Contains(expectedValue, "{{") {
		expanded, err := tc.substituteValues(expectedValue)
		if err != nil {
			return err
		}
		expectedValue = expanded
	}
	foundValue, err := tc.getJsonPath(jsonPath)
	if err != nil {
		return tc.logError(err)
	}

	if strings.HasPrefix(expectedValue, "regex:") {
		rawExpr := strings.TrimPrefix(expectedValue, "regex:")
		expr, err := regexp.Compile(rawExpr)
		if err != nil {
			return tc.logError(fmt.Errorf("invalid regex %q: %w", rawExpr, err))
		}
		if expr.MatchString(foundValue) {
			tc.logDebug("Value %s matches regex %s in path %s", foundValue, rawExpr, jsonPath)
			return nil
		}
	}

	// make this contains and not equals
	// if foundValue == strings.TrimSpace(expectedValue) {
	values := strings.SplitSeq(expectedValue, "|")
	for value := range values {
		if strings.Contains(foundValue, value) {
			return nil
		}
	}

	return tc.logError(fmt.Errorf("expected %s to be %s but was %s in %s", jsonPath, expectedValue, foundValue, asPrettyJson(string(tc.body))))
}

func (tc *scenarioConfig) theResponseShouldNotContainAtJSONPath(expectedValue string, jsonPath string) error {
	if strings.Contains(expectedValue, "{{") {
		expanded, err := tc.substituteValues(expectedValue)
		if err != nil {
			return err
		}
		expectedValue = expanded
	}
	if tc.theResponseShouldContainAtJSONPath(expectedValue, jsonPath) == nil {
		return tc.logError(fmt.Errorf("expected %s to not contain %s but it did", jsonPath, expectedValue))
	}
	return nil
}

func (tc *scenarioConfig) theArrayAtPathInResponseShouldHaveLength(jsonPath string, lengthStr string) error {
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return tc.logError(fmt.Errorf("expected integer length, got %q: %w", lengthStr, err))
	}
	raw, err := tc.getJsonPathValue(jsonPath)
	if err != nil {
		return err
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return tc.logError(fmt.Errorf("value at path %s is not an array, got %T", jsonPath, raw))
	}
	if len(arr) != length {
		return tc.logError(fmt.Errorf("expected array at path %s to have length %d, got %d", jsonPath, length, len(arr)))
	}
	return nil
}

func (tc *scenarioConfig) theArrayAtPathInResponseShouldHaveLengthAtLeast(jsonPath string, minLengthStr string) error {
	minLength, err := strconv.Atoi(minLengthStr)
	if err != nil {
		return tc.logError(fmt.Errorf("expected integer min length, got %q: %w", minLengthStr, err))
	}
	raw, err := tc.getJsonPathValue(jsonPath)
	if err != nil {
		return err
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return tc.logError(fmt.Errorf("value at path %s is not an array, got %T", jsonPath, raw))
	}
	if len(arr) < minLength {
		return tc.logError(fmt.Errorf("expected array at path %s to have length >= %d, got %d", jsonPath, minLength, len(arr)))
	}
	return nil
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
		return tc.logError(fmt.Errorf("failed to parse JSON response: %w", err))
	}
	// This directly uses a JSON pointer path
	pathObj, err := jsonParsed.JSONPointer(getJsonPointer(path))
	if err != nil {
		return tc.logError(fmt.Errorf("path %v does not exist in \n%s", path, string(tc.body)))
	}
	finalResult, ok := pathObj.Data().(string)
	if !ok {
		return tc.logError(fmt.Errorf("expected %s to be a string but got %T", path, pathObj.Data()))
	}
	if strings.HasPrefix(name, valuePrefix) {
		tc.values[strings.TrimPrefix(name, valuePrefix)] = finalResult
	} else {
		return tc.logError(fmt.Errorf("unexpected value %s, should start with '%s'", name, valuePrefix))
	}
	return nil
}

func (tc *scenarioConfig) fixThisStep() error {
	tc.logDebug("TODO: fix this step")
	return godog.ErrSkip
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
		case "jobs":
			url = "evaluations/jobs"
		case "collections":
			url = "evaluations/collections"
		case "providers":
			url = "evaluations/providers"
		}
		ids := slices.Clone(ids)
		for _, id := range ids {
			path := fmt.Sprintf("/api/v1/%s/%s?hard_delete=true", url, id)
			err := tc.iSendARequestTo("DELETE", path)
			if err != nil {
				return ctx, tc.logError(fmt.Errorf("failed to delete asset %s with id '%s': %w", assetName, id, err))
			}
			err = tc.theResponseStatusShouldBe(204)
			if err != nil {
				return ctx, tc.logError(fmt.Errorf("failed to delete asset %s expected status %d but got %d: %w", tc.lastURL, 204, tc.response.StatusCode, err))
			}
			tc.logDebug("Deleted asset %s with status %d\n", path, tc.response.StatusCode)
		}
	}
	tc.assets = nil
	return ctx, nil
}

func createScenarioConfig(apiConfig *apiFeature) *scenarioConfig {
	conf := new(scenarioConfig)
	conf.reqHeaders = make(map[string]string)
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
			tc.logDebug("Error checking health endpoint: %v\n", err.Error())
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
	tc := createScenarioConfig(api)
	paths := [][]string{
		{"/api/v1/evaluations", "evaluations", "", ""},
		{"/api/v1/evaluations/jobs", "evaluations", "jobs", ""},
		{"/api/v1/evaluations/jobs/f02b16a2-1990-4626-b24d-1cff3febdbfb", "evaluations", "jobs", "f02b16a2-1990-4626-b24d-1cff3febdbfb"},
		{"/api/v1/evaluations/jobs/f02b16a2-1990-4626-b24d-1cff3febdbfb/update", "evaluations", "jobs", "f02b16a2-1990-4626-b24d-1cff3febdbfb"},
		{"/api/v1/evaluations/collections", "evaluations", "collections", ""},
		{"/api/v1/evaluations/collections/f02b16a2-1990-4626-b24d-1cff3febdbfb", "evaluations", "collections", "f02b16a2-1990-4626-b24d-1cff3febdbfb"},
		{"/api/v1/evaluations/providers", "evaluations", "providers", ""},
		{"/api/v1/evaluations/providers/f02b16a2-1990-4626-b24d-1cff3febdbfb", "evaluations", "providers", "f02b16a2-1990-4626-b24d-1cff3febdbfb"},
		{"http://localhost:8080/api/v1/evaluations", "evaluations", "", ""},
		{"http://localhost:8080/api/v1/evaluations/jobs", "evaluations", "jobs", ""},
		{"http://localhost:8080/api/v1/evaluations/jobs/f02b16a2-1990-4626-b24d-1cff3febdbfb", "evaluations", "jobs", "f02b16a2-1990-4626-b24d-1cff3febdbfb"},
		{"http://localhost:8080/api/v1/evaluations/jobs/f02b16a2-1990-4626-b24d-1cff3febdbfb/update", "evaluations", "jobs", "f02b16a2-1990-4626-b24d-1cff3febdbfb"},
		{"http://localhost:8080/api/v1/evaluations/collections", "evaluations", "collections", ""},
		{"http://localhost:8080/api/v1/evaluations/collections/f02b16a2-1990-4626-b24d-1cff3febdbfb", "evaluations", "collections", "f02b16a2-1990-4626-b24d-1cff3febdbfb"},
		{"http://localhost:8080/api/v1/evaluations/providers", "evaluations", "providers", ""},
		{"http://localhost:8080/api/v1/evaluations/providers/f02b16a2-1990-4626-b24d-1cff3febdbfb", "evaluations", "providers", "f02b16a2-1990-4626-b24d-1cff3febdbfb"},
		{"http://localhost:8080/api/v1/evaluations/providers?a=b", "evaluations", "providers", ""},
		{"http://localhost:8080/api/v1/evaluations/providers/f02b16a2-1990-4626-b24d-1cff3febdbfb?a=b", "evaluations", "providers", "f02b16a2-1990-4626-b24d-1cff3febdbfb"},
	}
	for _, path := range paths {
		name, asset, id, err := tc.getAssetDetails(path[0])
		if err != nil {
			panic(tc.logError(fmt.Errorf("failed to parse details from path %s: %v", path, err)))
		}
		if name != path[1] {
			panic(tc.logError(fmt.Errorf("expected asset name %s for path %s, got %s", path[1], path[0], name)))
		}
		if asset != path[2] {
			panic(tc.logError(fmt.Errorf("expected asset %s for path %s, got %s", path[2], path[0], asset)))
		}
		if id != path[3] {
			panic(tc.logError(fmt.Errorf("expected asset id %s for path %s, got %s", path[3], path[0], id)))
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
	ctx.Step(`^there are no evaluation jobs$`, tc.thereAreNoEvaluationJobs)
	ctx.Step(`^there are no user providers$`, tc.thereAreNoUserProviders)
	ctx.Step(`^I set the header "([^"]*)" to "([^"]*)"$`, tc.iSetHeaderTo)
	ctx.Step(`^I unset the header "([^"]*)"$`, tc.iUnsetHeader)
	ctx.Step(`^I set transaction-id to "([^"]*)"$`, tc.iSetTransactionIdTo)
	ctx.Step(`^I send a (GET|DELETE|POST|PUT) request to "([^"]*)"$`, tc.iSendARequestTo)
	ctx.Step(`^I send a (POST|PUT|PATCH) request to "([^"]*)" with body "([^"]*)"$`, tc.iSendARequestToWithBody)
	ctx.Step(`^I send a (POST|PUT|PATCH) request to "([^"]*)" with body:$`, tc.iSendARequestToWithInlineBody)
	ctx.Step(`^the response code should be (\d+)$`, tc.theResponseStatusShouldBe)
	ctx.Step(`^the response should contain "([^"]*)" with value "([^"]*)"$`, tc.theResponseShouldContainWithValue)
	ctx.Step(`^the response should contain "([^"]*)"$`, tc.theResponseShouldContain)
	ctx.Step(`^the response should be JSON$`, tc.theResponseShouldBeJSON)
	ctx.Step(`^the response should contain Prometheus metrics$`, tc.theResponseShouldContainPrometheusMetrics)
	ctx.Step(`^the metrics should include "([^"]*)"$`, tc.theMetricsShouldInclude)
	ctx.Step(`^the metrics should show request count for "([^"]*)"$`, tc.theMetricsShouldShowRequestCountFor)
	// Responses
	ctx.Step(`^the response should have schema as:$`, tc.theResponseShouldHaveSchemaAs)
	ctx.Step(`^the "([^"]*)" field in the response should be saved as "([^"]*)"$`, tc.theFieldShouldBeSaved)
	ctx.Step(`^the response should contain the value "([^"]*)" at path "([^"]*)"$`, tc.theResponseShouldContainAtJSONPath)
	ctx.Step(`^the response should not contain the value "([^"]*)" at path "([^"]*)"$`, tc.theResponseShouldNotContainAtJSONPath)
	ctx.Step(`^the array at path "([^"]*)" in the response should have length (\d+)$`, tc.theArrayAtPathInResponseShouldHaveLength)
	ctx.Step(`^the array at path "([^"]*)" in the response should have length at least (\d+)$`, tc.theArrayAtPathInResponseShouldHaveLengthAtLeast)
	ctx.Step(`^I wait for the evaluation job status to be "([^"]*)"$`, tc.iWaitForEvaluationJobStatus)
	ctx.Step(`^the mode is local or CI then skip this scenario$`, tc.whenTheModeIsLocalOrCIThenSkipThisScenario)
	// Other steps
	ctx.Step(`^fix this step$`, tc.fixThisStep)
}
