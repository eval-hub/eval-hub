package features

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/cucumber/godog"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func (tc *scenarioConfig) infoMetadataMatchesConfiguredEnvironment() error {
	var response api.InfoResponse
	if err := json.Unmarshal(tc.body, &response); err != nil {
		return tc.logError(fmt.Errorf("decode service info response: %w", err))
	}

	fields := []struct {
		envName string
		field   string
		actual  string
	}{
		{envName: "TEST_INFO_VERSION", field: "version", actual: response.Version},
		{envName: "TEST_INFO_BUILD", field: "build", actual: response.Build},
		{envName: "TEST_INFO_BUILD_DATE", field: "build_date", actual: response.BuildDate},
		{envName: "TEST_INFO_GIT_HASH", field: "git_hash", actual: response.GitHash},
	}
	for _, field := range fields {
		if expected, set := os.LookupEnv(field.envName); set && expected != field.actual {
			return tc.logError(fmt.Errorf("service info %s = %q, want %q from %s", field.field, field.actual, expected, field.envName))
		}
	}
	return nil
}

func (tc *scenarioConfig) configuredTenantHardwareProfileShouldBeReturned() error {
	name := os.Getenv("TEST_INFO_HARDWARE_PROFILE_NAME")
	if name == "" {
		logDebug("Skipping scenario: TEST_INFO_HARDWARE_PROFILE_NAME is not set\n")
		return godog.ErrSkip
	}
	other := os.Getenv("TEST_INFO_OTHER_TENANT_HARDWARE_PROFILE_NAME")
	if other == name {
		return tc.logError(fmt.Errorf("other tenant hardware profile must differ from expected profile"))
	}
	var response api.InfoResponse
	if err := json.Unmarshal(tc.body, &response); err != nil {
		return tc.logError(fmt.Errorf("decode service info response: %w", err))
	}
	matches := 0
	for _, profile := range response.HardwareProfiles {
		if profile.Name == name {
			matches++
		}
		if other != "" && profile.Name == other {
			return tc.logError(fmt.Errorf("hardware profile %q from another tenant was returned", other))
		}
	}
	if matches != 1 {
		return tc.logError(fmt.Errorf("expected exactly one hardware profile named %q, got %d", name, matches))
	}
	return nil
}
