package features

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/cucumber/godog"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

type testContext struct {
	client           *mlflowclient.Client
	experimentID     string
	experimentName   string
	lastError        error
	lastResponse     interface{}
	createdResources []resource
}

type resource struct {
	Type string
	ID   string
	Name string
}

func (ctx *testContext) reset() {
	ctx.experimentID = ""
	ctx.experimentName = ""
	ctx.lastError = nil
	ctx.lastResponse = nil
}

func (ctx *testContext) cleanup() {
	// Clean up created resources in reverse order
	for i := len(ctx.createdResources) - 1; i >= 0; i-- {
		resource := ctx.createdResources[i]
		switch resource.Type {
		case "experiment":
			err := ctx.client.DeleteExperiment(resource.ID)
			if err != nil {
				// we just report this, as this is just an attempt
				// to clean up the resource we don't fail the tests for an error here
				debugLog("Error deleting experiment %s: %s", resource.ID, err.Error())
			}
		}
	}
	ctx.createdResources = nil
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	tc := &testContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		tc.reset()
		return ctx, nil
	})

	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		tc.cleanup()
		return ctx, nil
	})

	// Server setup steps
	ctx.Step(`^an MLflow server is running$`, tc.serverIsRunning)
	ctx.Step(`^I have an MLflow client connected to the server$`, tc.clientConnected)

	// Experiment steps
	ctx.Step(`^I create an experiment named "([^"]*)"$`, tc.createExperiment)
	ctx.Step(`^the experiment should be created successfully$`, tc.experimentCreatedSuccessfully)
	ctx.Step(`^the experiment should have the name "([^"]*)"$`, tc.experimentHasName)
	ctx.Step(`^an experiment named "([^"]*)" exists$`, tc.experimentExists)
	ctx.Step(`^an experiment with a unique name exists$`, tc.experimentUniqueNameExists)
	ctx.Step(`^I get the experiment by ID$`, tc.getExperimentByID)
	ctx.Step(`^I get the experiment by name "([^"]*)"$`, tc.getExperimentByName)
	ctx.Step(`^the experiment should be returned$`, tc.experimentReturned)
	ctx.Step(`^multiple experiments exist$`, tc.multipleExperimentsExist)
	ctx.Step(`^I delete the experiment$`, tc.deleteExperiment)

	// Other steps
	ctx.Step(`^fix this step$`, tc.fixThisStep)
}

func debugLog(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	log.Println(msg)
}

// Server setup steps
func (tc *testContext) serverIsRunning() error {
	// If the client is already connected then no need to check again
	if tc.clientConnected() == nil {
		return nil
	}
	// For testing, we'll use an existing server if MLFLOW_TRACKING_URI
	testURL := os.Getenv("MLFLOW_TRACKING_URI")
	if testURL != "" {
		client := mlflowclient.NewClient(context.Background(), testURL)
		tc.client = client
		return nil
	}
	return fmt.Errorf("MLFLOW_TRACKING_URI is not set")
}

func (tc *testContext) clientConnected() error {
	if tc.client == nil {
		return fmt.Errorf("client not initialized")
	}
	return nil
}

func (tc *testContext) fixThisStep() error {
	debugLog("TODO: fix this step")
	return godog.ErrSkip
}
