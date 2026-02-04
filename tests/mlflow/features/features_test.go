package features

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"."},
			TestingT: t,
			// Strict mode will fail immediately on undefined steps
			Strict: true,
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
