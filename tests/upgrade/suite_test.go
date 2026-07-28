package upgrade

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jeffail/gabs/v2"
	"github.com/PaesslerAG/jsonpath"
	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	pkgapi "github.com/eval-hub/eval-hub/pkg/api"
	"github.com/google/go-jsonnet"
	"github.com/spf13/pflag"
)

var opts = godog.Options{
	Output: colors.Colored(os.Stdout),
	Format: "pretty",
	Strict: true,
	Paths:  []string{"."},
}

var apiConfig *apiFeature

type apiFeature struct {
	baseURL *url.URL
	client  *http.Client
}

type scenarioState struct {
	api        *apiFeature
	reqHeaders map[string]string
	response   *http.Response
	body       []byte

	values map[string]string
	lastId string

	waitDeadline time.Duration
	waitInterval time.Duration

	loadedState *upgradeState
}

type upgradeState struct {
	CurrentJobID string       `json:"current_job_id,omitempty"`
	Jobs         []upgradeJob `json:"jobs"`
}

type upgradeJob struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type jsonnetHarness struct {
	Env    map[string]string `json:"env"`
	Values map[string]string `json:"values"`
}

var (
	templateTokenRe = regexp.MustCompile(`\{\{([^}]*)\}\}`)
	arrayIndexRe    = regexp.MustCompile(`\[(\d+)\]`)
)

func TestMain(m *testing.M) {
	godog.BindCommandLineFlags("godog.", &opts)
	pflag.Parse()
	if args := pflag.Args(); len(args) > 0 {
		opts.Paths = args
	}

	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, //nolint:gosec
	}

	serverURL := os.Getenv("SERVER_URL")
	if serverURL == "" {
		fmt.Fprintln(os.Stderr, "ERROR: SERVER_URL is not set")
		os.Exit(1)
	}
	uri, err := url.Parse(serverURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: invalid SERVER_URL: %v\n", err)
		os.Exit(1)
	}
	if uri.Scheme == "" || uri.Host == "" {
		fmt.Fprintf(os.Stderr, "ERROR: invalid SERVER_URL %q: must include scheme and host (e.g. https://host:port)\n", serverURL)
		os.Exit(1)
	}

	apiConfig = &apiFeature{
		baseURL: uri,
		client:  &http.Client{Timeout: 30 * time.Second},
	}

	suite := godog.TestSuite{
		Name:                "EvalHub Upgrade Tests",
		ScenarioInitializer: initializeScenario,
		Options:             &opts,
	}
	os.Exit(suite.Run())
}

func initializeScenario(ctx *godog.ScenarioContext) {
	s := &scenarioState{
		api:          apiConfig,
		reqHeaders:   make(map[string]string),
		values:       make(map[string]string),
		waitDeadline: 5 * time.Minute,
		waitInterval: 10 * time.Second,
	}

	ctx.Step(`^the service is running$`, s.theServiceIsRunning)
	ctx.Step(`^I set the header "([^"]*)" to "([^"]*)"$`, s.iSetHeaderTo)
	ctx.Step(`^I set the wait deadline to "([^"]*)"$`, s.iSetWaitDeadlineTo)
	ctx.Step(`^I set the wait interval to "([^"]*)"$`, s.iSetWaitIntervalTo)

	ctx.Step(`^I send a (GET|DELETE|POST|PUT) request to "([^"]*)"$`, s.iSendRequestTo)
	ctx.Step(`^I send a (POST|PUT|PATCH) request to "([^"]*)" with body "([^"]*)"$`, s.iSendRequestToWithBody)

	ctx.Step(`^the response code should be (\d+)$`, s.theResponseCodeShouldBe)
	ctx.Step(`^the response should contain the value "([^"]*)" at path "([^"]*)"$`, s.theResponseShouldContainAtJSONPath)

	ctx.Step(`^the "([^"]*)" field in the response should be saved as "([^"]*)"$`, s.theFieldShouldBeSaved)
	ctx.Step(`^I wait for the evaluation job status to be "([^"]*)"$`, s.iWaitForEvaluationJobStatus)
	ctx.Step(`^I collect all jobs and save upgrade state to "([^"]*)" expecting current job "([^"]*)" in "([^"]*)" state$`, s.iCollectAndSaveUpgradeState)
	ctx.Step(`^I load the upgrade state from "([^"]*)"$`, s.iLoadUpgradeState)
	ctx.Step(`^I hard-delete all evaluation jobs containing "([^"]*)" in its job name$`, s.iHardDeleteAllEvaluationJobsContaining)
	ctx.Step(`^I verify all jobs from upgrade state exist$`, s.iVerifyAllJobsFromUpgradeStateExist)
}

// ---------------------------------------------------------------------------
// Value resolution
// ---------------------------------------------------------------------------

const valuePrefix = "value:"

func (s *scenarioState) substituteValues(body string) (string, error) {
	for strings.Contains(body, "{{") {
		match := templateTokenRe.FindStringSubmatch(body)
		if len(match) <= 1 {
			break
		}
		token := match[1]
		if raw, ok := strings.CutPrefix(token, "env:"); ok {
			envName, fallback, hasFallback := strings.Cut(raw, "|")
			var value string
			if envValue, envOk := os.LookupEnv(envName); envOk {
				value = envValue
			} else if hasFallback {
				value = fallback
			}
			body = strings.ReplaceAll(body, fmt.Sprintf("{{%s}}", token), value)
		} else if name, ok := strings.CutPrefix(token, valuePrefix); ok {
			body = strings.ReplaceAll(body, fmt.Sprintf("{{%s}}", token), s.values[name])
		} else {
			return "", fmt.Errorf("unknown substitution: %s", token)
		}
	}
	return body, nil
}

func (s *scenarioState) getValue(id string) (string, error) {
	if value, err := s.substituteValues(id); err == nil {
		id = value
	}
	if strings.HasPrefix(id, "{") && strings.HasSuffix(id, "}") {
		n := strings.TrimSuffix(strings.TrimPrefix(id, "{"), "}")
		v := s.values[n]
		if v == "" {
			return "", fmt.Errorf("value {%s} not found", n)
		}
		return v, nil
	}
	if strings.HasPrefix(id, valuePrefix) {
		n := strings.TrimPrefix(id, valuePrefix)
		v := s.values[n]
		if v == "" {
			return "", fmt.Errorf("value %s not found", n)
		}
		return v, nil
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// Endpoint resolution
// ---------------------------------------------------------------------------

func (s *scenarioState) getEndpoint(path string) (string, error) {
	expanded, err := s.substituteValues(path)
	if err != nil {
		return "", err
	}
	path = expanded

	if strings.Contains(path, "{id}") {
		if s.lastId == "" {
			return "", fmt.Errorf("last ID is not set")
		}
		path = strings.Replace(path, "{id}", s.lastId, 1)
	}

	if !strings.HasPrefix(path, s.api.baseURL.String()) {
		baseStr := strings.TrimRight(s.api.baseURL.String(), "/")
		path = baseStr + path
	}
	return path, nil
}

// ---------------------------------------------------------------------------
// Request body handling (file + jsonnet)
// ---------------------------------------------------------------------------

var (
	testDataRoot     string
	testDataRootOnce sync.Once
	jsonnetLibDir    string
	jsonnetLibOnce   sync.Once
	envMap           map[string]string
	envMapOnce       sync.Once
	repoRootDir      string
	repoRootOnce     sync.Once
)

func upgradeTestDataRoot() string {
	testDataRootOnce.Do(func() {
		for _, dir := range []string{
			filepath.Join("tests", "upgrade", "test_data"),
			filepath.Join("test_data"),
		} {
			if _, err := os.Stat(dir); err == nil {
				testDataRoot = dir
				return
			}
		}
		testDataRoot = filepath.Join("tests", "upgrade", "test_data")
	})
	return testDataRoot
}

func fvtJsonnetLibDir() string {
	jsonnetLibOnce.Do(func() {
		for _, dir := range []string{
			filepath.Join("tests", "features", "test_data", "jsonnet"),
			filepath.Join("..", "features", "test_data", "jsonnet"),
		} {
			if _, err := os.Stat(dir); err == nil {
				jsonnetLibDir = dir
				return
			}
		}
		jsonnetLibDir = filepath.Join("tests", "features", "test_data", "jsonnet")
	})
	return jsonnetLibDir
}

func cachedEnvMap() map[string]string {
	envMapOnce.Do(func() {
		envMap = make(map[string]string)
		for _, kv := range os.Environ() {
			key, val, _ := strings.Cut(kv, "=")
			envMap[key] = val
		}
	})
	return envMap
}

func (s *scenarioState) jsonnetHarnessJSON() (string, error) {
	values := s.values
	if values == nil {
		values = map[string]string{}
	}
	harness := jsonnetHarness{
		Env:    cachedEnvMap(),
		Values: values,
	}
	encoded, err := json.Marshal(harness)
	if err != nil {
		return "", fmt.Errorf("encode jsonnet harness: %w", err)
	}
	return string(encoded), nil
}

func (s *scenarioState) evaluateJsonnetFile(path string) (string, error) {
	harnessJSON, err := s.jsonnetHarnessJSON()
	if err != nil {
		return "", err
	}
	vm := jsonnet.MakeVM()
	vm.Importer(&jsonnet.FileImporter{
		JPaths: []string{
			filepath.Dir(path),
			fvtJsonnetLibDir(),
		},
	})
	vm.ExtVar("harness", harnessJSON)
	output, err := vm.EvaluateFile(path)
	if err != nil {
		return "", fmt.Errorf("evaluate jsonnet file %s: %w", path, err)
	}
	return output, nil
}

func jsonnetSiblingName(fileName string) string {
	ext := filepath.Ext(fileName)
	if ext == "" {
		return fileName + ".jsonnet"
	}
	return strings.TrimSuffix(fileName, ext) + ".jsonnet"
}

func (s *scenarioState) getFile(fileName string) (string, error) {
	root := upgradeTestDataRoot()

	jsonnetName := jsonnetSiblingName(fileName)
	jsonnetPath := filepath.Join(root, jsonnetName)
	if _, err := os.Stat(jsonnetPath); err == nil {
		return s.evaluateJsonnetFile(jsonnetPath)
	}

	filePath := filepath.Join(root, fileName)
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("test file %s not found in %s: %w", fileName, root, err)
	}
	return string(contents), nil
}

func (s *scenarioState) getRequestBody(body string) (io.Reader, error) {
	if body == "" {
		return nil, nil
	}
	var err error
	if strings.HasPrefix(body, "file:/") {
		body, err = s.getFile(strings.TrimPrefix(body, "file:/"))
		if err != nil {
			return nil, err
		}
	}
	body, err = s.substituteValues(body)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(body), nil
}

// ---------------------------------------------------------------------------
// ID extraction
// ---------------------------------------------------------------------------

var pathDetails = regexp.MustCompile(`^.*/api/v1/([^/?]+)(?:/([^/?]+))?(?:/([^/?]+))?.*$`)

func (s *scenarioState) extractId(body []byte) (string, error) {
	if len(body) == 0 {
		return "", nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		return "", fmt.Errorf("failed to unmarshal body: %w", err)
	}
	resource, ok := obj["resource"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("response does not contain resource object: %s", string(body))
	}
	id, ok := resource["id"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("response does not contain resource.id: %s", string(body))
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// HTTP step definitions
// ---------------------------------------------------------------------------

func (s *scenarioState) theServiceIsRunning() error {
	endpoint := s.api.baseURL.String() + "/api/v1/health"
	for range 20 {
		req, _ := http.NewRequest("GET", endpoint, nil)
		for k, v := range s.reqHeaders {
			req.Header.Set(k, v)
		}
		resp, err := s.api.client.Do(req)
		if err == nil && resp.StatusCode == 200 {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("service is not running at %s", s.api.baseURL)
}

func (s *scenarioState) iSetHeaderTo(name, value string) error {
	resolved, err := s.getValue(value)
	if err != nil {
		return err
	}
	s.reqHeaders[name] = resolved
	return nil
}

func (s *scenarioState) iSendRequestTo(method, path string) error {
	return s.iSendRequestImpl(method, path, "")
}

func (s *scenarioState) iSendRequestToWithBody(method, path, body string) error {
	return s.iSendRequestImpl(method, path, body)
}

func (s *scenarioState) iSendRequestImpl(method, path, body string) error {
	endpoint, err := s.getEndpoint(path)
	if err != nil {
		return err
	}
	entity, err := s.getRequestBody(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(method, endpoint, entity)
	if err != nil {
		return err
	}

	for k, v := range s.reqHeaders {
		req.Header.Set(k, v)
	}

	s.response, err = s.api.client.Do(req)
	if err != nil {
		return err
	}
	s.body, err = io.ReadAll(s.response.Body)
	_ = s.response.Body.Close()
	if err != nil {
		return err
	}

	if method == http.MethodPost && (s.response.StatusCode == http.StatusAccepted || s.response.StatusCode == http.StatusCreated) {
		if matches := pathDetails.FindStringSubmatch(endpoint); len(matches) >= 3 && matches[2] != "" {
			s.lastId, err = s.extractId(s.body)
			if err != nil {
				return err
			}
			if s.lastId != "" {
				s.values["id"] = s.lastId
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Response assertions
// ---------------------------------------------------------------------------

func (s *scenarioState) theResponseCodeShouldBe(code int) error {
	if s.response == nil {
		return fmt.Errorf("expected status %d but no response was received", code)
	}
	if s.response.StatusCode != code {
		return fmt.Errorf("expected status %d, got %d: %s", code, s.response.StatusCode, string(s.body))
	}
	return nil
}

// ---------------------------------------------------------------------------
// JSONPath assertions
// ---------------------------------------------------------------------------

func (s *scenarioState) getJsonPathValue(jsonPath string) (interface{}, error) {
	var respMap map[string]interface{}
	if err := json.Unmarshal(s.body, &respMap); err != nil {
		return nil, err
	}
	path := jsonPath
	if !strings.HasPrefix(path, "$") {
		path = "$." + path
	}
	foundValue, err := jsonpath.Get(path, respMap)
	if err != nil {
		return nil, fmt.Errorf("JSONPath %s not found in %s: %w", jsonPath, asPrettyJson(string(s.body)), err)
	}
	return foundValue, nil
}

func (s *scenarioState) getJsonPath(jp string) (string, error) {
	jp = strings.ReplaceAll(jp, "&quot;", "\"")
	raw, err := s.getJsonPathValue(jp)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", raw), nil
}

func (s *scenarioState) theResponseShouldContainAtJSONPath(expectedValue, jsonPath string) error {
	expanded, err := s.substituteValues(expectedValue)
	if err != nil {
		return err
	}
	expectedValue = expanded

	foundValue, err := s.getJsonPath(jsonPath)
	if err != nil {
		return err
	}

	// Support OR matching with pipe separator
	for _, expected := range strings.Split(expectedValue, "|") {
		if strings.Contains(foundValue, expected) {
			return nil
		}
	}
	return fmt.Errorf("expected %q to contain %q at path %s", foundValue, expectedValue, jsonPath)
}

// ---------------------------------------------------------------------------
// Field saving
// ---------------------------------------------------------------------------

func getJsonPointer(path string) string {
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.ReplaceAll(path, ".", "/")
	path = arrayIndexRe.ReplaceAllString(path, "/$1")
	return path
}

func (s *scenarioState) theFieldShouldBeSaved(path, name string) error {
	jsonParsed, err := gabs.ParseJSON(s.body)
	if err != nil {
		return fmt.Errorf("failed to parse JSON response: %w", err)
	}
	pathObj, err := jsonParsed.JSONPointer(getJsonPointer(path))
	if err != nil {
		return fmt.Errorf("path %v does not exist in %s", path, string(s.body))
	}
	finalResult, ok := pathObj.Data().(string)
	if !ok {
		if floatResult, ok := pathObj.Data().(float64); ok {
			finalResult = strconv.FormatFloat(floatResult, 'f', -1, 64)
		} else {
			return fmt.Errorf("expected %s to be a string or float64 but got %T", path, pathObj.Data())
		}
	}
	if !strings.HasPrefix(name, valuePrefix) {
		return fmt.Errorf("save target %q must start with %q", name, valuePrefix)
	}
	realName := strings.TrimPrefix(name, valuePrefix)
	s.values[realName] = finalResult
	return nil
}

// ---------------------------------------------------------------------------
// Wait for evaluation job status
// ---------------------------------------------------------------------------

func (s *scenarioState) parseDuration(raw string) (time.Duration, error) {
	v, err := s.getValue(raw)
	if err != nil {
		return 0, err
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", v, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive, got %v", d)
	}
	return d, nil
}

func (s *scenarioState) iSetWaitDeadlineTo(paramValue string) error {
	d, err := s.parseDuration(paramValue)
	if err != nil {
		return err
	}
	s.waitDeadline = d
	return nil
}

func (s *scenarioState) iSetWaitIntervalTo(paramValue string) error {
	d, err := s.parseDuration(paramValue)
	if err != nil {
		return err
	}
	s.waitInterval = d
	return nil
}

func (s *scenarioState) iWaitForEvaluationJobStatus(expectedStatus string) error {
	deadline := time.Now().Add(s.waitDeadline)
	var lastErr error
	var lastStatus string

	for time.Now().Before(deadline) {
		if err := s.iSendRequestImpl(http.MethodGet, "/api/v1/evaluations/jobs/{id}", ""); err != nil {
			lastErr = err
			time.Sleep(s.waitInterval)
			continue
		}
		if s.response != nil && s.response.StatusCode == http.StatusOK {
			status, err := s.getJsonPath("$.status.state")
			if status != "" {
				lastStatus = status
			}
			if err != nil {
				lastErr = err
			} else if status == expectedStatus {
				return nil
			} else if pkgapi.OverallState(status).IsTerminalState() {
				message, _ := s.getJsonPath("$.status.message.message")
				if message != "" {
					return fmt.Errorf("evaluation job reached terminal state %q (expected %q): %s", status, expectedStatus, message)
				}
				return fmt.Errorf("evaluation job reached terminal state %q (expected %q)", status, expectedStatus)
			}
		} else if s.response != nil {
			lastErr = fmt.Errorf("unexpected response status %d", s.response.StatusCode)
		}
		time.Sleep(s.waitInterval)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("timed out after %v waiting for status %q, last status: %q", s.waitDeadline, expectedStatus, lastStatus)
}

// ---------------------------------------------------------------------------
// Upgrade state file
// ---------------------------------------------------------------------------

func repoRoot() string {
	repoRootOnce.Do(func() {
		wd, _ := os.Getwd()
		dir := wd
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				repoRootDir = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		repoRootDir = wd
	})
	return repoRootDir
}

func resolveRepoPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(repoRoot(), path)
}

const maxPaginationPages = 200

func (s *scenarioState) paginateJobs(visit func(*gabs.Container) error) error {
	endpoint := "/api/v1/evaluations/jobs"
	for page := 0; endpoint != ""; page++ {
		if page >= maxPaginationPages {
			return fmt.Errorf("pagination exceeded %d pages", maxPaginationPages)
		}
		ref, err := url.Parse(endpoint)
		if err != nil {
			return fmt.Errorf("parse endpoint %q: %w", endpoint, err)
		}
		req, err := http.NewRequest(http.MethodGet, s.api.baseURL.ResolveReference(ref).String(), nil)
		if err != nil {
			return fmt.Errorf("build list request: %w", err)
		}
		for k, v := range s.reqHeaders {
			req.Header.Set(k, v)
		}

		resp, err := s.api.client.Do(req)
		if err != nil {
			return fmt.Errorf("list jobs: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("list jobs returned %d: %s", resp.StatusCode, string(body))
		}

		parsed, err := gabs.ParseJSON(body)
		if err != nil {
			return fmt.Errorf("parse list response: %w", err)
		}

		for _, item := range parsed.Path("items").Children() {
			if err := visit(item); err != nil {
				return err
			}
		}

		if next := parsed.Path("next.href"); next != nil && next.Data() != nil {
			href, ok := next.Data().(string)
			if !ok {
				return fmt.Errorf("next.href is not a string: %T", next.Data())
			}
			endpoint = href
		} else {
			endpoint = ""
		}
	}
	return nil
}

func (s *scenarioState) iCollectAndSaveUpgradeState(path, expectedName, expectedState string) error {
	expanded, err := s.substituteValues(path)
	if err != nil {
		return fmt.Errorf("substitute state path: %w", err)
	}
	path = resolveRepoPath(expanded)

	var allJobs []upgradeJob
	var currentJob *upgradeJob
	if err := s.paginateJobs(func(item *gabs.Container) error {
		id, ok := item.Path("resource.id").Data().(string)
		if !ok {
			return fmt.Errorf("job missing resource.id or not a string")
		}
		name, ok := item.Path("name").Data().(string)
		if !ok {
			return fmt.Errorf("job missing name or not a string")
		}
		state, ok := item.Path("status.state").Data().(string)
		if !ok {
			return fmt.Errorf("job missing status.state or not a string")
		}
		job := upgradeJob{
			ID:    id,
			Name:  name,
			State: state,
		}
		if v := item.Path("resource.created_at").Data(); v != nil {
			job.CreatedAt, _ = v.(string)
		}
		if v := item.Path("resource.updated_at").Data(); v != nil {
			job.UpdatedAt, _ = v.(string)
		}
		allJobs = append(allJobs, job)
		if job.ID == s.lastId {
			currentJob = &allJobs[len(allJobs)-1]
		}
		return nil
	}); err != nil {
		return err
	}

	if currentJob == nil {
		return fmt.Errorf("current job %s not found in jobs list", s.lastId)
	}
	if currentJob.Name != expectedName {
		return fmt.Errorf("current job %s has name %q, expected %q", s.lastId, currentJob.Name, expectedName)
	}
	if currentJob.State != expectedState {
		return fmt.Errorf("current job %s has state %q, expected %q", s.lastId, currentJob.State, expectedState)
	}

	state := upgradeState{CurrentJobID: currentJob.ID, Jobs: allJobs}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal upgrade state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write upgrade state: %w", err)
	}
	return nil
}

func (s *scenarioState) iLoadUpgradeState(path string) error {
	expanded, err := s.substituteValues(path)
	if err != nil {
		return fmt.Errorf("substitute state path: %w", err)
	}
	path = resolveRepoPath(expanded)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read upgrade state %s: %w", path, err)
	}
	var state upgradeState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse upgrade state: %w", err)
	}
	s.loadedState = &state
	if state.CurrentJobID != "" {
		s.values["job_id"] = state.CurrentJobID
		s.lastId = state.CurrentJobID
	} else if len(state.Jobs) > 0 {
		s.values["job_id"] = state.Jobs[0].ID
		s.lastId = state.Jobs[0].ID
	}
	return nil
}

func (s *scenarioState) iVerifyAllJobsFromUpgradeStateExist() error {
	if s.loadedState == nil {
		return fmt.Errorf("no upgrade state loaded")
	}

	collected := make(map[string]bool)
	if err := s.paginateJobs(func(item *gabs.Container) error {
		if id, ok := item.Path("resource.id").Data().(string); ok {
			collected[id] = true
		}
		return nil
	}); err != nil {
		return err
	}

	var missing []string
	for _, job := range s.loadedState.Jobs {
		if !collected[job.ID] {
			missing = append(missing, fmt.Sprintf("%s (%s)", job.ID, job.Name))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("jobs from upgrade state not found: %s", strings.Join(missing, ", "))
	}

	return nil
}

func (s *scenarioState) iHardDeleteAllEvaluationJobsContaining(substring string) error {
	var jobIDs []string
	if err := s.paginateJobs(func(item *gabs.Container) error {
		name, _ := item.Path("name").Data().(string)
		if strings.Contains(name, substring) {
			if id, ok := item.Path("resource.id").Data().(string); ok {
				jobIDs = append(jobIDs, id)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	var errors []string
	for _, id := range jobIDs {
		delURL := s.api.baseURL.JoinPath("/api/v1/evaluations/jobs", id).String() + "?hard_delete=true"
		req, err := http.NewRequest(http.MethodDelete, delURL, nil)
		if err != nil {
			errors = append(errors, fmt.Sprintf("build delete request for %s: %v", id, err))
			continue
		}
		for k, v := range s.reqHeaders {
			req.Header.Set(k, v)
		}

		resp, err := s.api.client.Do(req)
		if err != nil {
			errors = append(errors, fmt.Sprintf("delete %s: %v", id, err))
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
			errors = append(errors, fmt.Sprintf("delete %s returned %d", id, resp.StatusCode))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func asPrettyJson(s string) string {
	var js map[string]interface{}
	if err := json.Unmarshal([]byte(s), &js); err != nil {
		return s
	}
	ns, err := json.MarshalIndent(js, "", "  ")
	if err != nil {
		return s
	}
	return string(ns)
}
