package features

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/internal/eval_hub/mlflow"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes"
	"github.com/eval-hub/eval-hub/internal/eval_hub/server"
	"github.com/eval-hub/eval-hub/internal/eval_hub/storage"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/otel"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	pkgapi "github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"

	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpserver "github.com/eval-hub/eval-hub/internal/evalhub_mcp/server"
)

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
		metricsBase, err := resolveMetricsBaseURL(uri)
		if err != nil {
			return nil, logError(err)
		}
		apiFeat := &apiFeature{client: client, baseURL: uri, metricsBaseURL: metricsBase}

		// Initialize MCP server even when using remote server
		logger, _, err := logging.NewLogger()
		if err != nil {
			return nil, logError(fmt.Errorf("failed to create logger for MCP: %w", err))
		}
		if err := apiFeat.setupMCPServer(logger); err != nil {
			return nil, logError(fmt.Errorf("failed to setup MCP server for remote testing: %w", err))
		}

		return apiFeat, nil
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

	metricsBase, err := resolveMetricsBaseURL(baseURL)
	if err != nil {
		return nil, logError(err)
	}

	apiFeat := &apiFeature{
		client:         client,
		baseURL:        baseURL,
		metricsBaseURL: metricsBase,
	}
	if err := apiFeat.startLocalServer(port); err != nil {
		return nil, err
	}
	return apiFeat, nil
}

// ensureFVTOTELConfig enables OTEL metrics export for embedded FVT servers when Prometheus
// scraping is configured. HTTP request duration is collected by otelhttp.
func ensureFVTOTELConfig(serviceConfig *config.Config) {
	if serviceConfig == nil || !serviceConfig.IsPrometheusEnabled() {
		return
	}
	if serviceConfig.OTEL == nil {
		serviceConfig.OTEL = &config.OTELConfig{}
	}
	serviceConfig.OTEL.Enabled = true
	serviceConfig.OTEL.EnableMetrics = true
	serviceConfig.OTEL.ExporterType = otel.ExporterTypeStdout
}

func (a *apiFeature) startLocalServer(port int) error {
	logger, _, err := logging.NewLogger()
	if err != nil {
		return err
	}
	validate, err := validation.NewValidator()
	if err != nil {
		return logError(err)
	}
	version, err := testhelpers.RepoVersion()
	if err != nil {
		return logError(err)
	}
	serviceConfig, err := config.LoadConfig(logger, version, "local", time.Now().Format(time.RFC3339), "")
	if err != nil {
		return logError(fmt.Errorf("failed to load service config: %w", err))
	}
	serviceConfig.Service.Port = port
	serviceConfig.Service.LocalMode = true // set local mode for testing

	// set up the provider configs
	providerConfigs, err := config.LoadProviderConfigs(logger, validate)
	if err != nil {
		// we do this as no point trying to continue
		return logError(fmt.Errorf("failed to load provider configs: %w", err))
	}

	if len(providerConfigs) == 0 {
		return logError(fmt.Errorf("no provider configs loaded"))
	}

	logger.Info("Providers loaded.")
	for key := range providerConfigs {
		providerCfg := providerConfigs[key]
		if providerCfg.Runtime == nil {
			return logError(fmt.Errorf("provider %q has no runtime configuration", providerCfg.Resource.ID))
		}
		if providerCfg.Runtime.Local == nil {
			providerCfg.Runtime.Local = &pkgapi.LocalRuntime{}
		}
		providerConfigs[key] = providerCfg
	}

	// set up the collection configs
	collectionConfigs, err := config.LoadCollectionConfigs(logger, validate)
	if err != nil {
		return logError(fmt.Errorf("failed to load collection configs: %w", err))
	}

	ensureFVTOTELConfig(serviceConfig)
	if serviceConfig.IsOTELEnabled() {
		if _, err := otel.SetupOTEL(context.Background(), serviceConfig.OTEL, logger, serviceConfig.IsPrometheusEnabled()); err != nil {
			return logError(fmt.Errorf("failed to setup OTEL: %w", err))
		}
	}
	if serviceConfig.IsOTELMetricsEnabled() {
		if err := metrics.Init(); err != nil {
			return logError(fmt.Errorf("failed to initialize OTEL metrics: %w", err))
		}
	}

	storage, err := storage.NewStorage(
		serviceConfig.Database,
		collectionConfigs,
		providerConfigs,
		serviceConfig.IsOTELStorageScansEnabled(),
		serviceConfig.IsOTELMetricsEnabled(),
		logger,
	)
	if err != nil {
		return logError(fmt.Errorf("failed to create storage: %w", err))
	}
	logger.Info("Storage created.")

	runtime, err := runtimes.NewRuntime(logger, serviceConfig)
	if err != nil {
		return logError(fmt.Errorf("failed to create runtime: %w", err))
	}

	mlflowClient, err := mlflow.NewMLFlowClient(serviceConfig, logger)
	if err != nil {
		return logError(fmt.Errorf("failed to create MLFlow client: %w", err))
	}

	a.server, err = server.NewServer(logger,
		serviceConfig,
		storage,
		validate,
		runtime,
		mlflowClient)
	if err != nil {
		return err
	}

	// Create a test server
	handler, err := a.server.SetupRoutes()
	if err != nil {
		return err
	}
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
		_ = a.httpServer.Serve(listener)
	}()

	if serviceConfig.IsPrometheusEnabled() {
		a.metricsServer = server.NewMetricsServer(logger, serviceConfig.Prometheus)
		go func() {
			if err := a.metricsServer.Start(); err != nil {
				logger.Error("Metrics server failed", "error", err.Error())
			}
		}()
	}

	// Initialize MCP server with real eval-hub client
	if err := a.setupMCPServer(logger); err != nil {
		return logError(fmt.Errorf("failed to setup MCP server: %w", err))
	}

	return nil
}

func (a *apiFeature) setupMCPServer(logger *slog.Logger) error {
	// Create real eval-hub client pointing to local test server
	tenant := os.Getenv("X_TENANT")
	token := os.Getenv("AUTH_TOKEN")

	logger.Info("Setting up MCP server", "base_url", a.baseURL.String(), "tenant", tenant, "has_token", token != "")

	evalhubClient := evalhubclient.NewClient(a.baseURL.String())
	if tenant != "" {
		evalhubClient = evalhubClient.WithTenant(tenant)
	}
	if token != "" {
		evalhubClient = evalhubClient.WithToken(token)
	}

	// Create MCP server with real backend
	version, err := testhelpers.RepoVersion()
	if err != nil {
		version = "unknown" // Fallback for test environment
	}

	// Get git hash from repo (matches Makefile: git rev-parse --short HEAD)
	gitHash := "unknown"
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	if output, err := cmd.Output(); err == nil {
		gitHash = strings.TrimSpace(string(output))
	}

	serverInfo := &mcpserver.ServerInfo{
		Build:     version,
		BuildDate: time.Now().Format(time.RFC3339),
		GitHash:   gitHash,
	}

	a.mcpServer = mcpserver.New(serverInfo, logger, nil)
	if err := mcpserver.RegisterHandlers(a.mcpServer, evalhubClient, serverInfo, logger, evalhubclient.DefaultListPageLimit); err != nil {
		return fmt.Errorf("failed to register MCP handlers: %w", err)
	}

	// Create in-memory transports for testing (like unit tests)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()

	// Connect server
	serverSession, err := a.mcpServer.Connect(ctx, serverTransport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect MCP server: %w", err)
	}
	a.mcpServerSession = serverSession

	// Connect client
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "fvt-test-client", Version: "1.0.X"}, nil) // this is just a test so it can be anything
	clientSession, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect MCP client: %w", err)
	}
	a.mcpClientSession = clientSession

	logger.Info("MCP server initialized successfully for FVT tests")
	return nil
}

func (a *apiFeature) cleanup(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
	if a.mcpClientSession != nil {
		if err := a.mcpClientSession.Close(); err != nil {
			// Log but don't fail - consistent with existing cleanup pattern
			logDebug("MCP client session close error (non-fatal): %v\n", err)
		}
	}
	if a.mcpServerSession != nil {
		if err := a.mcpServerSession.Close(); err != nil {
			// Log but don't fail - consistent with existing cleanup pattern
			logDebug("MCP server session close error (non-fatal): %v\n", err)
		}
	}
	if a.metricsServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = a.metricsServer.Shutdown(shutdownCtx)
		cancel()
	}
	if a.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = a.httpServer.Shutdown(shutdownCtx)
	}

	return ctx, nil
}

func setUpTestConf() {
	apiFeature, err := createApiFeature()
	if err != nil {
		panic(logError(fmt.Errorf("failed to create API feature: %v", err)))
	}
	apiFeat = apiFeature
}

func waitForService() {
	tc := createScenarioConfig(apiFeat)
	if err := tc.theServiceIsRunning(context.Background()); err != nil {
		panic("Stopped API Tests. Service is not ready for testing.\n")
	}
}

func tidyUpTests() {
	if apiFeat != nil {
		_, _ = apiFeat.cleanup(context.Background(), nil, nil)
	}
	if s, ok := getLogger().Writer().(*os.File); ok {
		err := s.Close()
		if err != nil {
			panic(fmt.Sprintf("Failed to close logger file: %v\n", err))
		}
	}
}

func checkModelEndpoint() {
	modelEndpointConnectivity = modelEndpointUnchecked

	modelURL := os.Getenv("MODEL_URL")
	if modelURL == "" {
		logDebug("MODEL_URL not set, skipping model endpoint pre-flight check\n")
		return
	}

	fmt.Printf("Checking model endpoint connectivity: %s\n", modelURL)

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true, //nolint:gosec
			},
		},
	}

	maxRetries := 3
	numRetries := 0
	shouldRetry := func() bool { return numRetries < maxRetries }
	notReadyStatus := func(statusCode int) bool { return statusCode == 503 }
	retryDelay := 10 * time.Second

	for shouldRetry() {
		resp, err := client.Get(modelURL) //nolint:gosec // This is a test, we don't need to be too strict about the HTTP client
		if err != nil {
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) {
				logDebug("WARNING: Cannot resolve model endpoint DNS for %s (test runner may be outside the cluster), proceeding with tests\n", modelURL)
				return
			}
			logDebug("WARNING: Model endpoint %s is not reachable: %v\n", modelURL, err)
			logDebug("Evaluation job scenarios will be skipped.\n")
			modelEndpointConnectivity = modelEndpointUnreachable
			return
		}
		status := resp.StatusCode
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		logDebug("Model endpoint preflight GET %s returned HTTP %d\n", modelURL, status)

		// 401 on the OpenAI-compatible base URL (/v1) still means the server is up;
		// unauthenticated GET often cannot list models. Do not treat 404 as reachable
		// (that can mask a bad MODEL_URL).
		reachableWithoutAuth := status == http.StatusUnauthorized

		if numRetries < maxRetries-1 && notReadyStatus(status) {
			logDebug("WARNING: Model endpoint %s is not ready (HTTP %d), waiting %s before retrying\n", modelURL, status, retryDelay)
			time.Sleep(retryDelay)
		} else if status >= 200 && status < 300 {
			logDebug("Model endpoint %s is reachable (HTTP %d)\n", modelURL, status)
			modelEndpointConnectivity = modelEndpointReachable
			return
		} else if reachableWithoutAuth {
			logDebug("Model endpoint %s is reachable (HTTP %d; auth expected for /v1 base URL)\n", modelURL, status)
			modelEndpointConnectivity = modelEndpointReachable
			return
		} else {
			logDebug("WARNING: Model endpoint %s returned HTTP %d, treating as unreachable\n", modelURL, status)
			logDebug("Evaluation job scenarios will be skipped.\n")
			modelEndpointConnectivity = modelEndpointUnreachable
			return
		}
		numRetries++
	}
}

// A bit of a hack to have some checks that the regexes are working as expected
func checkRegexes() {
	tc := createScenarioConfig(apiFeat)
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

	values := [][]string{
		{"{{value:num_providers}}+2", "{{value:num_providers}}", "2"},
		{"{{value:num_providers}} + 2", "{{value:num_providers}}", "2"},
		{"{{value:num_providers}}-2", "{{value:num_providers}}", "-2"},
		{"{{value:num_providers}} - 2", "{{value:num_providers}}", "-2"},
	}
	for _, value := range values {
		v, count, err := tc.getValueExpression(value[0])
		if err != nil {
			panic(tc.logError(fmt.Errorf("failed to parse value expression %s: %v", value[0], err)))
		}
		if v != value[1] {
			panic(tc.logError(fmt.Errorf("expected value '%s' for value expression '%s', got '%s'", value[1], value[0], v)))
		}
		if fmt.Sprintf("%d", count) != value[2] {
			panic(tc.logError(fmt.Errorf("expected count %s for value expression %s, got %d", value[1], value[0], count)))
		}
	}
}

func InitializeTestSuite(ctx *godog.TestSuiteContext) {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
		//nolint:gosec
		InsecureSkipVerify: true,
	}

	if authToken := os.Getenv("AUTH_TOKEN"); authToken != "" {
		logDebug("Using Authorization header with token\n")
	}
	if tenant := os.Getenv("X_TENANT"); tenant != "" {
		logDebug("Using X-Tenant header with value %s\n", tenant)
	}
	if metricsURL := os.Getenv(envMetricsURL); metricsURL != "" {
		logDebug("Using METRICS_URL for Prometheus scrape requests: %s\n", metricsURL)
	}

	ctx.BeforeSuite(checkRegexes)

	ctx.BeforeSuite(setUpTestConf)
	ctx.BeforeSuite(waitForService)
	ctx.BeforeSuite(checkModelEndpoint)

	// Initialize GPU test suite hooks
	InitializeGPUTestSuite(ctx)

	ctx.AfterSuite(tidyUpTests)
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	tc := createScenarioConfig(apiFeat)

	ctx.Before(tc.saveScenarioName)
	ctx.Before(tc.requireMetricsURLForRemoteServer)
	ctx.After(tc.assetCleanup)

	ctx.Step(`^the service is running$`, tc.theServiceIsRunning)
	ctx.Step(`^queue is enabled for payloads$`, tc.queueIsEnabledForJsonnetPayloads)
	ctx.Step(`^the model endpoint is reachable$`, tc.theModelEndpointIsReachable)
	ctx.Step(`^there are system providers$`, tc.thereAreSystemProviders)
	ctx.Step(`^there are system collections$`, tc.thereAreSystemCollections)
	ctx.Step(`^there is a system collection with id "([^"]*)"$`, tc.thereIsASystemCollectionWithId)
	ctx.Step(`^the value "([^"]*)" is not empty$`, tc.theValueIsSet)
	ctx.Step(`^I set the header "([^"]*)" to "([^"]*)"$`, tc.iSetHeaderTo)
	ctx.Step(`^I unset the header "([^"]*)"$`, tc.iUnsetHeader)
	ctx.Step(`^I set transaction-id to "([^"]*)"$`, tc.iSetTransactionIdTo)
	ctx.Step(`^I send a (GET|DELETE|POST|PUT) request to "([^"]*)"$`, tc.iSendARequestTo)
	ctx.Step(`^I send a (POST|PUT|PATCH) request to "([^"]*)" with body "([^"]*)"$`, tc.iSendARequestToWithBody)
	ctx.Step(`^I send a (POST|PUT|PATCH) request to "([^"]*)" with body:$`, tc.iSendARequestToWithInlineBody)
	ctx.Step(`^the response code should be (\d+)$`, tc.theResponseStatusShouldBe)
	ctx.Step(`^the response code should be (\d+) or (\d+)$`, tc.theResponseStatusShouldBeOr)
	ctx.Step(`^the response content type should be "([^"]*)"$`, tc.theResponseContentTypeShouldBe)
	ctx.Step(`^the response body should contain "([^"]*)"$`, tc.theResponseBodyShouldContain)
	ctx.Step(`^the response should contain "([^"]*)" with value "([^"]*)"$`, tc.theResponseShouldContainWithValue)
	ctx.Step(`^the response should contain "([^"]*)"$`, tc.theResponseShouldContain)
	ctx.Step(`^the response should be JSON$`, tc.theResponseShouldBeJSON)
	ctx.Step(`^the response should contain Prometheus metrics$`, tc.theResponseShouldContainPrometheusMetrics)
	ctx.Step(`^the metrics should include "([^"]*)"$`, tc.theMetricsShouldInclude)
	ctx.Step(`^the metrics should show request count for "([^"]*)"$`, tc.theMetricsShouldShowRequestCountFor)
	// Responses
	ctx.Step(`^the response should have schema as:$`, tc.theResponseShouldHaveSchemaAs)
	ctx.Step(`^the response should have schema from file "([^"]*)"$`, tc.theResponseShouldHaveSchemaFromFile)
	ctx.Step(`^the "([^"]*)" field in the response should be saved as "([^"]*)"$`, tc.theFieldShouldBeSaved)
	ctx.Step(`^the response should contain the value "([^"]*)" at path "([^"]*)"$`, tc.theResponseShouldContainAtJSONPath)
	ctx.Step(`^the response should equal the value "([^"]*)" at path "([^"]*)"$`, tc.theResponseShouldEqualAtJSONPath)
	ctx.Step(`^the response should match the value "([^"]*)" at path "([^"]*)"$`, tc.theResponseShouldMatchAtJSONPath)
	ctx.Step(`^the response should contain at least the value "([^"]*)" at path "([^"]*)"$`, tc.theResponseShouldContainAtJSONPathAtLeast)
	ctx.Step(`^the response should not contain the value "([^"]*)" at path "([^"]*)"$`, tc.theResponseShouldNotContainAtJSONPath)
	ctx.Step(`^the response should not equal the value "([^"]*)" at path "([^"]*)"$`, tc.theResponseShouldNotEqualAtJSONPath)
	ctx.Step(`^the array at path "([^"]*)" in the response should have length (\d+)$`, tc.theArrayAtPathInResponseShouldHaveLength)
	ctx.Step(`^the array at path "([^"]*)" in the response should have length "([^"]*)"$`, tc.theArrayAtPathInResponseShouldHaveLength)
	ctx.Step(`^the array at path "([^"]*)" in the response should have length at least (\d+)$`, tc.theArrayAtPathInResponseShouldHaveLengthAtLeast)
	ctx.Step(`^the array at path "([^"]*)" in the response should have length at least "([^"]*)"$`, tc.theArrayAtPathInResponseShouldHaveLengthAtLeast)
	ctx.Step(`^I wait for the evaluation job status to be "([^"]*)"$`, tc.iWaitForEvaluationJobStatus)
	ctx.Step(`^I wait for the evaluation job "([^"]*)" status to be "([^"]*)"$`, tc.iWaitForEvaluationJobStatusByID)
	ctx.Step(`^I set the wait deadline to "([^"]*)"$`, tc.iSetWaitDeadlineTo)
	ctx.Step(`^I set the wait interval to "([^"]*)"$`, tc.iSetWaitIntervalTo)
	// Other steps
	ctx.Step(`^fix this step$`, tc.fixThisStep)

	// MCP-specific steps
	InitializeMCPSteps(ctx, tc)

	// MLflow artifact steps
	ctx.Step(`^I fetch the MLflow artifact "([^"]*)" for run "([^"]*)"$`, tc.iFetchMLflowArtifact)
	ctx.Step(`^I fetch the MLflow artifact "([^"]*)" for experiment "([^"]*)" and job "([^"]*)"$`, tc.iFetchMLflowArtifactByExperimentAndJob)
	ctx.Step(`^the MLflow artifact should exist$`, tc.theMLflowArtifactShouldExist)
	ctx.Step(`^the MLflow artifact "([^"]*)" should not exist for experiment "([^"]*)" and job "([^"]*)"$`, tc.theMLflowArtifactShouldNotExistForExperimentAndJob)
	ctx.Step(`^the MLflow artifact should contain "([^"]*)"$`, tc.theMLflowArtifactShouldContain)
	ctx.Step(`^the MLflow artifact should contain the value "([^"]*)" at path "([^"]*)"$`, tc.theMLflowArtifactShouldContainValueAtPath)
	ctx.Step(`^the MLflow artifact should be valid JSON$`, tc.theMLflowArtifactShouldBeValidJSON)
	ctx.Step(`^the MLflow artifact field "([^"]*)" should match ISO 8601 format$`, tc.theMLflowArtifactFieldShouldMatchISO8601)
	ctx.Step(`^I wait for the evaluation job status to match "([^"]*)"$`, tc.iWaitForEvaluationJobStatusToMatch)

	// GPU-specific steps
	InitializeGPUSteps(ctx, tc)

	// Hardware profile steps (Kubernetes client-go via KUBECONFIG-first FVT helper)
	InitializeHardwareProfileSteps(ctx, tc)

	// Kubernetes lifecycle signal steps (event emission and evaluation-phase label)
	InitializeLifecycleSignalSteps(ctx, tc)
}
