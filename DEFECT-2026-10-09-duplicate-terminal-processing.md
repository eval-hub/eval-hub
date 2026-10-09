# Overlapping benchmark callbacks can run overall-job terminal processing twice

Suggested issue type: bug

Suggested priority: P2

## Problem

Two overlapping benchmark status callbacks can both start terminal processing for
the same evaluation job, even though only one callback's storage transaction
transitioned the overall job into a terminal state.

This is an existing race in the terminal callback logic. The OCI processing
changes expose an additional consequence: a duplicate export can overwrite a
successful OCI processing status with a failure. Fixing this defect is deferred
to a separate issue; this report does not implement the fix.

OCI processing still starts only after the overall evaluation job is completed.
The defect concerns which callback is allowed to start that processing, rather
than confusing individual benchmark completion with overall-job completion.

## Current code flow

The benchmark event HTTP handler, `HandleUpdateEvaluation`, performs three
separate operations in `internal/eval_hub/handlers/evaluations.go`:

1. Lines 709–711 read the overall job state before the benchmark update and save
   it as `previousState`.
2. Line 727 calls `scoped.UpdateEvaluationJob(evaluationJobID, status)`. Storage
   recalculates and persists the overall job state inside a transaction, but
   returns only an error.
3. Lines 746–748 invoke `onEvaluationJobUpdated` with a function that reads the
   job again after the update.

The runtime storage wrapper follows the same pattern in that file:
`runtimeStorage.UpdateEvaluationJob`, lines 76–97, reads the previous state,
updates storage, and supplies a fresh job read to the terminal callback.

In `internal/eval_hub/handlers/evaluation_terminal.go`, lines 21–29, the terminal
callback reads the latest job and proceeds when its state is terminal and differs
from the caller's saved `previousState`:

```go
job, err := getJob()
// Error and nil checks omitted here.
if !job.Status.State.IsTerminalState() || previousState == job.Status.State {
    return
}
h.exportEvaluationResults(ctx, storage, job, logger)
```

Storage knows the state produced by its own update. The caller does not receive
that information, and its subsequent read can include another callback's update.
The database lock serializes benchmark updates but does not cover the later
read and decision to start terminal processing.

Line numbers refer to the working tree reviewed on 2026-10-09 and may move.

## Triggering interleaving

Consider an overall job with two unfinished benchmarks, A and B:

| Step | Callback A | Callback B | Persisted overall job state |
| --- | --- | --- | --- |
| 1 | Reads `previousState = running` | Reads `previousState = running` | `running` |
| 2 | Completes benchmark A and commits; B is still unfinished | | `running` |
| 3 | Pauses before its post-update job read | Completes benchmark B and commits | `completed` |
| 4 | Reads the latest job as `completed` | Reads the latest job as `completed` | `completed` |
| 5 | Passes the terminal check | Passes the terminal check | `completed` |
| 6 | Starts terminal processing | Starts terminal processing | `completed` |

Both callbacks compare `running` with `completed`. Callback A should skip
terminal processing because its own transaction left the overall job running.
Only callback B should start it.

This requires overlapping callbacks for different benchmarks. A later callback
whose initial read already observes the terminal state is a different case and
is covered by the existing unchanged-terminal-state check.

## Impact and evidence

- MLflow and OCI evaluation-card exports can run twice.
- With the OCI processing changes, duplicate workflows can overwrite each other's
  progress. One workflow can publish successfully and record `completed`, then
  the other can fail and replace that status with `failed` while preserving the
  successful artifact reference.
- Other terminal actions, including terminal metrics, threshold notifications,
  and container log export, can also be invoked more than once.

The new `UpdateEvaluationJobOCI` implementation in
`internal/eval_hub/storage/sql/evaluations_oci.go` performs a locked merge but
unconditionally replaces the processing status. A row lock prevents simultaneous
writes; it does not establish ownership of an export workflow.

A temporary Go test overlay exercised the shared terminal callback twice with
`previousState = running`. The first export was paused, the second succeeded, and
then the first returned a publication error. The test observed two exports and a
final OCI state of `failed` with the successful artifact reference still present.

That reproducer validated the terminal callback consequence using test doubles.
It did not exercise the full concurrent benchmark-update path against SQL
storage. A deterministic regression test for that path is part of the proposed
fix below. The targeted package test suites passed during review; their existing
coverage did not detect this interleaving.

## Suggested fix

Return a transaction-derived outcome from `UpdateEvaluationJob` alongside its
error. For example:

```go
type EvaluationJobUpdateOutcome struct {
    BecameTerminal bool
}

UpdateEvaluationJob(id string, event *api.StatusEvent) (
    EvaluationJobUpdateOutcome, error,
)
```

Inside the storage transaction:

1. Capture the previous overall state from the locked job read, before modifying
   `job.Status.State`.
2. Apply the benchmark event, recalculate the overall job state, and persist the
   update as today.
3. Determine whether this transaction moved the job from a nonterminal state to
   a terminal state:

   ```go
   becameTerminal := !previousState.IsTerminalState() &&
       overallState.IsTerminalState()
   ```

4. Return the outcome to the caller only after the transaction commits
   successfully. Recompute the candidate outcome on each transaction retry;
   discard it on rollback, commit failure, or exhausted retries.

Update both the HTTP benchmark event handler and the runtime storage wrapper to
run terminal processing only when `outcome.BecameTerminal` is true. Their fresh
job read can still supply data for terminal processing, but must not determine
whether their update caused the transition.

Move the terminal metrics decision behind the same gate, along with results
export, threshold notifications, and container log export. Preserve normal
per-benchmark phase notifications and other update work regardless of whether
the overall job became terminal.

Update the relevant storage interfaces, implementations, and test doubles for
the new return value. Keep OCI's completed-only eligibility rule; other terminal
states must retain their existing behavior. Preserve the separate cancellation
path and its existing terminal metrics behavior.

This resolves the race at the shared storage/caller boundary for both Kubernetes
and local flows. It does not require separate mode-specific export locks or an
OCI-specific claim to address this particular interleaving. It also does not
provide durable export scheduling, restart recovery, or automatic retries.

## Acceptance criteria

- A deterministic test pauses callback A after its benchmark update commits with
  the overall state still `running`, lets callback B commit the final benchmark,
  and then resumes A's terminal decision. Only B starts terminal processing.
- Both the HTTP benchmark event caller and runtime storage wrapper use the
  committed transaction outcome as their gate.
- Terminal metrics, results exports, threshold notifications, and log export are
  invoked once for the tested transition, where enabled.
- Duplicate callbacks cannot change a successful OCI export to `failed` through
  the described interleaving.
- Failed updates and failed commits do not authorize terminal processing.
  Transaction retries return the outcome of the successfully committed attempt.
- Existing nonterminal updates, terminal-state guards, tenant scoping,
  cancellation behavior, and OCI export eligibility continue to work.
- Relevant tests pass, followed by the repository's required `make fmt lint`.
