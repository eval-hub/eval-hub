# Documentation catalog update notifications

`notify-docs-catalog-update.yml` sends a `repository_dispatch` event of type
`catalogs-updated` to `eval-hub/eval-hub.github.io` when a push to `main` changes
any file under `config/collections/`, including additions,
deletions, and renames. It can also be run manually from `main` to retry a
notification. Runs in forks and on other branches do not send events.

## Setup

1. Create a fine-grained personal access token scoped to
   `eval-hub/eval-hub.github.io` with **Contents: write** permission. Obtain any
   organization approval required for the token.
2. Store it as the Actions secret `DOCS_DISPATCH_TOKEN` in `eval-hub/eval-hub`,
   or as an organization secret available to this repository. The built-in
   `GITHUB_TOKEN` cannot dispatch to another repository.
3. In the documentation repository, add the following trigger to the existing
   deployment workflow on its default branch, alongside its existing triggers:

   ```yaml
   on:
     repository_dispatch:
       types: [catalogs-updated]
   ```

The event's `client_payload` contains `repository` and `sha` for traceability.
The receiver should rebuild and publish the documentation using its existing
`npm run build` pipeline. This workflow only sends the notification; it does
not change catalog generation or wait for the documentation deployment.

The notification fails if the secret is missing or the API request fails.
After configuring both repositories, use **Actions → Notify documentation of
catalog changes → Run workflow**, select `main`, and verify that the deployment
workflow runs in the documentation repository.

See GitHub's documentation for [repository dispatch events](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#repository_dispatch)
and [the dispatch API and token permissions](https://docs.github.com/en/rest/repos/repos#create-a-repository-dispatch-event).
