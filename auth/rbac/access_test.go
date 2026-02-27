package rbac

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/spf13/viper"
)

// loadRBACConfigFromYAML loads an RBAC config from a YAML file using Viper.
// yamlName is the base name of the file (e.g. "rbac_jobs") under testdata/.
func loadRBACConfigFromYAML(t *testing.T, yamlName string) AuthorizationConfig {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	configPath := filepath.Join(dir, "testdata", yamlName+".yaml")

	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig(%q): %v", configPath, err)
	}

	var cfg AuthorizationConfig
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return cfg
}

func TestComputeResourceAttributesSuite(t *testing.T) {
	t.Run("JobsPost", func(t *testing.T) {
		cfg := loadRBACConfigFromYAML(t, "rbac_jobs")

		req := FromRequest{
			Endpoint: "/api/v1/evaluations/jobs",
			Method:   "POST",
			Headers: Headers{
				"X-Tenant": {"tenant-a"},
			},
			QueryStrings: QueryStrings{},
		}

		got := ComputeResourceAttributes(req, cfg)

		want := []ResourceAttributes{
			{
				Namespace: "tenant-a",
				APIGroup:  "trustyai.opendatahub.io",
				Resource:  "evaluations",
				Verb:      "create",
			},
			{
				Namespace: "tenant-a",
				APIGroup:  "mlflow.kubeflow.org",
				Resource:  "experiments",
				Verb:      "create",
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}

	})
	t.Run("JobsGet", func(t *testing.T) {
		cfg := loadRBACConfigFromYAML(t, "rbac_jobs")

		req := FromRequest{
			Endpoint:     "/api/v1/evaluations/jobs",
			Method:       "GET",
			Headers:      Headers{"X-Tenant": {"my-ns"}},
			QueryStrings: QueryStrings{},
		}

		got := ComputeResourceAttributes(req, cfg)

		want := []ResourceAttributes{
			{
				Namespace: "my-ns",
				APIGroup:  "trustyai.opendatahub.io",
				Resource:  "evaluations",
				Verb:      "get",
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}
	})
	t.Run("NoMatch", func(t *testing.T) {
		cfg := loadRBACConfigFromYAML(t, "rbac_jobs")

		req := FromRequest{
			Endpoint:     "/api/v1/other",
			Method:       "GET",
			Headers:      Headers{},
			QueryStrings: QueryStrings{},
		}

		got := ComputeResourceAttributes(req, cfg)

		if len(got) != 0 {
			t.Errorf("ComputeResourceAttributes() = %+v, want nil/empty", got)
		}
	})
	t.Run("QueryString", func(t *testing.T) {
		cfg := loadRBACConfigFromYAML(t, "rbac_query")

		req := FromRequest{
			Endpoint: "/api/v1/namespaces",
			Method:   "GET",
			Headers:  Headers{},
			QueryStrings: QueryStrings{
				"namespace": {"query-ns"},
			},
		}

		got := ComputeResourceAttributes(req, cfg)

		want := []ResourceAttributes{
			{
				Namespace: "query-ns",
				APIGroup:  "",
				Resource:  "namespaces",
				Verb:      "get",
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}
	})
	t.Run("CollectionsMethodVerb", func(t *testing.T) {
		cfg := loadRBACConfigFromYAML(t, "rbac_mixed")

		req := FromRequest{
			Endpoint:     "/api/v1/evaluations/collections",
			Method:       "DELETE",
			Headers:      Headers{"X-Tenant": {"tenant-b"}},
			QueryStrings: QueryStrings{},
		}

		got := ComputeResourceAttributes(req, cfg)

		want := []ResourceAttributes{
			{
				Namespace: "tenant-b",
				APIGroup:  "trustyai.opendatahub.io",
				Resource:  "collections",
				Verb:      "delete",
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}
	})
	t.Run("NoHeader", func(t *testing.T) {
		cfg := loadRBACConfigFromYAML(t, "rbac_jobs")

		req := FromRequest{
			Endpoint:     "/api/v1/evaluations/jobs",
			Method:       "GET",
			Headers:      Headers{},
			QueryStrings: QueryStrings{},
		}

		got := ComputeResourceAttributes(req, cfg)

		// Rule still matches; namespace comes from empty header (template yields empty)
		want := []ResourceAttributes{
			{
				Namespace: "",
				APIGroup:  "trustyai.opendatahub.io",
				Resource:  "evaluations",
				Verb:      "get",
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}

	})
}
