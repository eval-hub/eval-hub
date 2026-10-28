package benchmarks

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestRoleFixturesDoNotSerializeConnectionParameters(t *testing.T) {
	for _, provider := range []string{"inspect", "deepeval", "ragas", "followbench", "tooleval", "ibm-clear", "promptfoo"} {
		paths, err := filepath.Glob(filepath.Join("testdata", provider, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			t.Run(path, func(t *testing.T) {
				lookup := func(key string) (string, bool) {
					if key == "JUDGE_API_KEY" {
						return "credential-must-not-enter-job", true
					}
					if key == "JUDGE_BASE_URL" {
						return "https://judge-must-not-enter-parameters.example/v1", true
					}
					return fixtureEnv(key)
				}
				f, err := loadFixture(path, lookup)
				if err != nil {
					t.Fatal(err)
				}
				job := f.jobConfig(provider, "fixture-benchmark", api.ModelRef{
					Name: "target", URL: "https://target.example/v1", Auth: &api.ModelAuth{SecretRef: "model-auth"},
				})
				if job.Model.Auth == nil || job.Model.Auth.SecretRef != "model-auth" {
					t.Fatal("model Secret reference was lost")
				}
				body, err := json.Marshal(job)
				if err != nil {
					t.Fatal(err)
				}
				for _, forbidden := range []string{"credential-must-not-enter-job", "judge-must-not-enter-parameters.example", `"judge_api_key"`, `"judge_base_url"`, `"auditor_base_url"`, `"grader_base_url"`, `"eval_model_url"`, `"embedding_url"`} {
					if strings.Contains(string(body), forbidden) {
						t.Errorf("submitted request contains legacy connection input %s", forbidden)
					}
				}
			})
		}
	}
}
