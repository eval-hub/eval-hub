# ADR 0001: Sandboxed evaluation runtime for agentic frameworks

- **Status:** Proposed
- **Date:** 2026-09-30
- **Deciders:** EvalHub team
- **Jira:** [RHAISTRAT-2289](https://redhat.atlassian.net/browse/RHAISTRAT-2289) (Feature), child of Outcome [RHAISTRAT-2574](https://redhat.atlassian.net/browse/RHAISTRAT-2574); Epics RHAI-731…737
- **Components:** `eval-hub`, `trustyai-service-operator`

## Context

Some evaluation frameworks — SWE-Bench, AgentDojo, ATIF/Harbor, Terminal-Bench,
ProgramBench — require the evaluated agent to **spawn and control pods** (per-tool,
per-sub-agent, or code-execution pods) and stream to them. These agents may be
adversarial or untrusted. Running them on the current unisolated job path would let
an agent reach the cloud/node metadata endpoint (`169.254.169.254`), arbitrary
network destinations, and — via a kernel exploit — the host.

Today eval jobs run as plain `batchv1.Job`s built in
[job_builders.go](../../internal/eval_hub/runtimes/k8s/job_builders.go#L153), created
imperatively into the tenant namespace. Per-container securityContext is hardened
(drop ALL caps, non-root, `seccompProfile: RuntimeDefault`) and
`automountServiceAccountToken: false` is set. Because no ServiceAccount token is
mounted, a job **cannot authenticate to the Kubernetes API server** today — the
current path does not grant API-server access. What is still missing is **namespace
isolation, NetworkPolicy, egress control, `RuntimeClassName`, pod-spawn mediation,
and any per-job lifecycle object the operator can watch**: an agent can still reach
the network (including the metadata endpoint) and, with a kernel exploit, the host.
The operator's evalhub controller provisions per-tenant SA/RoleBindings/ConfigMaps
but builds no eval pods and creates no sandbox primitives. **There is no sandbox
broker binary today.**

We need an isolation model that contains an adversarial agent's blast radius,
exfiltration, and (where warranted) host-kernel breakout, while leaving
non-sandboxed evaluations unaffected.

## Decision

Adopt a **namespace-first sandbox** with a **broker** as the pod-spawning mechanism and
an **operator-owned namespace lifecycle**. Concretely:

1. **Namespace isolation is the P0 primary boundary.** Each sandboxed evaluation runs
   in a dedicated **ephemeral namespace** with ResourceQuota + LimitRange, a
   default-deny egress NetworkPolicy, namespace-scoped RBAC, and baseline Pod Security
   Admission. **Standard `runc` is an acceptable baseline runtime.**

2. **A sandbox broker is the core P0 deliverable.** A new fifth Go binary,
   `eval-sandbox-broker`, runs as a **sidecar in the eval pod** and exposes a
   restricted, K8s-API-compatible REST + SPDY/WebSocket surface (`create`/`get`/`delete`
   pods, `exec`/`log`/`attach`) scoped to the sandbox namespace. It holds a
   namespace-scoped ServiceAccount token (mounted only into the broker container) and
   rejects `privileged`/`hostPath`/`hostNetwork`/`hostPID`/`hostIPC`, non-allowlisted
   images, and cross-namespace requests. It enforces fan-out/depth/TTL caps.

3. **The operator owns the namespace lifecycle.** eval-hub creates a per-job
   `SandboxedEvaluation` CR instead of creating the Job directly; the
   trustyai-service-operator reconciles it into the ephemeral namespace (namespace →
   PSA labels → quota → NetworkPolicy → broker SA/Role → image-allowlist admission →
   `status.namespaceReady`). eval-hub waits for `namespaceReady`, then creates the Job
   into the provisioned namespace, and deletes the CR on completion; a finalizer drives
   teardown. Governance (orphan reconciliation, TTL reaper, max-concurrent cap) lives in
   the operator.

4. **Sandbox is a cluster + provider property, never a per-job option.** The outcome is
   purely (provider requires sandbox) × (EvalHub CR has sandbox management enabled).
   There is no job/collection API field to request, opt out of, or select a tier.

5. **Sandbox-or-fail on a cluster; warn-and-allow in local mode.** On a Kubernetes
   cluster, a sandbox-requiring provider with sandbox management disabled → rejected at
   create time (`SandboxRequiredButNotConfigured`). A cluster with sandbox management
   enabled but no Kata still runs on the `runc` namespace baseline rather than being
   rejected. **Local mode has no namespace/NetworkPolicy/broker primitives, so it cannot
   enforce the sandbox** — but the user, not the service, decides whether to run an agent
   evaluation there. The run is **not** rejected: eval-hub emits a clear warning that the
   evaluation is executing **unsandboxed** (no isolation, egress control, or pod-spawn
   mediation) and that this is unsafe for adversarial or untrusted agents, then proceeds.
   The warning is surfaced in logs and recorded in the evaluation's provenance cards.

6. **OpenShell (P1) and Kata (P2) are enhancements layered on the P0 baseline, applied
   when available, not hard requirements.** The P0 MVP ships the namespace + broker +
   `runc` baseline described above; OpenShell (P1) and then Kata (P2) are added
   afterwards and attach to that baseline without replacing it (see **Roadmap and
   phasing** below). The broker/job-builder sets `RuntimeClassName` only when the cluster
   advertises a Kata runtime class. A provider may declare `min_isolation_tier: kata` so a
   genuinely adversarial provider is rejected on a Kata-less cluster while ordinary
   sandbox providers are not. Label-based simulated internet (M6) is a further future
   layer that attaches to this baseline.

7. **Egress: FQDN allowlist plus mandatory blocks.** Default-deny egress except kube-dns
   and named evaluation infrastructure; the Kubernetes API server and
   `169.254.169.254` are explicitly blocked. FQDN-based egress is preferred where the
   CNI supports it, with CIDR NetworkPolicy shipped for portability.

8. **The sandbox is CPU-only; LLMs are consumed over the network.** Models are served
   via KServe `LLMInferenceService` (± AI Gateway) as in-cluster HTTP endpoints; the
   agent gets one egress-allowlist entry for the endpoint. **In-sandbox GPU passthrough
   is out of scope**; a sandbox-requiring provider co-located with `K8sRuntime.GPU` is
   rejected.

9. **Provenance is recorded.** The EvaluationCard and EnvironmentCard record the
   effective sandbox config: `isolation_tier`, `namespace`, `psa_level`,
   `resource_limits`, egress/image allowlists, `network_sealed`, and a broker-spawned
   child-pod audit list.

### Ownership split

- **eval-hub** owns: the `eval-sandbox-broker` binary, the provider `sandbox` config
  section, job-builder integration, the two-phase CR handshake, pre-flight validation,
  and card provenance.
- **trustyai-service-operator** owns: the `SandboxedEvaluation` CRD reconciler,
  ephemeral-namespace lifecycle (quota/NetworkPolicy/RBAC/PSA/admission),
  finalizer-driven teardown, and operational governance.

### Image allowlist and running an evaluation

**What it is.** The image allowlist is the set of container images the broker and the
operator's admission check will permit to run inside a sandbox namespace. Any pod the
agent asks the broker to spawn must reference an allowlisted image (by registry/repository,
and ideally pinned by digest); anything else is rejected before it is created. This exists
because the sandbox contains *behaviour* at the network/namespace boundary but does not
inspect image contents — restricting *which* images can run is what stops an adversarial
agent from pulling an arbitrary image (a crypto-miner, a toolchain with a known escape,
an image baking in its own credentials) into the sandbox.

**Who defines it.** The allowlist is cluster/provider configuration, not a per-job field
(consistent with decision 4). Images come from two sources: the fixed eval-hub
infrastructure images (the eval runner, the broker sidecar, init containers) and the
images a provider declares it needs for its sub-pods (for example a code-execution image
for SWE-Bench). Operators curate the final allowlist for their cluster.

**How a user runs their evaluation.** Users do not pass raw image references at request
time. They select a provider/collection whose required images are already part of that
provider's `sandbox` config, and the evaluation runs using those pre-vetted images. If an
evaluation needs an image that is not yet allowlisted, the workflow is to get that image
added to the provider config / cluster allowlist (a review step owned by the operator),
**not** to supply an arbitrary image on the API. This keeps "what can run in a sandbox" a
governed, auditable decision rather than attacker-controlled input. The effective
allowlist for each run is recorded in the provenance cards (see decision 9).

### Cross-repo contract

The `SandboxedEvaluation` CRD is a hard, versioned interface between the two repos. A
startup capability check (CRD present + served version) makes a mismatch fail fast
rather than hang on `namespaceReady`.

### Roadmap and phasing

The work is delivered in priority order. Each phase is additive: it builds on the
previous baseline rather than replacing it, so a cluster can stop at any phase and still
have a coherent, usable isolation model.

**P0 — MVP (this ADR's committed scope).** Namespace-first sandbox: ephemeral namespace
per evaluation, ResourceQuota + LimitRange, default-deny egress NetworkPolicy, baseline
Pod Security Admission, the `eval-sandbox-broker` sidecar for mediated pod spawning, the
operator-owned `SandboxedEvaluation` lifecycle, and an image allowlist. Runs on standard
`runc`.

- **What P0 gives you:** blast-radius containment at the namespace/network boundary —
  no uncontrolled pod spawning, no Kubernetes-API abuse, no cloud-metadata theft, no
  arbitrary network egress, deterministic cleanup.
- **What is missing until P1/P2:** egress control is L3/L4 only — no per-process/per-binary
  egress and no L7/DNS inspection (closed by OpenShell, P1); and the agent still shares the
  host kernel (`runc`), so a kernel-level exploit can still break out to the node (closed by
  Kata, P2).

**P1 — OpenShell (in-sandbox policy enhancement).** Adds the OpenShell in-sandbox
policy layer on top of the P0 namespace. OpenShell gives **per-binary / per-process
egress control and finer-grained in-sandbox syscall and command policy**, closing the
"any allowed-egress destination is a C2/exfil channel" gap that P0 leaves open at L3/L4.
It layers onto the existing `batchv1.Job` + broker path rather than replacing pod
creation (contrast the rejected "OpenShell-provisioned pods" alternative below).

- **What P1 adds:** process-aware egress and command policy inside the sandbox.
- **Still missing until Kata (P2):** shared-kernel breakout is still possible under
  `runc`.

**P2 — Kata (kernel-isolation enhancement).** Adds hardware-virtualization isolation by
setting `RuntimeClassName` to a Kata runtime class when the cluster advertises one. Each
sandbox pod then runs in its own lightweight VM with a **separate guest kernel**, so a
kernel LPE no longer escapes to the host node.

- **What P2 (Kata) adds:** closes the shared-kernel breakout residual risk; enables
  `min_isolation_tier: kata` so genuinely adversarial providers can require it.
- **Availability caveat:** Kata needs bare metal or nested virtualization and is
  unavailable on most managed cloud; where it is absent, evaluations fall back to the
  P0 `runc` baseline (unless a provider mandates `min_isolation_tier: kata`, in which
  case they are rejected).

**M6 — Label-based simulated internet (future).** Reuses the NetworkPolicy generator to
present a controlled, simulated egress environment.

## Alternatives considered

- **Kata-first (superseded).** Kernel isolation as the mandatory P0 control, rejecting
  the job outright when Kata is unavailable. Rejected: too strict for the general case,
  unavailable on most managed cloud (needs bare metal / nested virt), and misaligned with
  RHAISTRAT-2289, which makes the namespace baseline the primary boundary. The Kata
  analysis is retained as a P2 enhancement and per-provider tier.

- **Per-job NetworkPolicy in the existing tenant namespace** (no dedicated namespace).
  Rejected: does not give deterministic, finalizer-driven cleanup, per-job RBAC/quota
  scoping, or a home for governance; namespace churn is the price of that determinism.

- **eval-hub creates the namespace itself** (no operator/CR handshake). Rejected: only a
  controller-owned CR gives finalizer-based teardown, orphan reconciliation, and the
  concurrency governance in RHAI-737.

- **Credential-less broker relay.** In the chosen design the broker sidecar holds a
  namespace-scoped ServiceAccount token (mounted only into the broker container) and
  talks to the Kubernetes API server directly. The credential-less variant removes that
  token from the eval pod entirely: instead of the in-pod broker authenticating to the
  API server, the broker would forward each pod-spawn/exec request **out of the sandbox**
  to a trusted relay running in the operator (or a dedicated control-plane component)
  that holds the credential and performs the API call on the sandbox's behalf after
  re-validating it. The benefit is that no API-server credential ever lives inside the
  (potentially compromised) sandbox pod, so a broker compromise cannot replay or exfil a
  token — the blast radius collapses to whatever the relay re-authorizes. The costs are a
  new always-on relay component, an extra network hop and protocol on every pod
  operation, and a second place that must implement the same validation. Rejected as the
  baseline for this reason, but **retained as a documented hardening variant**: the
  Feature's choice is a namespace-scoped SA on the broker, with blast radius bounded by
  the narrow Role (namespace-scoped, no cross-namespace or privileged verbs) and the
  operator's admission validation as a second line of defence.

- **OpenShell-provisioned pods** (delegate pod creation to the OpenShell K8s driver +
  agent-sandbox controller). Rejected for the MVP: replaces the `batchv1.Job` path and
  adds a new control-plane dependency. OpenShell is kept as a P1 in-sandbox policy
  enhancement.

- **In-sandbox GPU** (Kata GPU passthrough, gVisor + nvproxy). Rejected: out of scope;
  the sandbox contains the agent's actions, not token generation, so LLMs are
  network-served.

## Consequences

### Positive

- Closes the day-to-day holes: K8s-API abuse, cloud-metadata theft, uncontrolled pod
  spawning, and arbitrary network exfil, on a standard `runc` cluster.
- Deterministic, controller-owned namespace cleanup and governance.
- Non-sandboxed evaluations keep the current synchronous `Job.Create` golden path
  verbatim; the two-phase logic is entered only for sandbox-requiring providers.
- Extensible: isolation tiers (`namespace` | `kata`, room for `openshell`), an
  extensible card block, and a NetworkPolicy generator that the M6 simulated-internet
  layer reuses.

### Negative / costs

- **Dispatch model change.** Sandbox-requiring providers move from synchronous
  Job creation to an asynchronous two-phase (CR → `namespaceReady` → Job) flow, adding
  wait states, timeouts, rollback, a dynamic/typed CRD client + informer + RBAC in
  eval-hub, and a versioned cross-repo contract. This is the largest, riskiest change in
  the plan.
- **Namespace churn** must be capped and orphan-reconciled or it becomes an availability
  risk.
- **Broker is a privileged mediator**: its validation correctness (image allowlist,
  securityContext, namespace confinement) is load-bearing; the admission policy is the
  belt-and-braces second line.

### Residual risks (accepted for the MVP)

- **Shared-kernel `runc` baseline**: a kernel LPE through an allowed syscall escapes to
  the host; closed only by the Kata enhancement or `min_isolation_tier: kata`.
- **Allowed egress and DNS (port 53) are exfil/C2 channels**; closing needs L7 (OSSM) or
  per-binary egress (OpenShell) — deferred.
- **L3/L4 only**; no L7 or DNS-rebinding protection.
- **The CNI plugin is part of the TCB (Trusted Computing Base).** The CNI (Container
  Network Interface) plugin is the cluster component that wires up pod networking and
  actually enforces Kubernetes `NetworkPolicy` rules in the kernel/dataplane. The "TCB"
  is the set of components that must be correct and uncompromised for the security
  guarantee to hold — here, our entire egress seal (default-deny plus the allowlist)
  depends on the CNI honouring those policies. If the cluster runs a CNI that does
  **not** enforce egress NetworkPolicy (some CNIs ignore egress rules, or enforce
  ingress only), the egress seal silently does nothing and the agent can reach anywhere,
  even though the policy objects exist. The sandbox therefore assumes an egress-enforcing
  CNI such as **OVN-Kubernetes** (the default on OpenShift); deployment on a
  non-enforcing CNI is an accepted, documented risk.
- **Configuration and supply-chain beyond the image allowlist are trusted** (a poisoned
  but allowlisted image still runs); the cards record — they do not prevent.

## References

- Plan: [EvalHub-Agent-Sandbox-Plan-v3.md](../../EvalHub-Agent-Sandbox-Plan-v3.md)
- Architecture: [EvalHub architecture](https://github.com/opendatahub-io/architecture-context/blob/main/architecture/rhoai.next/eval-hub.md)
- Future layer: [RHAISTRAT-2289](hhttps://redhat.atlassian.net/browse/RHAISTRAT-2289) (label-based simulated internet, M6)
