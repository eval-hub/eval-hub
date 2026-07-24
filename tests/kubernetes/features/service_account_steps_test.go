package features

import (
	"fmt"
	"os"
	"strings"

	"github.com/cucumber/godog"
)

// ============================================================================
// Service Account & Environment Steps
// ============================================================================

func (tc *testContext) mlflowIsConfigured() error {
	if os.Getenv("MLFLOW_TRACKING_URI") == "" {
		return godog.ErrSkip
	}
	return nil
}

func (tc *testContext) environmentVariableIsSet(name, value string) error {
	actual, ok := os.LookupEnv(name)
	if os.Getenv("K8S_TEST_DEBUG") == "true" {
		fmt.Printf("[DEBUG] checking env %s (set=%t)\n", name, ok)
	}
	if !ok {
		return fmt.Errorf("environment variable %s is not set", name)
	}
	if actual != value {
		return fmt.Errorf("environment variable %s expected %q, got %q", name, value, actual)
	}
	return nil
}

func (tc *testContext) containerCommandShouldBeValidArray() error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	if len(container.Command) == 0 {
		return fmt.Errorf("Container %s has no command", container.Name)
	}

	return nil
}

func (tc *testContext) containerCommandShouldNotContainEmptyStrings() error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	for _, cmd := range container.Command {
		if cmd == "" {
			return fmt.Errorf("Container %s command contains empty string", container.Name)
		}
	}

	return nil
}

func (tc *testContext) containerCommandShouldHaveTrimmedWhitespace() error {
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}
	for _, cmd := range container.Command {
		if strings.TrimSpace(cmd) != cmd {
			return fmt.Errorf("Container %s command element has untrimmed whitespace: %q", container.Name, cmd)
		}
	}

	return nil
}

func (tc *testContext) containerShouldHaveProviderEnvVars() error {
	// Validates that provider environment variables are present
	// This is a general check that the container has env vars
	container, err := tc.firstContainer()
	if err != nil {
		return err
	}

	// Just verify that there are env vars
	if len(container.Env) == 0 {
		return fmt.Errorf("Container %s has no environment variables", container.Name)
	}

	return nil
}
