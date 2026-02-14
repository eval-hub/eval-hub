package features

import (
	"os"
	"strings"
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	missing := missingRequiredEnvVars()
	if len(missing) > 0 {
		t.Skipf("skipping kubernetes tests; missing env vars: %s", strings.Join(missing, ", "))
	}
	opts := &godog.Options{
		Format:   "pretty",
		Paths:    []string{"."},
		TestingT: t,
		Strict:   true,
	}
	if tags := os.Getenv("GODOG_TAGS"); tags != "" {
		opts.Tags = tags
	}

	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options:             opts,
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run kubernetes feature tests")
	}
}

func missingRequiredEnvVars() []string {
	required := []string{"SERVER_URL", "KUBERNETES_NAMESPACE", "AUTH_TOKEN"}
	missing := make([]string, 0, len(required))
	for _, name := range required {
		if os.Getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
