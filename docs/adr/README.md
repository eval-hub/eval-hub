# Architecture Decision Records

ADRs describe significant EvalHub decisions using numbered files with context, decision, alternatives and consequences. A Proposed record does not imply an accepted or implemented feature.

## Index

- **0001 — reserved, not used.** The sandboxed evaluation runtime proposal in [PR 1059](https://github.com/eval-hub/eval-hub/pull/1059) was closed unmerged on 2026-10-19 without landing as an ADR. The number stays reserved and must not be reused for a different decision.
- **[0002 — Evaluation sandboxes through the internal OpenShell gateway](0002-evaluation-sandboxes-through-internal-openshell-gateway.md).** Proposed, 2026-10-19; revised 2026-11-18. Decisions 2 and 4 record the model decided with the OpenShift AI architecture team: one static sandbox namespace for all evaluation job sandboxes in the driver's `shared` workspace namespace mode, one OpenShell workspace per EvalHub tenant, one sandbox per evaluation job. References and analysis are aligned with PR 174 head `97a0c4d` (`ODH-ADR-Operator-0021`). The namespace-model record is still open across three outcomes: PR 174 amended to `shared`, an EvalHub-scoped `shared` gateway, or supersession by a managed/operator variant. Acceptance gates are grouped by capability area, each with a named owner.

## Conventions

Use `NNNN-kebab-case-title.md` and the next unused number. Include status, date, deciders and references. Statuses are Proposed, Accepted, Deprecated or Superseded by ADR-NNNN. Preserve accepted history: add a successor and link the supersession rather than rewriting the original decision. Add each new record to this index.
