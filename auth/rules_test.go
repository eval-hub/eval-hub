package auth_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/auth"
	"github.com/spf13/viper"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

// loadAuthConfigFromYAML loads an RBAC config from a YAML file using Viper.
// yamlName is the base name of the file (e.g. "rbac_jobs") under testdata/.
func loadAuthConfigFromYAML(t *testing.T, yamlName string) *auth.AuthConfig {
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

	var cfg auth.AuthConfig
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return cfg.Optimize()
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

		atr, err := auth.AttributesFromRequest(req, cfg, NewTestUser("test"))
		if err != nil {
			t.Errorf("Got error = %+v, want nil", err)
		}
		got := attributesToRecords(atr)

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

		atr, err := auth.AttributesFromRequest(req, cfg, NewTestUser("test"))
		if err != nil {
			t.Errorf("Got error = %+v, want nil", err)
		}
		got := attributesToRecords(atr)

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

		atr, err := auth.AttributesFromRequest(req, cfg, NewTestUser("test"))
		if err != nil {
			t.Errorf("Got error = %+v, want nil", err)
		}
		got := attributesToRecords(atr)

		if len(got) != 0 {
			t.Errorf("ComputeResourceAttributes() = %+v, want nil/empty", got)
		}
	})
	t.Run("QueryString", func(t *testing.T) {
		cfg := loadAuthConfigFromYAML(t, "rbac_query")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces?tenant=query-ns", nil)

		atr, err := auth.AttributesFromRequest(req, cfg, NewTestUser("test"))
		if err != nil {
			t.Errorf("Got error = %+v, want nil", err)
		}
		got := attributesToRecords(atr)

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

		atr, err := auth.AttributesFromRequest(req, cfg, NewTestUser("test"))
		if err != nil {
			t.Errorf("Got error = %+v, want nil", err)
		}
		got := attributesToRecords(atr)

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

		atr, err = auth.AttributesFromRequest(req, cfg, NewTestUser("test"))
		if err != nil {
			t.Errorf("Got error = %+v, want nil", err)
		}
		got = attributesToRecords(atr)

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

		atr, err := auth.AttributesFromRequest(req, cfg, NewTestUser("test"))
		if err == nil {
			t.Errorf("Expected error = %+v, go %+v", err, atr)
		}

	})
	t.Run("MatchSpecificJob", func(t *testing.T) {
		cfg := loadAuthConfigFromYAML(t, "rbac_jobs")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/evaluations/jobs/2349872398472", nil)
		req.Header.Set("X-Tenant", "my-ns")

		atr, err := auth.AttributesFromRequest(req, cfg, NewTestUser("test"))
		if err != nil {
			t.Errorf("Got error = %+v, want nil", err)
		}
		got := attributesToRecords(atr)

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

	t.Run("MatchStatusEvents", func(t *testing.T) {
		cfg := loadAuthConfigFromYAML(t, "rbac_jobs")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs/2349872398472/events", nil)
		req.Header.Set("X-Tenant", "my-ns")

		atr, err := auth.AttributesFromRequest(req, cfg, NewTestUser("test"))
		if err != nil {
			t.Errorf("Got error = %+v, want nil", err)
		}
		got := attributesToRecords(atr)

		want := []authorizer.AttributesRecord{
			{
				Namespace: "my-ns",
				APIGroup:  "trustyai.opendatahub.io",
				Resource:  "status-events",
				Verb:      "create",
			},
		}
		if !eq(got, want) {
			t.Errorf("ComputeResourceAttributes() = %+v, want %+v", got, want)
		}
	})

	t.Run("MatchPaths", func(t *testing.T) {
		cases := []struct {
			pattern       string
			path          string
			expectedMatch bool
		}{
			{"/api/v1/jobs", "/api/v1/jobsabc", false},
			{"/api/v1/jobs", "/api/v1/jobs/123", true},
			{"/api/v1/jobs/*", "/api/v1/jobs", true},
			{"/api/v1/jobs/*", "/api/v1/jobs/123", true},
			{"/api/v1/jobs/*", "/api/v1/jobs/123/details", true},
			{"/api/*/jobs/*", "/api/v2/jobs/abc", true},
			{"/api/*/jobs/*", "/api/v2/users/123", false},
			{"/api/v1/evaluations/jobs/*/events", "/api/v1/evaluations/jobs", false},
		}

		for _, c := range cases {
			e := auth.Endpoint{
				Path:      c.pattern,
				PathParts: strings.Split(c.pattern, "/"),
			}
			match := auth.MatchEndpoint(c.path, e)
			if match != c.expectedMatch {
				t.Errorf("MatchEndpoint(%s, %s) = %v, want %v", c.pattern, c.path, match, c.expectedMatch)
			}
			fmt.Printf("Pattern: %-15s Path: %-25s Match: %v\n", c.pattern, c.path, auth.MatchEndpoint(c.path, e))
		}
	})
}

func TestHttpToKubeVerb(t *testing.T) {
	cases := map[string]string{
		http.MethodGet:     "get",
		http.MethodPost:    "create",
		http.MethodPut:     "update",
		http.MethodDelete:  "delete",
		http.MethodPatch:   "patch",
		http.MethodOptions: "options",
		http.MethodHead:    "head",
		"UNKNOWN":          "",
	}
	for method, want := range cases {
		if got := auth.HttpToKubeVerb(method); got != want {
			t.Errorf("httpToKubeVerb(%q) = %q, want %q", method, got, want)
		}
	}
}
