# Temporary review notes: ADR 0002 against PR 174 head

> **Temporary working file — delete before merge.** This is a review artifact,
> not an ADR and not an approved decision. It captures the review of
> [ADR 0002](0002-evaluation-sandboxes-through-internal-openshell-gateway.md)
> against Open Data Hub PR 174 head on 2026-11-16.

- **ADR reviewed:** [0002 — Evaluation sandboxes through the internal OpenShell gateway](0002-evaluation-sandboxes-through-internal-openshell-gateway.md)
- **Plan reviewed:** [OpenShell evaluation plan](../design/openshell-evaluation-plan.md)
- **Platform reference:** PR 174 head `97a0c4d59b34e0f9d1e6327aa79f026178b73d60` (`ODH-ADR-Operator-0021-internal-openshell-gateway.md`)
- **PR 174 URL:** <https://github.com/opendatahub-io/architecture-decision-records/pull/174>
- **Status:** working notes; remove before merge.

## What changed in this branch

The ADR and plan were aligned with PR 174 head, and the namespace-model divergence was made a first-class, reviewable amendment. The ADR's core choice was **not** changed from `shared` to `managed` mode.

| File | Change |
|---|---|
| `docs/adr/0002-evaluation-sandboxes-through-internal-openshell-gateway.md` | Replaced the stale `57bc18b`/`ODH-ADR-Operator-0019` pin with `97a0c4d`/`ODH-ADR-Operator-0021`; added **Platform alignment and requested amendment** (lines 28–45); corrected operator-mode and warn-and-allow statements; added OIDC/token-broker and least-privilege automation dependencies; clarified gateway-wide `workspace_mode`, shared-namespace scope, quota enforcement, broker capability scope, relay auth, secret delegation, storage/export and cleanup ownership; added alternatives, open questions, references and expanded acceptance gates. |
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

ADR 0002 quotes the three namespace statements and explicitly states that decision 4 does not adopt PR 174's namespace boundary. It asks the platform to choose either:

1. an **installation-wide** static sandbox namespace for all shared-mode consumers, or
2. an **EvalHub-scoped** static namespace, which requires a separate gateway (or a supported multi-static-namespace driver capability).

It also records that `workspace_mode` is gateway-wide, so EvalHub cannot opt into `shared` independently of the gateway's other consumers.

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

**P0 — the platform amendment is unresolved.** The ADR is now honest about it, but it cannot be Accepted until OpenShift AI platform and Security choose installation-wide vs EvalHub-scoped, or insist on PR 174's namespace-per-`SandboxedEvaluation`. If they insist on the latter, this ADR must be **superseded by a managed- or operator-mode variant**. No wording change can fix that; it is a platform decision.

**P1 — the `SandboxedEvaluation` contract is still not defined.** The ADR now says the API group/version, status conditions, idempotency key and compatibility policy must be recorded in the implementation plan and TrustyAI repository before any provider is enabled. That is the right requirement, but the plan still only sketches a `spec`; there is no schema, status/condition list, duplicate-dispatch rule or compatibility policy yet. This remains a Phase 2 delivery blocker.

**P1 — the “required OpenShell changes” mapping is incomplete.** PR 174 head splits its capability table into managed-mode gates and rows that apply to both modes. The ADR correctly says shared mode avoids the managed-mode rows, but it does not map which both-mode rows (enforceable profile, identity/automation, cleanup/availability) are implemented in the supported release. An open question now exists; it should also be an acceptance gate with an owner.

**P1 — data governance is only in open questions.** PR 174 head requires consuming services to document data classification, minimization, retention, geographic/regulatory constraints and log redaction, and forbids platform telemetry from capturing credentials, raw prompts, datasets, source files or model outputs without governance. The ADR mentions the question but does not put the requirement in the decision or gates. Add a short data-handling paragraph and a gate.

**P1 — HA, backup/restore and recovery are only in open questions.** The ADR now asks who owns the gateway/SLC and how DB/KEK/JWT/PKI/gateway ID survive restore or clone, but the acceptance gates do not require backup/restore, version-skew or gateway-ID preservation tests. PR 174 head treats these as platform readiness requirements.

**P2 — provider capability precedence is still not normative.** The ADR says sandboxing is resolved from trusted provider/task configuration and that request-time lowering is forbidden, but it does not state the full precedence: administrator ceiling > provider minimum > collection request > user request. It also does not reference the plan's agent-target classification (7 `evaluation_targets: [agent]` benchmarks across 3 providers) or the error taxonomy. Add one paragraph and link the plan section.

**P2 — failure matrix and threat model are still missing.** The Consequences section now covers gateway/identity outages, cost and provider coverage, and the adjacent sections cover many controls, but there is no compact matrix of component failure → behavior → user-visible condition → retryability. There is also no explicit adversary/asset/trust-boundary/residual-risk sign-off table, which is important given Security is a decider.

**P2 — ownership and deciders are not named.** The ADR says components “must have named operational ownership” but does not name the owners, and the front matter lists organizations rather than approvers. PR 174 head has a stakeholder table and a reviews table. Add the same for EvalHub.

**P2 — acceptance gates are one long sentence.** They are comprehensive now but hard to review. Convert them to a checklist grouped by owner/phase, with measurable pass criteria (for example, “supervisor egress denied on the supported CNI,” “cross-sibling broker call returns PermissionDenied,” “PVC and PV absent after teardown”).

**P3 — editorial.** The front matter now has two same-date revision bullets; they are labeled `(references)` and `(placement)`, so that is acceptable. The `Decision §4` phrase “installation-wide for shared-mode consumers unless…” could be tightened to “installation-wide unless the platform chooses the EvalHub-scoped option.” The `Open questions` list is long; grouping by owner would help. The ADR still lacks a short decision summary and glossary, but those are optional now that the platform-alignment section exists.

## Validation performed

- `npx --yes markdownlint-cli2@0.17.2` on the ADR, ADR README and plan: **0 errors**.
- `git diff --check`: **clean**.
- All relative links resolve, including the new `#platform-alignment-and-requested-amendment` anchors.
- External references checked: PR 174 commit `97a0c4d` (200), `ODH-ADR-Operator-0016` (200), EH-0002 (200), EH-0004 (200), PR 1059 (200).
- No stale `57bc18b`/`ODH-ADR-Operator-0019` references remain except the intentional historical note in the ADR References and plan Sources sections.
- The SVG diagrams were not regenerated in this pass because the `.mmd` sources did not change; they still contain the 2026-11-16 shared-namespace labels.

## Bottom line

The stale-reference blocker is fixed and the ADR now accurately reflects PR 174 head. The remaining blocker is the platform namespace-model decision, which the ADR now states explicitly and routes to platform/Security. If the platform accepts an installation-wide shared namespace, the remaining P1 items are contract/schema definition, required-change mapping, data governance and recovery gates. If it retains namespace-per-`SandboxedEvaluation`, this ADR should be superseded by a managed/operator variant.

## Remove before merge

This file is intentionally temporary. Delete it (or move the findings into the PR discussion) before merging ADR 0002.
