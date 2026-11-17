# Temporary review notes: ADR 0002 against PR 174 head

> **Temporary working file — delete before merge.** This is a review artifact,
> not an ADR and not an approved decision. It captures the review of
> [ADR 0002](0002-evaluation-sandboxes-through-internal-openshell-gateway.md)
> against Open Data Hub PR 174 head on 2026-11-16.

- **ADR reviewed:** [0002 — Evaluation sandboxes through the internal OpenShell gateway](0002-evaluation-sandboxes-through-internal-openshell-gateway.md)
- **Plan reviewed:** [OpenShell evaluation plan](../design/openshell-evaluation-plan.md)
- **Platform reference:** PR 174 head `97a0c4d59b34e0f9d1e6327aa79f026178b73d60` (`ODH-ADR-Operator-0021-internal-openshell-gateway.md`)
- **PR 174 URL:** <https://github.com/opendatahub-io/architecture-decision-records/pull/174>
- **Second pass:** 2026-11-17 — Tier 1 (structure/editorial) and Tier 2 (gate and paragraph additions) follow-up applied to the ADR; see the status notes in **Remaining findings, ranked**.
- **Decision recorded:** 2026-11-17 — the architecture team decided one static sandbox namespace for all evaluation job sandboxes, one OpenShell workspace per EvalHub tenant (`X-Tenant`), and one sandbox per evaluation job.
- **Status:** working notes; remove before merge.

## What changed in this branch

The ADR and plan were aligned with PR 174 head, and the namespace-model divergence was made a first-class, reviewable amendment. The ADR's core choice was **not** changed from `shared` to `managed` mode.

| File | Change |
|---|---|
| `docs/adr/0002-evaluation-sandboxes-through-internal-openshell-gateway.md` | Replaced the stale `57bc18b`/`ODH-ADR-Operator-0019` pin with `97a0c4d`/`ODH-ADR-Operator-0021`; added the platform-alignment section (now **Platform alignment and namespace decision**, lines 28–45); corrected operator-mode and warn-and-allow statements; added OIDC/token-broker and least-privilege automation dependencies; clarified gateway-wide `workspace_mode`, shared-namespace scope, quota enforcement, broker capability scope, relay auth, secret delegation, storage/export and cleanup ownership; added alternatives, open questions, references and expanded acceptance gates. |
| `docs/design/openshell-evaluation-plan.md` | Updated the PR 174 pin to `97a0c4d`; made namespace scope conditional on the ADR amendment; corrected operator-fallback wording; aligned least-privilege identity wording; added the namespace-model amendment to Phase 0 and “Review decisions still required.” |
| `docs/adr/README.md` | Index records the PR 174 head alignment and the namespace-model amendment request. |

## Verification against PR 174 head

The ADR now reflects these head statements:

- **Goals:** “Isolate consuming services from one another through separate OpenShell workspaces, Kubernetes namespaces, identities, credentials, quotas, and policies.”
- **EvalHub integration:** “Each `SandboxedEvaluation` receives a short-lived OpenShell workspace and therefore a dedicated managed Kubernetes namespace.”
- **Isolation and policy:** “Each consuming service receives its own OpenShell workspace and OpenShell-managed Kubernetes namespace boundary.”
- **Mode choice:** `managed` mode is preferred; `operator` mode is the **required fallback** when managed mode cannot satisfy split-RBAC, namespace-read, ownership, admission, template-ordering or qualification requirements.
- **Identity:** OpenShell v0.1.4 does not accept OpenShift ServiceAccount tokens as ordinary gateway API credentials; a qualified OIDC issuer/token broker is required, or new `TokenReview` support.
- **Least privilege:** the sandbox lifecycle controller uses an automation role that does not grant unrestricted Platform Admin authority.
- **Local mode:** warn-and-allow is permitted but not required.
- **Required OpenShell changes:** the head has a table of capabilities that must be implemented upstream or carried downstream before the applicable mode is supportable.

ADR 0002 quotes the three namespace statements and explicitly states that decision 4 does not adopt PR 174's namespace boundary. It records the model decided on 2026-11-17: one installation-wide static sandbox namespace for all evaluation job sandboxes, one OpenShell workspace per EvalHub tenant, and one sandbox per evaluation job. An EvalHub-scoped static namespace (which would require a separate gateway) and PR 174's namespace per `SandboxedEvaluation` were considered and not chosen. It also records that `workspace_mode` is gateway-wide, so EvalHub cannot opt into `shared` independently of the gateway's other consumers.

## Re-done analysis

### Aligned and resolved

- **Stale reference:** fixed. The ADR no longer pins the 2026-10-16 draft.
- **Operator fallback:** corrected. `operator` is PR 174's required fallback, not an amendment requiring new platform approval.
- **Warn-and-allow:** correctly tied to PR 174 head's EvalHub-integration section.
- **OIDC/token broker:** now a hard dependency with fail-closed behavior.
- **Least-privilege automation role:** aligned with PR 174 head; the ADR no longer says the provisioning service holds unrestricted Platform Admin authority.
- **Quota/fairness:** now states that OpenShell v0.1.4 exposes no quota/eviction RPC, so per-attempt/per-tenant fairness needs a platform-owned admission/reservation component, with Kueue where available and an explicit eviction contract.
- **Broker scope:** now requires capability binding to the calling primary sandbox and rejection of sibling benchmarks' sandboxes/children.
- **Relay/secret delegation:** now requires relay authentication and destination allowlisting, and explicitly forbids cluster-wide Secret read by the provisioning service.
- **Storage/export/cleanup:** now defines the storage contract (`Delete` reclaim, encryption, PVC/PV verification), export/data-loss disposition, and aligns TTL/orphan cleanup with PR 174's sandbox lifecycle controller.
- **Alternatives/open questions/references:** added, including the missing “defer until ready,” “namespace per consumer/trust domain,” and “dedicated EvalHub gateway” options.
- **Acceptance gates:** expanded to cover the platform amendment, OIDC, interceptor readiness, sibling denial, relay auth, supervisor egress, export/data-loss, ordinary-Job regression, local-mode rejection and upgrade/rollback.

### Remaining findings, ranked

**Applied 2026-11-17 (Tier 1 + Tier 2 follow-up).** The acceptance gates are now a grouped checklist with owners and measurable criteria, including the required-OpenShell-changes mapping, data handling and recovery/availability; capability precedence is normative in decision 1 and links the plan's agent-target classification and error taxonomy; decision 7 states the data-handling requirement; Consequences carries a failure/retryability matrix; open questions are grouped by owner; the decision 4 clause is tightened. The same day, decisions 2–4 were rewritten to record the architecture-team decision (namespace, tenant workspace, one sandbox per job) and the "requested amendment" framing was removed. Findings keep their original rank and note below what still needs a decision or external input.

**P0 — resolved 2026-11-17.** The architecture team decided one static sandbox namespace for all evaluation job sandboxes, one OpenShell workspace per EvalHub tenant (`X-Tenant`) and one sandbox per evaluation job. The ADR records it in the front matter, in **Platform alignment and requested amendment** and in decisions 2–4; the remaining platform action, amending PR 174, is now the first acceptance gate rather than an open question. If the platform later reinstates namespace-per-`SandboxedEvaluation`, the managed- or operator-mode variant supersedes this record.

**Open — job-to-job containment inside a tenant workspace (new, 2026-11-17).** OpenShell authorization is per workspace, not per sandbox, so a broker that is a workspace user can act on every sandbox in its tenant's workspace. Separation between jobs of one tenant now rests on the broker's capability binding and the sandbox boundary. Routed to Security (ADR open question and isolation gate).

**Open — child task environments under one sandbox per job (new, 2026-11-17).** The ADR records "one sandbox per job" as one *primary* sandbox per job and flags that five Inspect benchmarks declare a child task environment. Whether children remain permitted needs confirmation (ADR open question; plan review decisions).

**Open — tenant workspace lifecycle (new, 2026-11-17).** Provisioning, membership reconciliation and decommissioning of tenant workspaces are platform-owned. They are recorded as an acceptance gate and an open question, not yet as an agreed contract.

**P1 — the `SandboxedEvaluation` contract is still not defined.** The ADR now says the API group/version, status conditions, idempotency key and compatibility policy must be recorded in the implementation plan and TrustyAI repository before any provider is enabled. That is the right requirement, but the plan still only sketches a `spec`; there is no schema, status/condition list, duplicate-dispatch rule or compatibility policy yet. This remains a Phase 2 delivery blocker.

**P1 — the "required OpenShell changes" mapping — gate applied; content open.** The mapping is now an acceptance gate owned by OpenShell integration and platform. Which both-mode rows (enforceable profile, identity/automation, policy, cleanup, availability) the supported release implements still needs upstream verification.

**P1 — data governance — applied.** Decision 7 now carries the classification, minimization, retention, geographic/regulatory and log-redaction requirement, forbids ungoverned capture of credentials, raw prompts, datasets, source files and model outputs, and a gate requires it before a provider is enabled. The qualified rules remain the open question owned by Security and EvalHub.

**P1 — HA, backup/restore and recovery — gates applied; values open.** A Recovery and availability gate group now requires a named platform owner, HA/cleanup-SLO/version-skew targets, restore/clone preservation of the gateway database, KEK, JWT signing key, PKI and stable gateway ID with namespace-ownership revalidation, and a fail-closed outage condition. The owner and the numeric targets remain platform inputs.

**P2 — provider capability precedence — applied.** Decision 1 now states the full precedence (administrator ceiling > provider minimum > collection request > user request), forbids a collection from moving a benchmark into or out of sandbox execution, and links the plan's agent-target classification and error taxonomy.

**P2 — failure matrix — applied; threat model still open.** Consequences now carries component failure → behavior → user-visible condition → retryability for gateway, interceptor, identity, admission, attestation, CNI, export and cleanup failures. The adversary/asset/trust-boundary/residual-risk table with Security sign-off is still missing.

**P2 — ownership and deciders are not named — open.** Gates and open questions now name the role that must evidence or answer each item, but no individuals or approvers are recorded and the front matter still lists organizations. Names are an external input, not a wording change.

**P2 — acceptance gates are one long sentence — applied.** The gates are now 23 checkboxes in six groups (platform contract and amendment, identity/admission/capacity, isolation/policy/credentials, lifecycle/storage/data handling, recovery and availability, provider qualification and performance), each with an owner and a measurable criterion.

**P3 — editorial — applied except the revision bullets.** Decision 4 now reads “installation-wide unless the platform chooses the EvalHub-scoped option,” and open questions are grouped by owner. The two same-date revision bullets stay: their `(references)`/`(placement)` labels already distinguish them, and merging them would produce one very long paragraph. The ADR still lacks a short decision summary and glossary; those remain optional.

## Validation performed

- `npx --yes markdownlint-cli2@0.17.2` on the ADR, these notes, the ADR README and the plan: **0 errors**, re-run 2026-11-17 after the follow-up pass.
- `git diff --check`: **clean**.
- All relative links resolve, including `#platform-alignment-and-namespace-decision`, `#open-questions` and the plan anchors `#agent-target-classification` and `#framework-compatibility`.
- External references checked: PR 174 commit `97a0c4d` (200), `ODH-ADR-Operator-0016` (200), EH-0002 (200), EH-0004 (200), PR 1059 (200).
- No stale `57bc18b`/`ODH-ADR-Operator-0019` references remain except the intentional historical note in the ADR References and plan Sources sections.
- The two SVG diagrams contain no dates and already label the dedicated shared sandbox namespace, so they did not need regenerating; verified 2026-11-17.

## Bottom line

The stale-reference blocker is fixed. The namespace and sandbox model is now decided and recorded: one static sandbox namespace for all evaluation job sandboxes, one OpenShell workspace per EvalHub tenant, one sandbox per evaluation job. The remaining P1 items are the `SandboxedEvaluation` contract/schema definition and the required-OpenShell-changes mapping content; the threat-model table, named approvers, the qualified data-handling rules, job-to-job containment sign-off and the child-task-environment question are the other open items. The only remaining platform-record action is amending PR 174 to match.

## Remove before merge

This file is intentionally temporary. Delete it (or move the findings into the PR discussion) before merging ADR 0002. It is kept for now because the open items above (the platform amendment, the `SandboxedEvaluation` contract, the required-change mapping content, the threat model and the named approvers) still need a disposition record. Nothing links to it, so `git rm docs/adr/0002-review-notes.md` is the whole cleanup once those items are dispositioned.
