package features

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	if os.Getenv("MLFLOW_TRACKING_URI") == "" {
		t.Skip("skipping mlflow tests; MLFLOW_TRACKING_URI is not set")
	}
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"."},
			TestingT: t,
			// Strict mode will fail immediately on undefined steps
			Strict: true,
			Tags:   "~@ignore",
		},
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests. Check for undefined steps in feature files.")
	}
}

func TestMain(m *testing.M) {
	status := m.Run()
	os.Exit(status)
}
