# Reusable workflows

## Synchronize a downstream fork

[`sync-fork.yml`](workflows/sync-fork.yml) updates a GitHub fork from its upstream
using GitHub's [fork-sync API](https://docs.github.com/en/rest/branches/branches#sync-a-fork-branch-with-the-upstream-repository).
Repositories in other organizations can call this public workflow when their
GitHub Actions policies allow it.

The destination must be a GitHub fork. The API synchronizes the selected branch
from the same-named upstream branch; it does not accept an arbitrary source
repository or source branch. For example, `opendatahub-io/eval-hub:main` is updated
from `eval-hub/eval-hub:main`. The API reads the latest upstream state when called,
so a run may include commits newer than the push that triggered it.

### Call from the upstream repository

Add this caller to the upstream repository to sync after each push to `main`,
including pull request merges:

```yaml
name: Sync downstream fork

on:
  push:
    branches: [main]
  workflow_dispatch: {}

permissions: {}

jobs:
  sync:
    # Prevent this caller from also running in forks that inherit the file.
    if: github.repository == 'eval-hub/eval-hub'
    uses: eval-hub/eval-hub/.github/workflows/sync-fork.yml@main
    with:
      target-repository: opendatahub-io/eval-hub
      target-branch: main
    secrets:
      sync-token: ${{ secrets.DOWNSTREAM_SYNC_TOKEN }}
```

Replace the repository names for other projects. Pin the workflow reference to
a reviewed commit SHA for stable behavior; `@main` tracks future workflow changes.
The shared workflow does not install this caller or enable synchronization itself.

### Credentials and permissions

Create `DOWNSTREAM_SYNC_TOKEN` as a repository secret in the caller, or as an
organization secret available to selected caller repositories. Pass it explicitly
as `sync-token`. Secrets in the repository hosting the reusable workflow are not
automatically available to callers.

Use a fine-grained personal access token or a GitHub App installation token with
**Contents: write** access to the destination fork. When upstream changes include
workflow files, the credential also needs permission to update workflows.
The upstream caller's `GITHUB_TOKEN` cannot write to a separate downstream
repository. GitHub App installation tokens expire after one hour, so callers
using them must supply a fresh token for each run.

The destination's branch protections and rulesets still apply. This workflow
directly updates the branch, so destinations requiring pull requests should use
a PR-based synchronization process instead.

### Call from the downstream repository

The same workflow can be called from the destination fork on a schedule or with
`workflow_dispatch`. An upstream push does not trigger workflows in the fork.
When using the destination's `GITHUB_TOKEN`, grant the caller `contents: write`
and pass `${{ secrets.GITHUB_TOKEN }}` explicitly as `sync-token`. Events caused
by that token generally do not trigger further workflows; use a GitHub App token
or PAT if the synchronized push must trigger downstream CI.

### Results and failures

Successful runs add the API's result, merge type, and upstream branch to the job
summary. An already synchronized fork is a successful no-op. Merge conflicts
(HTTP 409), insufficient permissions, and branch protection failures fail the job
with the API error visible in its log. Resolve conflicts manually and rerun the
caller. The workflow does not force-push or resolve conflicts automatically.

Runs targeting the same repository and branch are serialized within each caller
repository. GitHub Actions concurrency groups do not serialize runs across
different caller repositories, so use one caller per destination branch.
