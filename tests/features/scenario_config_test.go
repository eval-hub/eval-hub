package features

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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

	"github.com/eval-hub/eval-hub/internal/eval_hub/server"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"

	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	valuePrefix  = "value:"
	mlflowPrefix = "mlflow:"
	envPrefix    = "env:"
	regexpPrefix = "regex:"

	envMetricsURL        = "METRICS_URL"
	envMlflowTrackingURI = "MLFLOW_TRACKING_URI"
)

// modelEndpointStatus captures preflight outcome from checkModelEndpoint for steps that gate on connectivity.
type modelEndpointStatus int

const (
	modelEndpointUnchecked modelEndpointStatus = iota
	modelEndpointUnreachable
	modelEndpointReachable
)

// ociConfigStatus captures OCI configuration presence for skipping @oci scenarios
type ociConfigStatus int

const (
	ociConfigUnchecked ociConfigStatus = iota
	ociConfigMissing
	ociConfigPresent
)

var (
	// testConfig to be used throughout all the test suites
	// for the global configuration
	apiFeat *apiFeature

	once   sync.Once
	logger *log.Logger

	modelEndpointConnectivity modelEndpointStatus
	ociConfiguration          ociConfigStatus
)

type apiFeature struct {
	baseURL        *url.URL
	metricsBaseURL *url.URL
	server         *server.Server
	httpServer     *http.Server
	metricsServer  *server.MetricsServer
	client         *http.Client
	// MCP-specific fields
	mcpServer        *mcp.Server
	mcpClientSession *mcp.ClientSession
	mcpServerSession *mcp.ServerSession
}

// this is used for a scenario to ensure that scenarios do not overwrite
// data from other scenarios...
type scenarioConfig struct {
	scenarioName string
	apiFeature   *apiFeature
	response     *http.Response
	body         []byte

	reqHeaders map[string]string

	lastURL    string
	lastMethod string
	lastId     string

	// MCP-specific fields
	mcpToolResult    *mcp.CallToolResult
	mcpError         error
	mcpResourceText  string
	mcpResourceError error
	mcpPromptResult  *mcp.GetPromptResult
	mcpPromptError   error

	// assetsSync sync.Mutex
	assets map[string][]string

	values map[string]string

	// jsonnetHarnessEnv overrides process env in the jsonnet harness only (see jsonnetHarnessJSON).
	jsonnetHarnessEnv map[string]string
	// jsonnetHarnessEnvOmit drops keys from the harness env snapshot even when set in the process.
	jsonnetHarnessEnvOmit []string
	// jsonnetMlflowEnabled overrides harness.mlflow_enabled when non-nil.
	jsonnetMlflowEnabled *bool
	// jsonnetQueueEnabled overrides harness.queue_enabled when non-nil.
	jsonnetQueueEnabled *bool

	waitDeadline time.Duration
	waitInterval time.Duration

	// MLflow artifact fetching
	mlflowArtifactBody  []byte
	mlflowArtifactError error

	// OCI manifest fetching
	ociManifestBody       []byte
	ociManifestError      error
	ociManifestStatusCode int
	ociManifestData       map[string]interface{} // Parsed manifest JSON

	// OCI artifact fetching (the actual EvalCard JSON blob)
	ociArtifactBody  []byte
	ociArtifactError error

	// Shared HTTP client for OCI operations
	ociHTTPClient *http.Client
	// Cached Bearer tokens keyed by repository (avoids redundant token exchanges)
	ociBearerTokens map[string]string
}

func getLogger() *log.Logger {
	once.Do(func() {
		if logger == nil {
			path := filepath.Join("bin", "tests.log")
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

// getMLflowTLSConfig returns TLS config for MLflow clients, respecting MLFLOW_INSECURE env var
func getMLflowTLSConfig() *tls.Config {
	insecure := true // default to true for backward compatibility with cluster internal certs
	if insecureStr := os.Getenv("MLFLOW_INSECURE"); insecureStr != "" {
		if parsed, err := strconv.ParseBool(insecureStr); err == nil {
			insecure = parsed
		}
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecure, //nolint:gosec
	}
}

// getMLflowHTTPClient returns an HTTP client configured for MLflow API calls
func getMLflowHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: getMLflowTLSConfig(),
		},
	}
}

// mlflowBaseURL returns the MLflow base URL from the MLFLOW_TRACKING_URI env var.
// It fails when MLFLOW_TRACKING_URI is unset or empty after trailing-slash trimming.
func mlflowBaseURL() (string, error) {
	baseURL := strings.TrimRight(os.Getenv(envMlflowTrackingURI), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%s environment variable is required", envMlflowTrackingURI)
	}
	return baseURL, nil
}

// mlflowWorkspace resolves the MLflow workspace using X_TENANT env → X-Tenant header → "tenant" fallback
func (tc *scenarioConfig) mlflowWorkspace() string {
	workspace := os.Getenv("X_TENANT")
	if workspace == "" {
		if tenant, ok := tc.reqHeaders["X-Tenant"]; ok && tenant != "" {
			workspace = tenant
		} else {
			workspace = "tenant" // fallback
		}
	}
	return workspace
}

func (tc *scenarioConfig) mlflowClient() (*mlflowclient.Client, error) {
	baseURL, err := mlflowBaseURL()
	if err != nil {
		return nil, err
	}
	client := mlflowclient.NewClient(baseURL).
		WithContext(context.Background()).
		WithHTTPClient(getMLflowHTTPClient()).
		WithToken(os.Getenv("AUTH_TOKEN")).
		WithWorkspacesSupport(true).
		WithWorkspace(tc.mlflowWorkspace())
	return client, nil
}

func isMetricsScrapePath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "/metrics" {
		return true
	}
	u, err := url.Parse(path)
	return err == nil && u.Path == "/metrics"
}

func joinBaseURL(base *url.URL, path string) string {
	baseStr := strings.TrimRight(base.String(), "/")
	if strings.HasPrefix(path, baseStr) {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseStr + path
}

// resolveMetricsBaseURL returns the base URL for Prometheus scrape requests.
func resolveMetricsBaseURL(apiBase *url.URL) (*url.URL, error) {
	// METRICS_URL is set (local or remote/cluster).
	// Input: METRICS_URL=http://evalhub-metrics.<ns>.svc:8081 (or any valid scrape base).
	// Behavior: parse and return it; used when the pipeline targets the dedicated metrics port directly.
	if raw := strings.TrimSpace(os.Getenv(envMetricsURL)); raw != "" {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid METRICS_URL: %w", err)
		}
		checkBaseURL(u, raw)
		return u, nil
	}

	// Remote/cluster mode without METRICS_URL.
	// Input: SERVER_URL=https://evalhub.example.com, METRICS_URL unset.
	// Behavior: return nil so callers can skip @metrics scenarios or error if /metrics is requested
	// (metrics are not on the kube-rbac-proxy route; the pipeline must set METRICS_URL explicitly).
	if strings.TrimSpace(os.Getenv("SERVER_URL")) != "" {
		return nil, nil
	}

	// Local embedded-server mode (SERVER_URL unset, METRICS_URL unset).
	// Input: apiBase=http://localhost:8080 (or PORT); local mode serves /metrics on the main router.
	// Behavior: default scrape base to the API base URL.
	return apiBase, nil
}

// scenarioHasTag reports whether tag appears in tags, with or without a leading "@".
// Tags are plain strings so callers (and unit tests) stay independent of cucumber
// messages package versions that godog.Scenario aliases across.
func scenarioHasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.TrimPrefix(t, "@") == tag {
			return true
		}
	}
	return false
}

func scenarioTagNames(sc *godog.Scenario) []string {
	if sc == nil || len(sc.Tags) == 0 {
		return nil
	}
	names := make([]string, len(sc.Tags))
	for i, t := range sc.Tags {
		names[i] = t.Name
	}
	return names
}

func (tc *scenarioConfig) logDebug(format string, a ...any) {
	if v, exists := tc.reqHeaders[server.TransactionIDHeader]; exists && v != "" {
		format = fmt.Sprintf("(%s) %s", v, format)
	}
	fmt.Printf(format, a...)
	getLogger().Printf(format, a...)
}

func (tc *scenarioConfig) logError(err error, withStack ...bool) error {
	var sb = strings.Builder{}
	sb.WriteString("Error")
	if reqId, exists := tc.reqHeaders[server.TransactionIDHeader]; exists && reqId != "" {
		fmt.Fprintf(&sb, " (%s)", reqId)
	}
	sb.WriteString(": ")
	if len(withStack) > 0 && withStack[0] {
		getLogger().Printf("%s%v\n%s\n", sb.String(), err, string(debug.Stack()))
	} else {
		getLogger().Printf("%s%v\n", sb.String(), err)
	}
	return fmt.Errorf("%s%v", sb.String(), err)
}

func (tc *scenarioConfig) saveValue(name, value string) {
	tc.values[name] = value
	tc.logDebug("Saved value %s: %s\n", name, value)
}

var errTestFileNotFound = errors.New("test file not found")

// fvtDisconnected reports whether FVT is targeting a disconnected cluster
// (ENVIRONMENT_ID contains "disconnected").
func fvtDisconnected() bool {
	return strings.Contains(strings.ToLower(os.Getenv("ENVIRONMENT_ID")), "disconnected")
}

// fvtBenchmarkTokenizer returns the expected benchmark tokenizer for FVT assertions and payloads.
func fvtBenchmarkTokenizer() string {
	if fvtDisconnected() {
		return "/test_data/tokenizer"
	}
	return "google/flan-t5-small"
}

func (tc *scenarioConfig) findFile(fileName string) (string, error) {
	file := filepath.Join(testDataRoot(), fileName)
	_, err := os.Stat(file)
	if err == nil {
		return file, nil
	}
	if os.IsNotExist(err) {
		return "", errTestFileNotFound
	}
	return "", tc.logError(fmt.Errorf("stat test file %s: %w", fileName, err))
}

func (tc *scenarioConfig) getFile(fileName string) (string, error) {
	if jsonnetPath, err := tc.findFile(tc.jsonnetSiblingName(fileName)); err == nil {
		return tc.evaluateJsonnetFile(jsonnetPath)
	} else if !errors.Is(err, errTestFileNotFound) {
		return "", err
	}
	filePath, err := tc.findFile(fileName)
	if errors.Is(err, errTestFileNotFound) {
		path, _ := os.Getwd()
		return "", tc.logError(fmt.Errorf("test file %s not found in directory %s", fileName, path))
	}
	if err != nil {
		return "", err
	}
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(contents), nil
}

func (tc *scenarioConfig) substituteValues(body string) (string, error) {
	re := regexp.MustCompile(`\{\{([^}]*)\}\}`)
	for strings.Contains(body, "{{") {
		match := re.FindStringSubmatch(body)
		if len(match) <= 1 {
			return "", tc.logError(fmt.Errorf("unterminated substitution token in body: %s", body))
		}
		{
			if after, ok := strings.CutPrefix(match[1], mlflowPrefix); ok {
				// Use the literal after mlflow: as the experiment name. When MLflow is configured,
				// it could be resolved from MLflow; for tests without MLflow, this allows name-based
				// search to match stored jobs.
				experimentName := after
				if os.Getenv("MLFLOW_TRACKING_URI") == "" {
					experimentName = ""
				}
				tc.logDebug("Substituting value '%s' with '%s'\n", match[1], experimentName)
				body = strings.ReplaceAll(body, fmt.Sprintf("{{%s}}", match[1]), experimentName)
			} else if raw, ok := strings.CutPrefix(match[1], envPrefix); ok {
				envName, fallback, hasFallback := strings.Cut(raw, "|")
				var value string
				if v, found := gpuTestSuiteSubstValue(envName); found {
					value = v
				} else if envName == "FVT_BENCHMARK_TOKENIZER" {
					value = fvtBenchmarkTokenizer()
				} else if envValue, envOk := os.LookupEnv(envName); envOk {
					value = envValue
				} else if hasFallback {
					value = fallback
				} else {
					value = ""
				}
				tc.logDebug("Substituting value '%s' with '%s'\n", match[1], value)
				body = strings.ReplaceAll(body, fmt.Sprintf("{{%s}}", match[1]), value)
			} else if after1, ok := strings.CutPrefix(match[1], valuePrefix); ok {
				n := after1
				v := tc.values[n]
				tc.logDebug("Substituting value '%s' with '%s'\n", match[1], v)
				body = strings.ReplaceAll(body, fmt.Sprintf("{{%s}}", match[1]), v)
			} else {
				return "", tc.logError(fmt.Errorf("unknown substitution value: %s", match[1]))
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
	//tc.assetsSync.Lock()
	//defer tc.assetsSync.Unlock()
	tc.assets[assetName] = append(tc.assets[assetName], id)
	tc.logDebug("Added asset id %s for %s\n", id, assetName)
}

func (tc *scenarioConfig) removeAsset(assetName, id string) {
	//tc.assetsSync.Lock()
	//defer tc.assetsSync.Unlock()
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

var valueExpression = regexp.MustCompile(`^(.*)[\s]*([+-])[\s]*(\d+)$`)

func (tc *scenarioConfig) getValueExpression(id string) (string, int, error) {
	matches := valueExpression.FindStringSubmatch(id)
	if len(matches) >= 4 {
		v, err := strconv.Atoi(matches[3])
		if err != nil {
			return "", 0, err
		}
		if matches[2] == "+" {
			return strings.TrimRight(matches[1], " "), v, nil
		}
		return strings.TrimRight(matches[1], " "), -v, nil
	}
	return id, 0, nil
}

func (tc *scenarioConfig) getValue(id string) (string, error) {
	// start with the full substitution
	if value, err := tc.substituteValues(id); err == nil {
		id = value
	}
	// Handle {variable} pattern by looking up in values map
	if strings.HasPrefix(id, "{") && strings.HasSuffix(id, "}") {
		n := strings.TrimSuffix(strings.TrimPrefix(id, "{"), "}")
		v := tc.values[n]
		if v == "" {
			return "", tc.logError(fmt.Errorf("failed to find value for {%s}", n))
		}
		return v, nil
	}
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
				v, err := tc.getValue(match[1])
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

	if isMetricsScrapePath(path) {
		if tc.apiFeature.metricsBaseURL == nil {
			return "", tc.logError(fmt.Errorf(
				"METRICS_URL is required when SERVER_URL is set (metrics are served on a separate port, not through kube-rbac-proxy)",
			))
		}
		return joinBaseURL(tc.apiFeature.metricsBaseURL, path), nil
	}

	endpoint := path
	if !strings.HasPrefix(endpoint, tc.apiFeature.baseURL.String()) {
		endpoint = joinBaseURL(tc.apiFeature.baseURL, path)
	}

	return endpoint, nil
}

func createScenarioConfig(apiConfig *apiFeature) *scenarioConfig {
	conf := new(scenarioConfig)
	conf.reqHeaders = make(map[string]string)
	conf.assets = make(map[string][]string)
	conf.values = make(map[string]string)
	conf.apiFeature = apiConfig

	conf.waitDeadline = 30 * time.Minute
	conf.waitInterval = 1 * time.Minute

	return conf
}
