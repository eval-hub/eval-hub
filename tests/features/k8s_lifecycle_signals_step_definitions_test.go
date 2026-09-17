package features

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

type lifecycleSignalsState struct {
	k8s            *fvtK8sClient
	jobName        string
	lastPhaseLabel string
}

func (s *lifecycleSignalsState) reset() {
	s.k8s = nil
	s.jobName = ""
	s.lastPhaseLabel = ""
}

func (s *lifecycleSignalsState) initHelper() error {
	if s.k8s != nil {
		return nil
	}
	client, err := newFVTK8sClient()
	if err != nil {
		return fmt.Errorf("create fvt kubernetes client: %w", err)
	}
	s.k8s = client
	return nil
}

func (s *lifecycleSignalsState) resolveJobName(tc *scenarioConfig) string {
	if s.jobName != "" {
		return s.jobName
	}
	if tc.lastK8sJobName != "" {
		s.jobName = tc.lastK8sJobName
		return s.jobName
	}
	return ""
}

func isJobNotFoundError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no Kubernetes Job found")
}

// iObserveKubernetesEventWithReasonWithin polls the Events API until an event with the given
// reason appears on the Kubernetes Job backing the current evaluation job, or the timeout elapses.
// The backing Job may already be deleted after failure; Events remain addressable by Job name.
func (tc *scenarioConfig) iObserveKubernetesEventWithReasonWithin(state *lifecycleSignalsState, reason, timeoutStr string) error {
	if tc.lastId == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found; submit a job before asserting events"))
	}
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return tc.logError(fmt.Errorf("parse timeout %q: %w", timeoutStr, err))
	}
	if err := state.initHelper(); err != nil {
		return tc.logError(err)
	}

	jobName := state.resolveJobName(tc)
	if jobName == "" {
		return tc.logError(fmt.Errorf(
			"no cached Kubernetes Job name for eval job %s; call 'I wait for the Kubernetes evaluation Job to be created' before asserting events",
			tc.lastId,
		))
	}

	namespace := tc.tenantNamespace()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	logDebug("Waiting for Kubernetes Event reason=%s on Job %s/%s within %s\n", reason, namespace, jobName, timeoutStr)

	if err := state.k8s.waitForEventOnJob(ctx, namespace, jobName, reason); err != nil {
		return tc.logError(err)
	}
	if reason == "EvaluationFailed" {
		readCtx, readCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer readCancel()
		if actual, readErr := state.k8s.getJobPhaseLabel(readCtx, namespace, tc.lastId); readErr == nil {
			state.lastPhaseLabel = actual
			logDebug("Read phase label %q from Kubernetes Job after EvaluationFailed event\n", actual)
		}
	}
	logDebug("Observed Kubernetes Event reason=%s on Job %s\n", reason, jobName)
	return nil
}

// theEvaluationJobShouldHaveLabelEqualTo does an immediate (non-polling) check that the label
// on the Kubernetes Job backing the current evaluation job matches the expected value.
func (tc *scenarioConfig) theEvaluationJobShouldHaveLabelEqualTo(state *lifecycleSignalsState, labelKey, expectedValue string) error {
	if tc.lastId == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}
	if err := state.initHelper(); err != nil {
		return tc.logError(err)
	}
	namespace := tc.tenantNamespace()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if labelKey == "trustyai.opendatahub.io/evaluation-phase" {
		actual, err := state.k8s.getJobPhaseLabel(ctx, namespace, tc.lastId)
		if isJobNotFoundError(err) {
			if state.lastPhaseLabel == expectedValue {
				logDebug(
					"Kubernetes Job for eval job %s already deleted; using verified cached phase label %q\n",
					tc.lastId, state.lastPhaseLabel,
				)
				return nil
			}
			if state.lastPhaseLabel == "" {
				return tc.logError(fmt.Errorf(
					"Kubernetes Job for eval job %s was deleted before label %s=%s could be verified (phase label unverified)",
					tc.lastId, labelKey, expectedValue,
				))
			}
			return tc.logError(fmt.Errorf(
				"Kubernetes Job for eval job %s was deleted before label %s=%s could be verified (last read phase: %q)",
				tc.lastId, labelKey, expectedValue, state.lastPhaseLabel,
			))
		}
		if err != nil {
			return tc.logError(err)
		}
		if actual != expectedValue {
			return tc.logError(fmt.Errorf("expected label %s=%s on eval job %s, got %q", labelKey, expectedValue, tc.lastId, actual))
		}
		state.lastPhaseLabel = actual
		logDebug("Verified label %s=%s on Kubernetes Job for eval job %s\n", labelKey, actual, tc.lastId)
		return nil
	}
	return tc.logError(fmt.Errorf("unsupported label key %q in lifecycle signal step", labelKey))
}

// iWaitForEvaluationJobToHaveLabelEqualToWithin polls until the label on the backing Kubernetes
// Job equals expectedValue or the timeout elapses.
func (tc *scenarioConfig) iWaitForEvaluationJobToHaveLabelEqualToWithin(state *lifecycleSignalsState, labelKey, expectedValue, timeoutStr string) error {
	if tc.lastId == "" {
		return tc.logError(fmt.Errorf("no evaluation job ID found"))
	}
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return tc.logError(fmt.Errorf("parse timeout %q: %w", timeoutStr, err))
	}
	if err := state.initHelper(); err != nil {
		return tc.logError(err)
	}

	namespace := tc.tenantNamespace()

	if labelKey != "trustyai.opendatahub.io/evaluation-phase" {
		return tc.logError(fmt.Errorf("unsupported label key %q in lifecycle signal step", labelKey))
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	logDebug("Waiting for label %s=%s on eval job %s within %s\n", labelKey, expectedValue, tc.lastId, timeoutStr)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastObserved string
	for {
		select {
		case <-ctx.Done():
			if lastObserved == expectedValue {
				state.lastPhaseLabel = lastObserved
				if state.jobName == "" {
					state.jobName = tc.lastK8sJobName
				}
				logDebug(
					"Label %s=%s observed on eval job %s before Job deletion\n",
					labelKey, expectedValue, tc.lastId,
				)
				return nil
			}
			return tc.logError(fmt.Errorf(
				"timeout waiting for label %s=%s on eval job %s (last observed: %q)",
				labelKey, expectedValue, tc.lastId, lastObserved,
			))
		default:
		}

		actual, err := state.k8s.getJobPhaseLabel(ctx, namespace, tc.lastId)
		if err == nil {
			lastObserved = actual
			if actual == expectedValue {
				state.lastPhaseLabel = actual
				if name, nameErr := state.k8s.getJobNameForEvalJob(ctx, namespace, tc.lastId); nameErr == nil {
					state.jobName = name
					tc.lastK8sJobName = name
				} else if tc.lastK8sJobName != "" {
					state.jobName = tc.lastK8sJobName
				}
				logDebug("Label %s=%s observed on eval job %s\n", labelKey, actual, tc.lastId)
				return nil
			}
		} else if isJobNotFoundError(err) && lastObserved == expectedValue {
			state.lastPhaseLabel = lastObserved
			if state.jobName == "" {
				state.jobName = tc.lastK8sJobName
			}
			logDebug(
				"Label %s=%s observed on eval job %s before Job deletion\n",
				labelKey, expectedValue, tc.lastId,
			)
			return nil
		}

		select {
		case <-ctx.Done():
			if lastObserved == expectedValue {
				state.lastPhaseLabel = lastObserved
				if state.jobName == "" {
					state.jobName = tc.lastK8sJobName
				}
				logDebug(
					"Label %s=%s observed on eval job %s before Job deletion\n",
					labelKey, expectedValue, tc.lastId,
				)
				return nil
			}
			return tc.logError(fmt.Errorf(
				"timeout waiting for label %s=%s on eval job %s (last observed: %q)",
				labelKey, expectedValue, tc.lastId, lastObserved,
			))
		case <-ticker.C:
		}
	}
}

// InitializeLifecycleSignalSteps registers the Kubernetes lifecycle signal FVT steps.
func InitializeLifecycleSignalSteps(ctx *godog.ScenarioContext, tc *scenarioConfig) {
	state := &lifecycleSignalsState{}

	ctx.Before(func(goCtx context.Context, sc *godog.Scenario) (context.Context, error) {
		for _, t := range sc.Tags {
			if strings.TrimPrefix(t.Name, "@") == "k8s_lifecycle" {
				state.reset()
				break
			}
		}
		return goCtx, nil
	})

	ctx.Step(
		`^I observe a Kubernetes Event with reason "([^"]*)" for the evaluation job within "([^"]*)"$`,
		func(reason, timeout string) error {
			return tc.iObserveKubernetesEventWithReasonWithin(state, reason, timeout)
		},
	)
	ctx.Step(
		`^the evaluation Job should have label "([^"]*)" equal to "([^"]*)"$`,
		func(labelKey, expected string) error {
			return tc.theEvaluationJobShouldHaveLabelEqualTo(state, labelKey, expected)
		},
	)
	ctx.Step(
		`^I wait for the evaluation Job to have label "([^"]*)" equal to "([^"]*)" within "([^"]*)"$`,
		func(labelKey, expected, timeout string) error {
			return tc.iWaitForEvaluationJobToHaveLabelEqualToWithin(state, labelKey, expected, timeout)
		},
	)
}
