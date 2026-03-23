package features

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/spf13/pflag"
)

var opts = godog.Options{
	Output:   colors.Colored(os.Stdout),
	Format:   "junit:../../bin/junit-fvt.xml", // can define default values
	Strict:   true,
	Tags:     "~@ignore",
	Paths:    []string{"."},
	TestingT: nil,
}

// func TestFeatures(t *testing.T) {
func TestMain(m *testing.M) {
	godog.BindCommandLineFlags("godog.", &opts)

	pflag.Parse()
	opts.Paths = pflag.Args()

	if serverURL := os.Getenv("SERVER_URL"); serverURL != "" {
		// t.Logf("Running FVT tests against the server %s", serverURL)
	}

	// Get the absolute path to the features directory
	// When running from project root, use "tests/features", when from features dir, use "."
	workDir, _ := os.Getwd()
	// t.Log("Working directory:", workDir)
	var featuresPath string
	if filepath.Base(workDir) == "features" {
		featuresPath = "."
	} else {
		featuresPath = filepath.Join(workDir, "tests", "features")
	}

	paths := []string{featuresPath}
	if envPaths := os.Getenv("GODOG_PATHS"); envPaths != "" {
		paths = normalizePaths(splitPaths(envPaths), workDir)
	}
	opts.Paths = paths

	tags := os.Getenv("GODOG_TAGS")
	if tags != "" {
		opts.Tags = tags
	}

	suite := godog.TestSuite{
		Name:                 "EvalHub Feature Tests",
		TestSuiteInitializer: InitializeTestSuite,
		ScenarioInitializer:  InitializeScenario,
		Options:              &opts,
	}

	if st := suite.Run(); st != 0 {
		// t.Fatal("non-zero status returned, failed to run feature tests", st)
		os.Exit(st)
	}
}

func splitPaths(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';'
	})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

func normalizePaths(paths []string, workDir string) []string {
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, normalizePath(trimmed, workDir))
	}
	return normalized
}

func normalizePath(path string, workDir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	// If running from tests/features, allow "tests/features/..." input.
	if filepath.Base(workDir) == "features" && strings.HasPrefix(path, "tests/features/") {
		trimmed := strings.TrimPrefix(path, "tests/features/")
		if _, err := os.Stat(trimmed); err == nil {
			return trimmed
		}
	}
	joined := filepath.Join(workDir, path)
	if _, err := os.Stat(joined); err == nil {
		return joined
	}
	return path
}
