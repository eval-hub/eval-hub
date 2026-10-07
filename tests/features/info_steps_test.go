package features

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

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

func (tc *scenarioConfig) configuredTenantQueueShouldBeReturned() error {
	queueName, set := os.LookupEnv("TEST_INFO_QUEUE_NAME")
	if !set || queueName == "" {
		logDebug("Skipping scenario: TEST_INFO_QUEUE_NAME is not set\n")
		return godog.ErrSkip
	}
	otherTenantQueueName := os.Getenv("TEST_INFO_OTHER_TENANT_QUEUE_NAME")
	if otherTenantQueueName != "" && otherTenantQueueName == queueName {
		return tc.logError(fmt.Errorf("TEST_INFO_OTHER_TENANT_QUEUE_NAME %q must differ from TEST_INFO_QUEUE_NAME", otherTenantQueueName))
	}

	type queueInfo struct {
		Name    string `json:"name"`
		Active  *bool  `json:"active"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	var response struct {
		Queues []queueInfo `json:"queues"`
	}
	if err := json.Unmarshal(tc.body, &response); err != nil {
		return tc.logError(fmt.Errorf("decode service info response: %w", err))
	}

	var matches []queueInfo
	for _, queue := range response.Queues {
		if queue.Name == queueName {
			matches = append(matches, queue)
		}
	}
	if len(matches) != 1 {
		return tc.logError(fmt.Errorf("expected exactly one queue named %q, got %d in response", queueName, len(matches)))
	}
	queue := matches[0]
	if queue.Active == nil {
		return tc.logError(fmt.Errorf("queue %q response does not include active state", queueName))
	}

	if expected, set := os.LookupEnv("TEST_INFO_QUEUE_ACTIVE"); set {
		want, err := strconv.ParseBool(expected)
		if err != nil {
			return tc.logError(fmt.Errorf("invalid TEST_INFO_QUEUE_ACTIVE %q: %w", expected, err))
		}
		if *queue.Active != want {
			return tc.logError(fmt.Errorf("queue %q active = %t, want %t", queueName, *queue.Active, want))
		}
	}
	if expected, set := os.LookupEnv("TEST_INFO_QUEUE_REASON"); set && queue.Reason != expected {
		return tc.logError(fmt.Errorf("queue %q reason = %q, want %q", queueName, queue.Reason, expected))
	}
	if expected, set := os.LookupEnv("TEST_INFO_QUEUE_MESSAGE"); set && queue.Message != expected {
		return tc.logError(fmt.Errorf("queue %q message = %q, want %q", queueName, queue.Message, expected))
	}

	if otherTenantQueueName != "" {
		for _, queue := range response.Queues {
			if queue.Name == otherTenantQueueName {
				return tc.logError(fmt.Errorf("queue %q from another tenant was returned", otherTenantQueueName))
			}
		}
	}
	return nil
}
