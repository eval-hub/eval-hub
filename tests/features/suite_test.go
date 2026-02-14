package features

import (
	"os"
	"path/filepath"
	"strings"
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

	paths := []string{featuresPath}
	if envPaths := os.Getenv("GODOG_PATHS"); envPaths != "" {
		paths = normalizePaths(splitPaths(envPaths), workDir)
	}

	format := os.Getenv("GODOG_FORMAT")
	if format == "" {
		format = "pretty"
	}

	var outputFile *os.File
	output := os.Stdout
	if outputPath := os.Getenv("GODOG_OUTPUT"); outputPath != "" {
		file, err := os.Create(outputPath)
		if err != nil {
			t.Fatalf("failed to create GODOG_OUTPUT file: %v", err)
		}
		outputFile = file
		output = file
	}
	if outputFile != nil {
		defer func() {
			_ = outputFile.Close()
		}()
	}

	suite := godog.TestSuite{
		TestSuiteInitializer: InitializeTestSuite,
		ScenarioInitializer:  InitializeScenario,
		Options: &godog.Options{
			Format:   format,
			Output:   output,
			Paths:    paths,
			TestingT: t,
			Strict:   true,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}

func splitPaths(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ':'
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
