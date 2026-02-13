package features

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
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

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
