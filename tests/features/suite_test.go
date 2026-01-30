package features

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	if serverURL := os.Getenv("SERVER_URL"); serverURL != "" {
		t.Logf("Running FVT tests against the server %s", serverURL)
	}
	// Get the absolute path to the features directory
	// When running from project root, use "tests/features", when from features dir, use "."
	workDir, _ := os.Getwd()
	t.Log("Working directory:", workDir)
	var featuresPath string
	if filepath.Base(workDir) == "features" {
		featuresPath = "."
	} else {
		featuresPath = filepath.Join(workDir, "tests", "features")
	}

	suite := godog.TestSuite{
		TestSuiteInitializer: InitializeTestSuite,
		ScenarioInitializer:  InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{featuresPath},
			TestingT: t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}
