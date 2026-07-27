# Upgrade Tests

BDD-style upgrade tests using [godog](https://github.com/cucumber/godog). These tests verify that evaluation resources survive an RHOAI upgrade.

## Phases

| Makefile target            | Tag                  | Description                                        |
|----------------------------|----------------------|----------------------------------------------------|
| `run-pre-upgrade`          | `@pre-upgrade`       | Create evaluation jobs on the source version       |
| `run-post-upgrade-verify`  | `@post-upgrade-verify` | Verify resources survived the upgrade            |
| `run-post-upgrade`         | `@post-upgrade`      | Run new evaluations on the target version          |
| `run-post-upgrade-cleanup` | `@post-upgrade-cleanup` | Clean up all test resources                     |

## Pre-upgrade

Creates an evaluation job, waits for completion, then collects all jobs on the cluster into a state file (`test-reports/upgrade-state.json`) for use by post-upgrade phases.

## Required environment variables

| Variable                | Description                          |
|-------------------------|--------------------------------------|
| `SERVER_URL`            | EvalHub API base URL                 |
| `AUTH_TOKEN`            | Bearer token for authentication      |
| `X_TENANT`              | Tenant namespace                     |
| `MODEL_URL`             | Model inference endpoint URL         |
| `MODEL_NAME`            | Model name                           |

## Optional environment variables

| Variable                | Description                          |
|-------------------------|--------------------------------------|
| `MODEL_AUTH_SECRET_REF` | Secret reference for model auth (omits `model.auth` from the job payload if unset) |
| `X_USER`                | User identity header (default: `upgrade-test-user`) |
| `UPGRADE_STATE_JSON`    | State file path (default: `test-reports/upgrade-state.json`) |

## Required Makefile variables

| Variable                | Description                          |
|-------------------------|--------------------------------------|
| `JUNIT_XML`             | Output path for JUnit XML report (required by all make targets) |

### Running

Export the required environment variables, then execute the make target for each step.

```bash
export SERVER_URL=https://....openshiftapps.com
export X_TENANT=dataplane
export MODEL_URL=https://vllm-sim2-evalhub-test.....openshiftapps.com/v1
export MODEL_NAME=ibm-granite/granite-3.3-2b-instruct
export MODEL_AUTH_SECRET_REF=vllm-sim2-secret
export AUTH_TOKEN="..."
```

### make run-pre-upgrade

```bash
SERVER_URL=$SERVER_URL \
AUTH_TOKEN=$AUTH_TOKEN \
X_TENANT=$X_TENANT \
MODEL_URL=$MODEL_URL \
MODEL_NAME=$MODEL_NAME \
MODEL_AUTH_SECRET_REF=$MODEL_AUTH_SECRET_REF \
UPGRADE_STATE_JSON=test-reports/upgrade-state.json \
JUNIT_XML=test-reports/pre-upgrade.xml \
make run-pre-upgrade
```

### make run-post-upgrade-verify

This step is dependent on UPGRADE_STATE_JSON generated from the prior step of run-pre-upgrade

```bash
SERVER_URL=$SERVER_URL \
AUTH_TOKEN=$AUTH_TOKEN \
X_TENANT=$X_TENANT \
UPGRADE_STATE_JSON=test-reports/upgrade-state.json \
JUNIT_XML=test-reports/post-upgrade-verify.xml \
make run-post-upgrade-verify
```

### make run-post-upgrade

This can run independent of other targets.

```bash
SERVER_URL=$SERVER_URL \
AUTH_TOKEN=$AUTH_TOKEN \
X_TENANT=$X_TENANT \
MODEL_URL=$MODEL_URL \
MODEL_NAME=$MODEL_NAME \
MODEL_AUTH_SECRET_REF=$MODEL_AUTH_SECRET_REF \
JUNIT_XML=test-reports/post-upgrade.xml \
make run-post-upgrade
```


### make run-post-upgrade-cleanup

Clean up all evaluation jobs which have "*-upgrade-*" in their job name. This can run independent of other targets.

```bash
SERVER_URL=$SERVER_URL \
AUTH_TOKEN=$AUTH_TOKEN \
X_TENANT=$X_TENANT \
JUNIT_XML=test-reports/post-upgrade-cleanup.xml \
make run-post-upgrade-cleanup
```

### make run-atris-upgrade

Quick dev target that chains all four phases in order. Each phase overwrites the same `JUNIT_XML` file, so only the last phase's report is kept. CI pipelines and `scripts/upgrade-test.sh` use per-phase `JUNIT_XML` paths instead.

```bash
SERVER_URL=$SERVER_URL \
AUTH_TOKEN=$AUTH_TOKEN \
X_TENANT=$X_TENANT \
MODEL_URL=$MODEL_URL \
MODEL_NAME=$MODEL_NAME \
MODEL_AUTH_SECRET_REF=$MODEL_AUTH_SECRET_REF \
UPGRADE_STATE_JSON=test-reports/upgrade-state.json \
JUNIT_XML=test-reports/upgrade.xml \
make run-atris-upgrade
```

### Generated files

- `test-reports/upgrade-state.json` from `UPGRADE_STATE_JSON` -- all evaluation jobs on the cluster (id, name, state), used by post-upgrade phases
- `$JUNIT_XML` -- JUnit XML test report for each make target.

## Running all phases with the upgrade script

The `scripts/upgrade-test.sh` script automates the steps listed above. It
validates that the required environment variables are set, passes `JUNIT_XML`
and `UPGRADE_STATE_JSON` automatically, and reports results under `test-reports/`.

Run all four phases in order:

```bash
source .env
./scripts/upgrade-test.sh
```

Run only specific phases:

```bash
./scripts/upgrade-test.sh pre-upgrade                       # single phase
./scripts/upgrade-test.sh post-upgrade-verify post-upgrade   # multiple phases
```

Run `./scripts/upgrade-test.sh --help` for the full list of options.
