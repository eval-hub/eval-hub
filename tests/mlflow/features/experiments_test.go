package features

import (
	"fmt"
	"time"

	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
	"github.com/google/uuid"
)

// Experiment step implementations

func (tc *testContext) createExperiment(name string) error {
	tc.experimentName = name
	req := mlflowclient.CreateExperimentRequest{
		Name: name,
	}
	resp, err := tc.client.CreateExperiment(req)
	if err != nil {
		tc.lastError = err
		return err
	}
	tc.experimentID = resp.ExperimentID
	tc.createdResources = append(tc.createdResources, resource{Type: "experiment", ID: resp.ExperimentID, Name: name})
	return nil
}

func (tc *testContext) experimentCreatedSuccessfully() error {
	if tc.experimentID == "" {
		return fmt.Errorf("experiment ID is empty")
	}
	return nil
}

func (tc *testContext) experimentHasName(name string) error {
	exp, err := tc.client.GetExperiment(tc.experimentID)
	if err != nil {
		return err
	}
	if exp.Experiment.Name != name {
		return fmt.Errorf("expected experiment name %s, got %s", name, exp.Experiment.Name)
	}
	return nil
}

func (tc *testContext) experimentExists(name string) error {
	resp, err := tc.client.GetExperimentByName(name)
	if err != nil {
		debugLog("Error whilst getting experiment by name %s: %s", name, err.Error())
		return tc.createExperiment(name)
	}
	if resp.Experiment.LifecycleStage != "active" {
		return fmt.Errorf("experiment %s is not active", name)
	}
	if resp.Experiment.Name != name {
		return fmt.Errorf("expected experiment name %s, got %s", name, resp.Experiment.Name)
	}
	return nil
}

func (tc *testContext) experimentUniqueNameExists() error {
	return tc.experimentExists(fmt.Sprintf("test-experiment-%s", uuid.New().String()))
}

func (tc *testContext) getExperimentByID() error {
	if tc.experimentID == "" {
		return fmt.Errorf("no experiment ID set")
	}
	_, err := tc.client.GetExperiment(tc.experimentID)
	if err != nil {
		tc.lastError = err
		return err
	}
	return nil
}

func (tc *testContext) getExperimentByName(name string) error {
	_, err := tc.client.GetExperimentByName(name)
	if err != nil {
		tc.lastError = err
		return err
	}
	return nil
}

func (tc *testContext) experimentReturned() error {
	return nil // Already checked in getExperimentByID/getExperimentByName
}

func (tc *testContext) multipleExperimentsExist() error {
	// Create a few experiments
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("multi-exp-%d-%d", time.Now().Unix(), i)
		if err := tc.createExperiment(name); err != nil {
			return err
		}
	}
	return nil
}

func (tc *testContext) deleteExperiment() error {
	if tc.experimentID == "" {
		return fmt.Errorf("no experiment ID set")
	}
	err := tc.client.DeleteExperiment(tc.experimentID)
	if err != nil {
		return err
	}
	debugLog("Experiment %s deleted", tc.experimentID)
	return nil
}
