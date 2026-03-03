package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/viper"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

// loadAuthConfigFromYAML loads an RBAC config from a YAML file using Viper.
// yamlName is the base name of the file (e.g. "rbac_jobs") under testdata/.
func loadAuthConfigFromYAML(t *testing.T, yamlName string) AuthConfig {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	configPath := filepath.Join(dir, "testdata", yamlName+".yaml")

	t.Logf("Loading RBAC config from %s", configPath)
	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig(%q): %v", configPath, err)
	}

	var cfg AuthConfig
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return cfg
}

func attributesToRecords(attributes []authorizer.Attributes) []authorizer.AttributesRecord {
	records := []authorizer.AttributesRecord{}
	for _, attribute := range attributes {
		records = append(records, attribute.(authorizer.AttributesRecord))
	}
	return records
}

type TestUser struct {
	Name string
}

func NewTestUser(name string) *TestUser {
	return &TestUser{Name: name}
}

func (u *TestUser) GetName() string {
	return u.Name
}

func (u *TestUser) GetUID() string {
	return u.Name
}

func (u *TestUser) GetGroups() []string {
	return []string{"test"}
}

func (u *TestUser) GetExtra() map[string][]string {
	return map[string][]string{}
}

func eq(got []authorizer.AttributesRecord, want []authorizer.AttributesRecord) bool {

	if len(got) != len(want) {
		return false
	}
	for i, g := range got {
		w := want[i]
		if g.Namespace != w.Namespace ||
			g.APIGroup != w.APIGroup ||
			g.Resource != w.Resource ||
			g.Verb != w.Verb {
			return false
		}
	}
	return true
}

func TestComputeResourceAttributesSuite(t *testing.T) {
	t.Run("JobsPost", func(t *testing.T) {
		cfg := loadAuthConfigFromYAML(t, "rbac_jobs")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs", nil)
		req.Header.Set("X-Tenant", "tenant-a")

		got := attributesToRecords(AttributesFromRequest(req, cfg, NewTestUser("test")))

		fmt.Println("got ", got)
		want := []authorizer.AttributesRecord{
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
		if !eq(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}
	})
	t.Run("JobsGet", func(t *testing.T) {
		cfg := loadAuthConfigFromYAML(t, "rbac_jobs")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/jobs", nil)
		req.Header.Set("X-Tenant", "my-ns")

		got := attributesToRecords(AttributesFromRequest(req, cfg, NewTestUser("test")))

		want := []authorizer.AttributesRecord{
			{
				Namespace: "my-ns",
				APIGroup:  "trustyai.opendatahub.io",
				Resource:  "evaluations",
				Verb:      "get",
			},
		}
		if !eq(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}
	})
	t.Run("NoMatch", func(t *testing.T) {
		cfg := loadAuthConfigFromYAML(t, "rbac_jobs")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/other", nil)
		req.Header.Set("X-Tenant", "my-ns")

		got := attributesToRecords(AttributesFromRequest(req, cfg, NewTestUser("test")))

		if len(got) != 0 {
			t.Errorf("ComputeResourceAttributes() = %+v, want nil/empty", got)
		}
	})
	t.Run("QueryString", func(t *testing.T) {
		cfg := loadAuthConfigFromYAML(t, "rbac_query")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces?tenant=query-ns", nil)

		got := attributesToRecords(AttributesFromRequest(req, cfg, NewTestUser("test")))

		want := []authorizer.AttributesRecord{
			{
				Namespace: "query-ns",
				APIGroup:  "",
				Resource:  "namespaces",
				Verb:      "get",
			},
		}
		if !eq(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}
	})
	t.Run("CollectionsMethodVerb", func(t *testing.T) {
		cfg := loadAuthConfigFromYAML(t, "rbac_mixed")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/evaluations/collections", nil)
		req.Header.Set("X-Tenant", "tenant-b")

		got := attributesToRecords(AttributesFromRequest(req, cfg, NewTestUser("test")))

		want := []authorizer.AttributesRecord{
			{
				Namespace: "tenant-b",
				APIGroup:  "trustyai.opendatahub.io",
				Resource:  "collections",
				Verb:      "delete",
			},
		}

		if !eq(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}

		req = httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/providers", nil)
		req.Header.Set("X-Tenant", "tenant-b")

		got = attributesToRecords(AttributesFromRequest(req, cfg, NewTestUser("test")))

		want = []authorizer.AttributesRecord{
			{
				Namespace: "tenant-b",
				APIGroup:  "trustyai.opendatahub.io",
				Resource:  "providers",
				Verb:      "create",
			},
		}

		if !eq(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}
	})
	t.Run("NoHeader", func(t *testing.T) {
		cfg := loadAuthConfigFromYAML(t, "rbac_jobs")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/jobs", nil)

		got := attributesToRecords(AttributesFromRequest(req, cfg, NewTestUser("test")))

		// Rule still matches; namespace comes from empty header (template yields empty)
		want := []authorizer.AttributesRecord{
			{
				Namespace: "",
				APIGroup:  "trustyai.opendatahub.io",
				Resource:  "evaluations",
				Verb:      "get",
			},
		}
		if !eq(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}

	})
}
