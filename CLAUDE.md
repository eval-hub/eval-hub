# Agent instructions

@AGENTS.md

## CVE fixing

### Instructions for CVE fixing

Find any CVEs in the repository dependencies and create a PR with the proposed fix in the repository.

Verify that there is not already an open `PR` that provides this fix, if an open `PR` already
exists then report the `PR` number and skip the rest.

#### Updating the golang version

Before updating to a new golang version check that this version is supported in the go-toolset that can be found here `registry.access.redhat.com/ubi9/go-toolset`. If the new golang version is not yet supported in `registry.access.redhat.com/ubi9/go-toolset` then move to the latest supported version, if possible, and report that the desired version is not yet supported by go-toolset.
The PR should also update the major golang version, if needed, in the Containerfile.

The go.mod must not be updated until the same version exists in go-toolset.

If there are other files in the repository that require updating due to new golang version then mention them in the PR.
Use `go-version-file: "go.mod"` in the github actions where possible.

#### npm devDependencies

Ensure that version pinning is correct and pins to a **single version**,
regenerate package-lock.json by running npm install after modifying the overrides in package.json.

If updating any dependencies related to `npm` then verify that the documentation
build still works by running `make documentation`.
If `make documentation` changes any files in the `docs` directory then add them to the `PR`.
