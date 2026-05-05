---
paths:
  - "tests/**"
name: fix-fvt-test
description: Finds the root cause of a FVT test failure, as well as a PR when possible to fix the error, by looking at the code for the service and tests, as well as looking at the test logs, and if necessary by looking at the cluster pod logs. Use when trying to analyze and fix FVT test failures on an OpenShift AI cluster, or when the user asks "fix this failing FVT test"
allowed-tools:
  - Read
  #- Edit
  #- Write
  - WebFetch
  - Bash(grep *)
  - Bash(find *)
  - Bash(git *)
  - Bash(gh *)
  - Bash(sed *)
  - Bash(go *)
  - Bash(make *)
  #- Bash(oc *)

disable-model-invocation: true
# Scripted workflow skill; keep disabled so the agent follows steps instead of delegating to another model.
---

# When proposing a fix, always include the following

1. **Ask the user for a url to the failing tests**: this will normally be a JUnit XML test report, if there are multiple failing tests ask the user if he or she wants to focus on a specific failing test
2. **Read the full test output log**: you will need to prompt the user to get the log file
3. **Read the pod logs if they are available**: you will need to prompt the user to get the pod logs. If you detect problems related to the execution of the jobs, such as `connection error` then ask for the pod logs
4. **Ask for the oc login command**: as a last resort, ask the user for the oc login command to read the pod logs, no other oc commands can be run without explicit agreement from the user
5. **Source repositories**: use the source code and the OpenAPI repositories that are mentioned in the `Additional resources` section, normally the `main` branch will be used but the user may specify a branch to use
6. **Provide a full root cause analysis**: describe in detail the problem that is causing the failures
7. **Provide a PR to fix the issue**: if a fix is possible ask the user if you should propose a PR. The fix can either be in the server code, or the python sdk code, or in the FVT test code, then create a PR using conventional commit format with a detailed description of the fix

Provide a detailed root cause where possible, keep the proposed fix as simple as possible.

Note that files may be URLs to download or pasted content into the chat.

## Additional resources

- The service code is in the repository <https://github.com/eval-hub/eval-hub>
- OpenAPI spec (raw, suitable for tools): [openapi.json](https://raw.githubusercontent.com/eval-hub/eval-hub/main/docs/openapi.json) · [openapi.yaml](https://raw.githubusercontent.com/eval-hub/eval-hub/main/docs/openapi.yaml) — or browse in-repo under `docs/`
- The FVT tests and test code are in the repository <https://github.com/eval-hub/eval-hub>
- The Python adapter code is in the repository <https://github.com/eval-hub/eval-hub-sdk>
