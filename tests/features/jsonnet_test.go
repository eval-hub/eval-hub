package features

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-jsonnet"
)

type jsonnetHarness struct {
	Env           map[string]string `json:"env"`
	Values        map[string]string `json:"values"`
	MlflowEnabled bool              `json:"mlflow_enabled"`
	Disconnected  bool              `json:"disconnected"`
}

// jsonnetSiblingName returns the test-data filename with a .jsonnet extension for the
// same base name as fileName (e.g. evaluation_job.json -> evaluation_job.jsonnet).
func (tc *scenarioConfig) jsonnetSiblingName(fileName string) string {
	ext := filepath.Ext(fileName)
	if ext == "" {
		return fileName + ".jsonnet"
	}
	return strings.TrimSuffix(fileName, ext) + ".jsonnet"
}

func testDataRoot() string {
	candidates := []string{
		filepath.Join("tests", "features", "test_data"),
		filepath.Join("test_data"),
	}
	for _, dir := range candidates {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
	return filepath.Join("tests", "features", "test_data")
}

func jsonnetLibDir() string {
	return filepath.Join(testDataRoot(), "jsonnet")
}

func (tc *scenarioConfig) jsonnetProcessEnv() map[string]string {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		key, val, _ := strings.Cut(kv, "=")
		env[key] = val
	}
	for _, key := range tc.jsonnetHarnessEnvOmit {
		delete(env, key)
	}
	for key, val := range tc.jsonnetHarnessEnv {
		env[key] = val
	}
	return env
}

func (tc *scenarioConfig) jsonnetHarnessJSON() (string, error) {
	env := tc.jsonnetProcessEnv()
	values := tc.values
	if values == nil {
		values = map[string]string{}
	}
	mlflowEnabled := env["MLFLOW_TRACKING_URI"] != ""
	if tc.jsonnetMlflowEnabled != nil {
		mlflowEnabled = *tc.jsonnetMlflowEnabled
	}
	disconnected := strings.Contains(strings.ToLower(env["ENVIRONMENT_ID"]), "disconnected")
	harness := jsonnetHarness{
		Env:           env,
		Values:        values,
		MlflowEnabled: mlflowEnabled,
		Disconnected:  disconnected,
	}
	encoded, err := json.Marshal(harness)
	if err != nil {
		return "", fmt.Errorf("encode jsonnet harness: %w", err)
	}
	return string(encoded), nil
}

func (tc *scenarioConfig) newJsonnetVM() (*jsonnet.VM, error) {
	harnessJSON, err := tc.jsonnetHarnessJSON()
	if err != nil {
		return nil, err
	}
	vm := jsonnet.MakeVM()
	vm.Importer(&jsonnet.FileImporter{
		JPaths: []string{jsonnetLibDir()},
	})
	vm.ExtVar("harness", harnessJSON)
	return vm, nil
}

func (tc *scenarioConfig) evaluateJsonnetFile(path string) (string, error) {
	vm, err := tc.newJsonnetVM()
	if err != nil {
		return "", err
	}
	output, err := vm.EvaluateFile(path)
	if err != nil {
		return "", fmt.Errorf("evaluate jsonnet file %s: %w", path, err)
	}
	return output, nil
}

func TestJsonnetHarnessEnvAndValue(t *testing.T) {
	tc := &scenarioConfig{
		values: map[string]string{"collection_id": "col-123"},
		jsonnetHarnessEnv: map[string]string{
			"MODEL_URL": "http://example.com",
		},
	}
	vm, err := tc.newJsonnetVM()
	if err != nil {
		t.Fatalf("newJsonnetVM: %v", err)
	}
	out, err := vm.EvaluateAnonymousSnippet("snippet.jsonnet", `
local test = import 'test.libsonnet';
{
  url: test.env('MODEL_URL', 'http://fallback'),
  missing: test.env('NOT_SET', 'default'),
  collection: test.value('collection_id'),
}
`)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["url"] != "http://example.com" {
		t.Errorf("url = %q, want http://example.com", got["url"])
	}
	if got["missing"] != "default" {
		t.Errorf("missing = %q, want default", got["missing"])
	}
	if got["collection"] != "col-123" {
		t.Errorf("collection = %q, want col-123", got["collection"])
	}
}

func TestJsonnetHarnessEnvOverrides(t *testing.T) {
	t.Setenv("MODEL_AUTH_SECRET_REF", "from-process")
	tc := &scenarioConfig{
		jsonnetHarnessEnvOmit: []string{"MODEL_AUTH_SECRET_REF"},
	}
	vm, err := tc.newJsonnetVM()
	if err != nil {
		t.Fatalf("newJsonnetVM: %v", err)
	}
	out, err := vm.EvaluateAnonymousSnippet("snippet.jsonnet", `
local test = import 'test.libsonnet';
test.model()
`)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	var omitted map[string]interface{}
	if err := json.Unmarshal([]byte(out), &omitted); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := omitted["auth"]; ok {
		t.Fatal("auth should be omitted when MODEL_AUTH_SECRET_REF is omitted from harness")
	}

	tc.jsonnetHarnessEnvOmit = nil
	tc.jsonnetHarnessEnv = map[string]string{"MODEL_AUTH_SECRET_REF": "harness-only"}
	vm, err = tc.newJsonnetVM()
	if err != nil {
		t.Fatalf("newJsonnetVM with harness env: %v", err)
	}
	out, err = vm.EvaluateAnonymousSnippet("snippet.jsonnet", `
local test = import 'test.libsonnet';
test.model()
`)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	auth, ok := got["auth"].(map[string]interface{})
	if !ok {
		t.Fatalf("auth = %#v, want object", got["auth"])
	}
	if auth["secret_ref"] != "harness-only" {
		t.Errorf("secret_ref = %v, want harness-only", auth["secret_ref"])
	}
}

func TestJsonnetModelAuth(t *testing.T) {
	tc := &scenarioConfig{
		values:                map[string]string{},
		jsonnetHarnessEnvOmit: []string{"MODEL_AUTH_SECRET_REF"},
	}
	eval := func() map[string]interface{} {
		t.Helper()
		vm, err := tc.newJsonnetVM()
		if err != nil {
			t.Fatalf("newJsonnetVM: %v", err)
		}
		out, err := vm.EvaluateAnonymousSnippet("snippet.jsonnet", `
local test = import 'test.libsonnet';
test.model()
`)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		var got map[string]interface{}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return got
	}

	if _, ok := eval()["auth"]; ok {
		t.Fatal("auth should be omitted when MODEL_AUTH_SECRET_REF is unset")
	}

	tc.jsonnetHarnessEnvOmit = nil
	tc.jsonnetHarnessEnv = map[string]string{"MODEL_AUTH_SECRET_REF": "my-secret"}
	got := eval()
	auth, ok := got["auth"].(map[string]interface{})
	if !ok {
		t.Fatalf("auth = %#v, want object", got["auth"])
	}
	if auth["secret_ref"] != "my-secret" {
		t.Errorf("secret_ref = %v, want my-secret", auth["secret_ref"])
	}
}

func TestJsonnetExperiment(t *testing.T) {
	mlflowOff := false
	tc := &scenarioConfig{
		values:               map[string]string{},
		jsonnetMlflowEnabled: &mlflowOff,
	}
	evalExperiment := func() (string, error) {
		t.Helper()
		vm, err := tc.newJsonnetVM()
		if err != nil {
			return "", err
		}
		return vm.EvaluateAnonymousSnippet("snippet.jsonnet", `
local test = import 'test.libsonnet';
test.experiment('my-test-experiment')
`)
	}
	evalMerged := func() (map[string]interface{}, error) {
		t.Helper()
		vm, err := tc.newJsonnetVM()
		if err != nil {
			return nil, err
		}
		out, err := vm.EvaluateAnonymousSnippet("snippet.jsonnet", `
local test = import 'test.libsonnet';
test.mergeOptional({ name: 'base' }, test.experiment('my-test-experiment'))
`)
		if err != nil {
			return nil, err
		}
		var got map[string]interface{}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			return nil, err
		}
		return got, nil
	}

	out, err := evalExperiment()
	if err != nil {
		t.Fatalf("evaluate experiment: %v", err)
	}
	if strings.TrimSpace(out) != "null" {
		t.Fatalf("experiment alone = %q, want null when MLflow is not configured", out)
	}

	merged, err := evalMerged()
	if err != nil {
		t.Fatalf("evaluate mergeOptional: %v", err)
	}
	if _, ok := merged["experiment"]; ok {
		t.Fatalf("merged = %#v, experiment key should be absent", merged)
	}
	if merged["name"] != "base" {
		t.Errorf("name = %v, want base", merged["name"])
	}

	mlflowOn := true
	tc.jsonnetMlflowEnabled = &mlflowOn
	out, err = evalExperiment()
	if err != nil {
		t.Fatalf("evaluate experiment with mlflow: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	exp, ok := got["experiment"].(map[string]interface{})
	if !ok {
		t.Fatalf("experiment = %#v, want object", got["experiment"])
	}
	if exp["name"] != "my-test-experiment" {
		t.Errorf("name = %v, want my-test-experiment", exp["name"])
	}
	tags, ok := exp["tags"].([]interface{})
	if !ok || len(tags) != 1 {
		t.Fatalf("tags = %#v, want one tag", exp["tags"])
	}
	tag, ok := tags[0].(map[string]interface{})
	if !ok || tag["key"] != "environment" || tag["value"] != "test" {
		t.Errorf("tag = %#v, want environment=test", tag)
	}
}

func TestJsonnetMlflow(t *testing.T) {
	mlflowOff := false
	tc := &scenarioConfig{
		values:               map[string]string{},
		jsonnetMlflowEnabled: &mlflowOff,
	}
	vm, err := tc.newJsonnetVM()
	if err != nil {
		t.Fatalf("newJsonnetVM: %v", err)
	}
	out, err := vm.EvaluateAnonymousSnippet("snippet.jsonnet", `
local test = import 'test.libsonnet';
{ name: test.mlflow('my-experiment') }
`)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	var disabled map[string]string
	if err := json.Unmarshal([]byte(out), &disabled); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if disabled["name"] != "" {
		t.Errorf("mlflow disabled name = %q, want empty", disabled["name"])
	}

	mlflowOn := true
	tc.jsonnetMlflowEnabled = &mlflowOn
	vm, err = tc.newJsonnetVM()
	if err != nil {
		t.Fatalf("newJsonnetVM with mlflow: %v", err)
	}
	out, err = vm.EvaluateAnonymousSnippet("snippet.jsonnet", `
local test = import 'test.libsonnet';
{ name: test.mlflow('my-experiment') }
`)
	if err != nil {
		t.Fatalf("evaluate with mlflow: %v", err)
	}
	var enabled map[string]string
	if err := json.Unmarshal([]byte(out), &enabled); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if enabled["name"] != "my-experiment" {
		t.Errorf("mlflow enabled name = %q, want my-experiment", enabled["name"])
	}
}

func TestJsonnetCollection(t *testing.T) {
	tc := &scenarioConfig{values: map[string]string{}}
	vm, err := tc.newJsonnetVM()
	if err != nil {
		t.Fatalf("newJsonnetVM: %v", err)
	}
	out, err := vm.EvaluateAnonymousSnippet("snippet.jsonnet", `
local test = import 'test.libsonnet';
test.collection()
`)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if strings.TrimSpace(out) != "null" {
		t.Fatalf("collection alone = %q, want null when collection_id is unset", out)
	}

	tc.values = map[string]string{"collection_id": "col-123"}
	vm, err = tc.newJsonnetVM()
	if err != nil {
		t.Fatalf("newJsonnetVM with values: %v", err)
	}
	out, err = vm.EvaluateAnonymousSnippet("snippet.jsonnet", `
local test = import 'test.libsonnet';
test.collection()
`)
	if err != nil {
		t.Fatalf("evaluate with id: %v", err)
	}
	var got map[string]map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["collection"]["id"] != "col-123" {
		t.Errorf("collection.id = %q, want col-123", got["collection"]["id"])
	}
}

func TestEvaluateEvaluationJobJsonnetDisconnected(t *testing.T) {
	tc := &scenarioConfig{
		values: map[string]string{},
		jsonnetHarnessEnv: map[string]string{
			"ENVIRONMENT_ID": "disconnected",
		},
	}
	path, err := filepath.Abs(filepath.Join(testDataRoot(), "evaluation_job.jsonnet"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	out, err := tc.evaluateJsonnetFile(path)
	if err != nil {
		t.Fatalf("evaluateJsonnetFile: %v", err)
	}
	var job struct {
		Benchmarks []struct {
			ID          string         `json:"id"`
			Parameters  map[string]any `json:"parameters"`
			TestDataRef struct {
				S3 struct {
					Bucket    string `json:"bucket"`
					Key       string `json:"key"`
					SecretRef string `json:"secret_ref"`
				} `json:"s3"`
			} `json:"test_data_ref"`
		} `json:"benchmarks"`
	}
	if err := json.Unmarshal([]byte(out), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(job.Benchmarks) != 1 {
		t.Fatalf("benchmarks = %#v, want one benchmark", job.Benchmarks)
	}
	b := job.Benchmarks[0]
	if b.ID != "blimp" {
		t.Errorf("benchmark id = %q, want blimp", b.ID)
	}
	if b.Parameters["tokenizer"] != "/test_data/tokenizer" {
		t.Errorf("tokenizer = %v, want /test_data/tokenizer", b.Parameters["tokenizer"])
	}
	if b.TestDataRef.S3.Bucket != "mlpipeline" || b.TestDataRef.S3.Key != "offline" || b.TestDataRef.S3.SecretRef != "minio-test" {
		t.Errorf("test_data_ref.s3 = %+v, want mlpipeline/offline/minio-test", b.TestDataRef.S3)
	}
}

func TestEvaluateEvaluationJobJsonnet(t *testing.T) {
	tc := &scenarioConfig{
		values: map[string]string{},
		jsonnetHarnessEnv: map[string]string{
			"MODEL_NAME": "my-model",
		},
	}
	path, err := filepath.Abs(filepath.Join(testDataRoot(), "evaluation_job.jsonnet"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
	out, err := tc.evaluateJsonnetFile(path)
	if err != nil {
		t.Fatalf("evaluateJsonnetFile: %v", err)
	}
	var job struct {
		Model struct {
			URL  string `json:"url"`
			Name string `json:"name"`
		} `json:"model"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.Model.Name != "my-model" {
		t.Errorf("model.name = %q, want my-model", job.Model.Name)
	}
	if job.Name != "test-evaluation-job" {
		t.Errorf("name = %q, want test-evaluation-job", job.Name)
	}
}

func TestEvaluateEvaluationJobWithCollectionJsonnet(t *testing.T) {
	tc := &scenarioConfig{values: map[string]string{"collection_id": "col-abc"}}
	path, err := filepath.Abs(filepath.Join(testDataRoot(), "evaluation_job.jsonnet"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	out, err := tc.evaluateJsonnetFile(path)
	if err != nil {
		t.Fatalf("evaluateJsonnetFile: %v", err)
	}
	var job struct {
		Model      map[string]interface{} `json:"model"`
		Collection struct {
			ID string `json:"id"`
		} `json:"collection"`
		Name       string `json:"name"`
		Benchmarks []any  `json:"benchmarks"`
	}
	if err := json.Unmarshal([]byte(out), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.Collection.ID != "col-abc" {
		t.Errorf("collection.id = %q, want col-abc", job.Collection.ID)
	}
	if job.Name != "test-evaluation-job" {
		t.Errorf("name = %q, want test-evaluation-job", job.Name)
	}
	if job.Benchmarks != nil {
		t.Errorf("benchmarks = %#v, want key absent for collection job", job.Benchmarks)
	}
}

func TestEvaluateEvaluationJobJsonnetWithCollectionId(t *testing.T) {
	tc := &scenarioConfig{values: map[string]string{"collection_id": "col-xyz"}}
	path, err := filepath.Abs(filepath.Join(testDataRoot(), "evaluation_job.jsonnet"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	out, err := tc.evaluateJsonnetFile(path)
	if err != nil {
		t.Fatalf("evaluateJsonnetFile: %v", err)
	}
	var job struct {
		Collection struct {
			ID string `json:"id"`
		} `json:"collection"`
		Benchmarks []any `json:"benchmarks"`
	}
	if err := json.Unmarshal([]byte(out), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.Collection.ID != "col-xyz" {
		t.Errorf("collection.id = %q, want col-xyz", job.Collection.ID)
	}
	if job.Benchmarks != nil {
		t.Errorf("benchmarks = %#v, want key absent when collection_id is set", job.Benchmarks)
	}
}
