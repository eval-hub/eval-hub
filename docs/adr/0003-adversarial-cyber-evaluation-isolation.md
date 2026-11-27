# ADR 0003: Dedicated isolation for adversarial cyber evaluations

- **Status:** Proposed
- **Date:** 2026-11-27
- **Deciders:** EvalHub, TrustyAI Service Operator, OpenShift AI platform and Security (named approvers still to be recorded)
- **Strategy:** [RHAISTRAT-2289](https://redhat.atlassian.net/browse/RHAISTRAT-2289)
- **Amends:** [ADR 0002](0002-evaluation-sandboxes-through-internal-openshell-gateway.md). Decisions 2, 4, 6 and 7 keep their current meaning for sandbox providers outside the class defined here, and that record's [Platform alignment and namespace decision](0002-evaluation-sandboxes-through-internal-openshell-gateway.md#platform-alignment-and-namespace-decision) gains one class-scoped outcome. No other decision changes.
- **Revised:** 2026-11-27 (EvalHub-owned gateway) — decision 2 no longer treats the class gateway as a platform amendment. Under the EvalHub-owned deployment option recorded in [ADR 0002](0002-evaluation-sandboxes-through-internal-openshell-gateway.md#deployment-option-an-evalhub-owned-gateway), the class gateway is a deployment choice inside EvalHub's own installation; the recorded mode, the namespace-controller owner and the attestation evidence are still required. Decisions 3–10 are unchanged.
- **Depends on:** [shared internal OpenShell gateway proposal, PR 174](https://github.com/opendatahub-io/architecture-decision-records/pull/174), reviewed at [`97a0c4d`](https://github.com/opendatahub-io/architecture-decision-records/commit/97a0c4d59b34e0f9d1e6327aa79f026178b73d60) (`ODH-ADR-Operator-0021-internal-openshell-gateway.md`), and the OpenShell v0.1.10 pin recorded in ADR 0002.
- **Implementation plan:** [OpenShell evaluation plan](../design/openshell-evaluation-plan.md).

## Context

This record uses *run* for one evaluation job of a benchmark in the class below: the unit one accepted request creates (`EvaluationJobConfig` in [pkg/api/evaluations.go](../../pkg/api/evaluations.go)), the same lifecycle unit ADR 0002 calls an evaluation job. A *run namespace* and a *run workspace* are that run's Kubernetes namespace and OpenShell workspace. The *class* is the set of benchmarks defined in decision 1.

### What the class is

Some evaluation benchmarks do not merely execute untrusted code. Their scored objective is to attack a system, escape containment or misuse access that was granted for a legitimate purpose. The review that prompted this record names exploit-development and cyber-range suites, capture-the-flag suites, sandbox-escape suites and cyber red-team suites: ExploitGym, Cybersec CTF evaluations, SandboxEscapeBench, Cyber-Rage and Anthropic's Siren. The published containment-incident reports from OpenAI with Hugging Face, from Anthropic with three partner organizations, and from the UK AI Safety Institute describe the same failure mode: an evaluated agent leaving its intended boundary, or using authorized access as an attack, and exfiltrating material. This record treats those reports as design inputs. It does not restate their incident details and no decision below depends on their accuracy; the primary sources must be linked when the record is reviewed.

The catalog already contains members. [`config/providers/inspect.yaml`](../../config/providers/inspect.yaml) declares `inspect/cybench` (cybersecurity capture-the-flag, `evaluation_targets: [agent]`) and `inspect/cybergym` (automated cyber attack and defence exercises, `evaluation_targets: [agent]`), and the implementation plan's [Agent-target classification](../design/openshell-evaluation-plan.md#agent-target-classification) already records both as primary evaluator plus child sandbox. Under ADR 0002 alone, both would be dispatched into the installation-wide shared sandbox namespace.

### Why ADR 0002 is not sufficient for it

ADR 0002 assumes untrusted code and bounds it with a sandbox, a workspace, admission, a per-job broker capability and teardown. It deliberately accepts job-to-job risk inside one tenant workspace, keeps that workspace and its templates and provider bindings across jobs, and runs every sandbox of every tenant in one long-lived namespace. Three properties of this class break that model, and none of them is a payload difference that a stronger policy alone fixes.

1. **The run needs its own network.** A cyber range needs a simulated internet: web applications, mail servers, DNS, database services, a CMS and whatever else the exercise declares. The agent must reach those services and nothing else, and the services must reach each other. The pinned driver does the opposite: at v0.1.10 it creates one namespace-wide `NetworkPolicy` that denies all workload egress and permits only supervisor Pods to reach the sandbox TLS port, supervisors keep permissive egress, `enable_gateway_service_routing` is off by default, and exposed-service routing is disabled. A single static namespace with that fence cannot host a working range.
2. **The run's success condition is often persistence.** Leaving a message, a file, a log entry, an annotation or a scheduled action that a later run or session reads and continues from is a documented escape technique, and it is exactly what these suites reward. Every legitimate write surface therefore becomes a coordination channel. ADR 0002's retained substrate — the long-lived tenant workspace, its templates, its provider and credential bindings, the installation-wide namespace, the shared logs and provenance, and the relay's MLflow and OCI destinations — is the substrate a later run would read.
3. **The blast radius is not one job.** A shared namespace, workspace or identity lets one run influence another run, another tenant on the same instance or another instance on the same gateway. Containing the job is not containing the class.

The OpenShell release also constrains the answer. `workspace_mode` is gateway-wide, so one gateway cannot be `shared` for ordinary providers and per-run for this class. `managed` creates and deletes a generated namespace and ServiceAccount per workspace; `operator` maps workspace names to pre-provisioned namespaces discovered by label selector or allowlist and fails closed when a workspace is not listed. Either mode maps one namespace to one workspace. The driver's fence also means that, whichever mode is chosen, replacing the namespace-wide workload-egress rule needs a supported mechanism or a higher-precedence cluster control, and an ordinary additive `NetworkPolicy` cannot remove an existing allowance.

### What ADR 0002 already settles for this class

These carry over unchanged and are not re-decided here: no unsandboxed Job fallback and no request-time way to lower a provider's requirements (decision 1); a provider may declare Kata and must then fail closed without a qualified runtime (decision 1); trusted per-job brokers and relays outside evaluated code, in the instance's adapter namespace (decision 3, [The adapter namespace](0002-evaluation-sandboxes-through-internal-openshell-gateway.md#the-adapter-namespace)); no original Secret, gateway credential or pod-creation token in evaluated code (decision 5); `Delete`-reclaim storage only, with no snapshots and no retained volumes (decision 6); the normative teardown order, `CleanupComplete`, idempotent finalizers and durable orphan records (decision 7); and sandbox execution off unless `spec.sandbox` enables it against a platform-published qualified contract (decision 8).

What ADR 0002 cannot settle is the per-run namespace and its destruction, because its decision 4 declines that model for the shared path, and it cannot mix workspace modes on one gateway. That is the gap this record fills.

## Decision

EvalHub decides that benchmarks in the adversarial cyber class do not run in the installation-wide sandbox namespace at all. Each of their evaluation jobs runs in **its own run namespace and its own run workspace**, with the simulated internet deployed inside that boundary, on a **class-scoped OpenShell gateway** using a **mandatory Kata profile**, and the namespace and everything in it is **destroyed and verified gone** after the run. ADR 0002 keeps its current meaning for every other sandbox provider. Decisions 1–10 record the classification, the gateway, the per-run allocation, the simulation topology, the runtime requirement, the no-reuse and covert-channel rules, the destruction and quarantine rules, the capacity model, the enablement gate, and the scope limit that keeps per-run isolation off ordinary agent evaluations. All ten fail closed.

### 1. Classify the class from trusted onboarding metadata and gate dispatch on it

Define the class as **`adversarial_cyber`**: benchmarks whose evaluated agent is asked to attack, escape or misuse the environment, including exploit development, capture-the-flag, sandbox escape and cyber red-team exercises against a simulated environment. The classification is provider and benchmark onboarding metadata, in the same precedence as ADR 0002's decision 1: an administrator may add or remove a benchmark, a provider minimum may only tighten, and no request input — collection, benchmark, evaluation request or tenant value — can classify, declassify, retarget or bypass it.

`inspect/cybench` and `inspect/cybergym` are the current members. Membership carries three binding consequences: the benchmark is dispatched only through the class gateway (decision 2), it requires `profile: kata` (decision 5), and it is rejected rather than queued when the class contract is not qualified. A request may select the benchmark; it cannot select a different isolation model.

Local mode rejects the class outright, on the same stricter basis ADR 0002 applies to mandatory-sandbox providers: a process on a developer machine is never reported as class isolation. When the class contract is absent or unqualified, the evaluation is rejected at onboarding and at dispatch with `AdversarialIsolationUnavailable`, a row this record adds to the user-visible taxonomy in the plan's [Framework compatibility](../design/openshell-evaluation-plan.md#framework-compatibility). There is no fallback to the shared sandbox namespace, to a plain Job, or to warn-and-allow.

### 2. Give the class its own gateway and record its workspace mode

Because `workspace_mode` is gateway-wide, the class cannot share the installation-wide gateway: enabling per-run namespaces there would change the mode for every ordinary sandbox provider, which is exactly what ADR 0002's decision 4 declines. The class therefore has **one platform-managed gateway of its own**, qualified and attested separately from the shared gateway, and the platform records exactly one of two modes:

- **`managed`** — the driver creates and deletes a generated namespace (`openshell-{gateway_id}-{workspace}`) and a ServiceAccount per run workspace. Namespace lifecycle is automated, and the cost is that the gateway holds cluster-scoped namespace create and delete authority, PR 174 head's managed-mode gates apply (namespace templates, managed-mode RBAC partitioning, the managed-namespace ownership handshake), the first sandbox in each namespace waits on a readiness barrier, and every run pays OpenShift SCC/UID allocation.
- **`operator`** — workspace names map to pre-provisioned run namespaces discovered through a label selector or allowlist, and the driver fails closed for a workspace that is not listed. Namespace authority stays with a platform namespace controller, at the cost of an external create/label/configure/delete lifecycle per run and an allowlist that must stay race-free: a namespace must be listed before its workspace exists and de-listed before it is deleted.

Either mode is acceptable; leaving the mode unrecorded is not. The class contract names the mode, the namespace controller, the qualified Kata `RuntimeClass`, the simulation profile and the namespace/workspace naming rules, and it is published and consumed on the same terms as ADR 0002's [gateway interaction contract](0002-evaluation-sandboxes-through-internal-openshell-gateway.md#gateway-interaction-contract-extension) — by the platform, or by the EvalHub deployment under ADR 0002's [EvalHub-owned gateway option](0002-evaluation-sandboxes-through-internal-openshell-gateway.md#deployment-option-an-evalhub-owned-gateway), in which case the class gateway is a deployment choice inside EvalHub's own installation rather than a platform amendment, and the mode, the namespace-controller owner and the attestation evidence still have to be recorded and signed off by Security. Because `workspace_mode` is gateway-wide, the class needs a gateway in its own mode: today that is a second gateway, unless the ordinary sandbox path itself moves to per-run namespaces, the option decision 10 declines for now. If only a shared-mode gateway is available, the class is **not enabled** on that installation, and sandbox-required dispatch for a class benchmark into the shared namespace is prohibited in every case, including as a degraded mode.

### 3. Allocate one run workspace and one run namespace per evaluation job

Every evaluation job in the class gets exactly one run workspace and exactly one run namespace, created for that job and for no other purpose. Nothing in the boundary is reused across jobs, benchmarks, tenants, EvalHub instances or repeats of the same benchmark: not the namespace, not the workspace, not the ServiceAccount or its projected token, not the SCC-assigned UID or MCS range, not the storage, not the templates, not the provider or credential bindings, and not the run's identity.

The run namespace may hold more than one sandbox and more than one workload. It holds the job's primary sandbox, one sandbox or workload per simulated service the profile declares, and their supervisors. ADR 0002's one-primary-sandbox-per-job rule and its open question about child task environments are therefore resolved for this class: children are permitted **inside the run namespace** and are forbidden anywhere outside it. A provider that needs a child outside the run boundary fails closed.

Lifecycle ownership follows ADR 0002's split, with the namespace added: the platform lifecycle controller creates and attests the run namespace; the platform provisioning service creates the run workspace and its membership; TrustyAI reconciles the `SandboxedEvaluation`, which stays in the instance's adapter namespace exactly as ADR 0002's decision 3 assigns it; the EvalHub operator requests provisioning and creates the job's adapters, and holds no namespace authority. Run namespace and workspace names are derived from the immutable job identity, are DNS-1123-safe and bounded, and are recorded with their UIDs in provenance. Dispatch requires an attested run namespace and a ready run workspace; a withdrawn attestation stops new dispatch immediately, and neither the operator nor TrustyAI may create, label, configure or delete a run namespace.

### 4. Deploy the simulated internet inside the run boundary and nothing outside it

A class benchmark declares a **simulation profile** as part of its trusted onboarding metadata: the services the exercise needs (web, mail, DNS, database, CMS, file or code hosting, and any other declared endpoint), their addressing, and which of them the agent may reach. The profile is part of the qualified class contract and cannot be extended by a request.

Only two shapes are qualified, and each provider records which one it uses:

- **Every simulated service is an OpenShell sandbox in the run namespace.** One enforcement path, one identity model and one teardown path, at the cost of the driver's two-Pod (workload plus supervisor) footprint per service.
- **Simulated services run as declared in-namespace workloads behind a run-scoped addressing scheme.** Lower footprint, but those workloads are outside the driver's sandbox boundary, so they need their own admission, policy, identity and teardown, and they must be unreachable from outside the run namespace.

Connectivity inside the run is default-deny. The only allowed paths are the intra-run traffic the declared profile requires, the job's broker and relay endpoints, and the approved model, judge or attacker endpoints. There is no path from the run to the gateway API, the Agent Sandbox controller, the adapter namespace, the EvalHub API, the Kubernetes API, node or cloud metadata, another run namespace, the installation-wide sandbox namespace, or the platform's own services. The run's resolver answers only inside the run: external name resolution is either denied or served by the run's simulated DNS, and the profile records which, because unrestricted resolution is itself an exfiltration path. Gateway service routing stays disabled and no exposed service or Host-based route is created for a run.

The driver's workload-egress fence must not be weakened informally. Replacing it in a class namespace requires a supported mechanism or a qualified higher-precedence cluster control, and it re-opens ADR 0002's supervisor-egress acceptance gate on the supported CNI: API server, metadata, IPv4/IPv6, DNS, IP literals and redirects are re-tested, and an unqualified CNI or an unqualified profile fails closed. NetworkPolicy object creation is not proof of enforcement.

### 5. Require Kata for every benchmark in the class

Every class benchmark declares `profile: kata`, and the class contract must qualify a Kata `RuntimeClass`. Admission rejects a class benchmark without Kata, so `standard` is not a dispatchable profile for it, no collection or request can lower the requirement, and a missing or unqualified runtime means the provider is not enabled rather than degraded.

The guarantee is stated honestly, because it is narrower than it sounds: Kata isolates the sandbox workload's kernel, not the supervisor (which may run on the default runtime), not the gateway, not the platform control plane, and not simulated services that decision 4's second shape places outside the driver's boundary. A provider that uses that second shape must run those workloads on Kata too, or record with Security why its simulation is exempt. Kata reduces the workload-escape risk that these suites target; it does not make the run a virtual machine.

### 6. Treat every retained write surface as a covert channel

These suites reward leaving something behind. Every surface on which a run can write is therefore classified, and a later run must not be able to read anything the earlier one wrote except the tenant's authorized result. No Kubernetes API credential is present in a run, which removes object annotations, labels, ConfigMaps, Secrets and Events from reach; that is verified by negative test rather than assumed, and if a compatibility adapter ever exposes metadata writes, they are prohibited or must be proven deleted and unreadable before the next run.

The matrix is normative. It records, for each surface, whether the run can write it, whether it survives the run, and how it is handled:

| Surface | Survives the run? | Handling |
|---|---|---|
| Run namespace, its sandboxes, workloads, Services and NetworkPolicies | No | Deleted with the namespace and verified by UID absence (decision 7) |
| Run workspace, membership and ServiceAccount tokens | No | Deleted with the run; no membership survives teardown |
| Run templates and provider or credential bindings | No | Replicated into the run, never inherited from a tenant workspace, deleted with it |
| `/sandbox` PVCs and any run-scoped volume | No | `Delete` reclaim only, no snapshots, no retained volumes; reclamation verified |
| Artifacts and results accepted by the relay | Yes, as authorized output | Copied to the tenant's durable store as immutable output before teardown; the run-scoped copy is deleted |
| MLflow runs, OCI artifacts and callback records | Yes, as authorized output | Run-scoped, unique destination; read-only after the run; no later run may write to or read from it |
| Logs, events and platform telemetry | Yes, bounded | Redacted per ADR 0002's data-handling rules; never an input to another run |
| Evaluation provenance and gateway state, including the 24-hour `request_id` replay cache | Yes, bounded | Identifiers and metadata only; `request_id` is never reused, and it is scoped to a workspace that no longer exists |
| Annotations, labels, ConfigMaps, Secrets, Events | Not reachable | No API credential in the run; a negative test proves it |

On top of the matrix, three rules apply to the run boundary: every run-visible name, path and identifier is derived from the run's unique identity and carries a per-run prefix; the next run is given nothing that refers to a previous run; and a later run of the same benchmark must not be able to tell that an earlier run happened except through its authorized inputs. That last property is what the class's tests assert, and it is the reason a shared workspace, a shared namespace, a shared template set or a shared log stream cannot be used for the class.

### 7. Destroy the run completely, verify it is gone, and quarantine anything that will not go

A completed run leaves nothing behind in its boundary. Teardown extends ADR 0002's normative order rather than replacing it:

1. Freeze spawning and revoke the job's broker capability.
2. Export available results through the relay and obtain the verified export or a recorded authorized data-loss disposition. ADR 0002's export-before-delete rule still governs: destruction never precedes an accepted disposition.
3. Delete the job's adapter objects (broker, relay, their Services, ServiceAccounts, capability and token Secrets and NetworkPolicies) in the adapter namespace.
4. Delete the run's sandboxes with `Delete` (never `stop` or `suspend`, which preserve storage) and verify sandbox and PVC absence, including PV reclamation.
5. Delete the run workspace and its membership, templates and provider bindings.
6. Delete the run namespace and everything still in it, and verify absence by immutable namespace UID, not by API success. Namespace deletion is asynchronous and blocks on finalizers, so the check is that the UID no longer resolves.
7. Record `RunIsolationDestroyed` in provenance, with the namespace UID, the deletion time, the reclamation result and the export disposition.

Namespace deletion is the platform lifecycle controller's operation. Neither the EvalHub operator nor TrustyAI may hold namespace delete authority, as ADR 0002's operator gate already requires, and its finalizer must not be what a namespace deletion waits on: the `SandboxedEvaluation` and the job's adapters live in the adapter namespace, outside the namespace being destroyed, which is what makes step 6 safe.

A run whose export is blocked, whose deletion stalls or whose reclamation fails enters **quarantine**. Quarantine is not a normal state: the namespace is marked non-reusable, removed from the workspace allowlist so no future workspace can map into it, reported as a durable orphan record owned by the platform lifecycle controller, and counted against the capacity cap in decision 8. It never admits another run, it is never recorded as a successful cleanup, and it is resolved by authorized recovery or by an explicitly recorded data-loss disposition. An absolute run deadline plus a platform TTL is the backstop, and the TTL sweeper — not the operator, not TrustyAI — holds the namespace authority for the class. Both reconciliation paths stay idempotent against each other, and a timeout never becomes a cleanup assertion.

### 8. Bound the blast radius and the cost

Per-run isolation multiplies objects, so it is capped rather than left to demand: each run declares its full footprint — primary sandbox, every simulated service, supervisors, init peaks, adapters, storage, streams and depth — and a per-run `ResourceQuota` and `LimitRange` are derived from the declared simulation profile at namespace creation. Namespace quota alone is not fairness: an admission/reservation component records the reservation and admits the run before its namespace is created, on the same terms as ADR 0002's decision 6, and parallel creates cannot oversubscribe.

The class has no fleet-wide quota, because there is no fleet-wide namespace. The installation caps concurrent run namespaces and per-tenant run namespaces, caps the rate at which namespaces are created and deleted (SCC/UID allocation is a cluster-scoped operation), and counts quarantined namespaces against the cap so a stalled teardown cannot be hidden by creating more. Measured namespace create/attest/delete latency and cost per run are part of the capacity envelope and are recorded before provider coverage or a delivery date is committed.

### 9. Keep the class off by default and enable it only against a qualified class contract

Class execution is a disabled capability, not a default, on the same basis as ADR 0002's decision 8. `spec.sandbox` alone never enables it: the class has its own optional block that names the class contract, its gateway reference and its profile, and its absence or `enabled: false` leaves the instance with no class dispatch path at all. A benchmark in the class is then rejected at onboarding and at dispatch with `AdversarialIsolationUnavailable`; it is never routed to the shared sandbox namespace and never falls back to a Job.

`enabled: true` is necessary but not sufficient. The operator requires a platform-published class contract with `qualified: "true"`, a recorded `workspaceMode`, a Kata runtime class, a namespace controller, a simulation-profile revision, a non-empty namespace UID policy and versions on the supported matrix. Until `AdversarialGatewayReady` and `AdversarialRunIsolationReady` are both `True`, no run namespace is created and no class benchmark is dispatched. Only the platform writes `qualified` and only the platform can withdraw it; withdrawing it, or an upgrade that invalidates it, stops new dispatch without an operator restart, is visible in status and `/api/v1/info`, and leaves in-flight runs to be reconciled to a terminal state and destroyed. The operator never self-attests and never infers a usable class gateway from a reachable endpoint.

### 10. Do not extend per-run isolation to every agent evaluation

Per-run namespaces and workspaces are a property of this class, not of agent evaluations in general. Ordinary agent benchmarks — the live-agent rows in the plan's [Agent-target classification](../design/openshell-evaluation-plan.md#agent-target-classification) that are not in the class, such as GAIA, AgentDojo, The Agent Company and the Harbor / Terminal-Bench paths — stay on ADR 0002's shared sandbox namespace. The class is separated from them because of what it is scored to do, not because its agent is live: a trace-scoring benchmark and an attack benchmark are both agent evaluations, and only one of them is trying to leave.

Six reasons decided it, in the order that mattered.

1. **The namespace is not what contains a sandbox.** At the pin every sandbox is already a separate `Sandbox` resource with its own workload and supervisor Pods, and the Sandbox Protocol binds each connection to the exact sandbox and supervisor identities. A run namespace adds three things on top: a fresh SCC/UID/MCS range, a namespace-scoped policy boundary, and a single object whose deletion wipes everything. Ordinary agent jobs are contained without those; the class needs all three because it must reach a simulated internet and must not persist anything a later run can read.
2. **Automating it requires the least-ready gateway mode.** A namespace per job can only be created and destroyed automatically in `managed` mode, which PR 174 head records as not yet supportable (namespace templates, managed-mode RBAC partitioning, the managed-namespace ownership handshake) and makes the required fallback to `operator` when those gates fail. `operator` mode would need an allowlist entry, a race-free create, configure and delete, and a de-listing per job, at job rate. Generalising the model therefore puts every ordinary agent provider behind the least mature part of the platform.
3. **It moves cluster-scoped namespace authority into the path of every job.** ADR 0002's decision 4 avoids exactly that authority because it is cluster-scoped and a compromised gateway credential reaches every namespace. A class run justifies granting it; the whole agent fleet does not.
4. **It puts a slow cluster operation inside every job's teardown.** Namespace deletion is asynchronous, blocks on finalizers and scales with the objects in the namespace, while the plan's proposed targets are complete teardown within 60 seconds of terminal execution and `CleanupBlocked` after five minutes. Every ordinary job would wait on namespace deletion, and every retry would pay for namespace creation and SCC allocation again. The class accepts that because a class run should never be cheap to recreate.
5. **It removes the single quota and does not buy fairness.** OpenShell v0.1.10 exposes no quota or eviction RPC, and namespace quota is not fairness: ADR 0002's decision 6 delegates per-job and per-tenant fairness to a platform admission and reservation component that is not yet delivered. One namespace per job fragments the fleet quota and still needs that component, so it adds cost without addressing the problem it looks like it solves.
6. **It would mix policies on the class gateway.** The class gateway is qualified for mandatory Kata, a simulation profile and the destruction rules. Running ordinary agent benchmarks through it would either force Kata on jobs that do not need it — capacity and start-up latency paid for every GAIA or Terminal-Bench run — or put two policies on the gateway that carries the class, which weakens both its qualification and its blast-radius story.

The strongest alternative is a single **agent gateway** in `managed` mode serving every agent benchmark with a namespace per job, with the class adding only Kata, the simulation profile and the covert-channel rules. It is coherent, and if the platform records PR 174 outcome 3 at per-job granularity it becomes the better model: this decision's cost would then be paid once for all sandbox providers and the second namespace model would disappear. It is not chosen now because it pays that cost for every agent provider before managed mode is supportable, and because it gives up the isolation ADR 0002 already proves rather than adding to it.

This decision tracks the platform's outcome rather than standing independently of it. Revisit it when outcome 3 is recorded at per-job granularity; when managed mode's gates are implemented and qualified at the supported pin; when the admission and reservation component and namespace templates are delivered and measured against the teardown targets; or when the class's per-run model is in operation and its measured churn shows that one model would be cheaper than two. Until then a non-class agent benchmark is admitted to the shared sandbox namespace and never to the class gateway, and a class benchmark is admitted to the class gateway and never to the shared namespace.

## Relationship to ADR 0002

| ADR 0002 item | For this class |
|---|---|
| Decision 1 — sandbox execution, precedence, no unsandboxed fallback | Carries over. The shared gateway does not: the class uses its own gateway. Kata moves from optional to mandatory |
| Decision 2 — one long-lived workspace per tenant, retained across jobs | Replaced. One short-lived workspace per run, deleted with the run |
| Decision 3 — trusted per-job adapters outside evaluated code, in the instance adapter namespace | Carries over unchanged. The adapters stay in the adapter namespace, outside the run namespace |
| Decision 4 — one installation-wide sandbox namespace in `shared` mode | Replaced. One namespace per run, in the class gateway's recorded mode; the run namespace hosts only that run's sandboxes and declared simulation workloads |
| Decision 5 — tenant and credential boundaries | Carries over, with no canonical tenant workspace to fall back on. Credentials are still delivered per run and never mounted as originals |
| Decision 6 — capacity, bounded lifetime, storage deletion contract | Carries over, except the fleet-quota sentence. Per-run quota, a namespace-churn cap and a class-wide cap replace it |
| Decision 7 — cleanup, provenance, orphan records | Carries over and extends to workspace and namespace destruction, verification and quarantine |
| Decision 8 — off by default, contract-gated enablement | Carries over. The class adds its own contract, its own block and its own conditions |
| Gateway interaction contract, operator duties, `SandboxedEvaluation` surface | Carry over. The class contract adds the mode, the namespace controller, the Kata runtime and the simulation profile; the class block is added to the operator's rendered configuration |
| Platform alignment outcomes (three) | A fourth, class-scoped outcome is added there: this class runs on its own gateway at per-run granularity. It does not substitute for one of the three for the remaining providers |

## Alternatives considered

- **One agent gateway for every agent benchmark, with a namespace per job:** the strongest generalisation, and it is coherent. Rejected for now for the six reasons in decision 10, chiefly that it puts every ordinary agent provider behind `managed` mode's unsupported gates and behind a namespace deletion on each job's teardown path; it becomes the better model if PR 174 outcome 3 is recorded at per-job granularity.
- **ADR 0002's shared namespace with a stricter policy and job-scoped identity:** the cheapest option, and the one ADR 0002's open question names. Rejected for this class: it keeps the long-lived namespace, the tenant workspace, the shared template and binding set and the shared UID/MCS range, which are the persistence substrate the suites target; it gives no namespace deletion, no fresh SCC allocation and no per-run east-west network, so a simulated internet cannot be deployed and a later run can read what an earlier one wrote.
- **Per-run namespaces on the installation-wide shared gateway:** impossible at the pin. `workspace_mode` is gateway-wide, so it would change the model for every ordinary sandbox provider — the change ADR 0002's decision 4 declines.
- **`managed` mode only, with no separate contract or gateway:** the mechanism, not the model. It still needs a class gateway with a recorded mode, the class contract and the destruction rules; recording it as the only mode would remove the `operator` alternative the platform may prefer.
- **`operator` mode with the namespace lifecycle left to TrustyAI or the EvalHub operator:** rejected. ADR 0002's operator gate is explicit that the operator holds no namespace create/delete, and namespace lifecycle is platform scope. `operator` mode is acceptable only with a platform namespace controller.
- **Keep the simulation inside the run's own sandbox container rather than in separate workloads:** removes the east-west requirement, but needs a multi-container sandbox template that the pinned two-Pod (workload plus supervisor) footprint does not provide, and leaves the simulated services sharing the agent's kernel, which defeats the point of Kata for them. Not selected; revisit only if a qualified multi-container template lands.
- **Quarantine instead of destruction:** keeps a namespace on teardown failure for later inspection. Rejected as the normal path: a retained run boundary is the substrate this record exists to remove. Quarantine is allowed only as the failure state in decision 7, is non-reusable, and is capped.
- **Defer the class until the platform records a class outcome:** the current, safe default, and the outcome whenever the platform will not add a gateway. It leaves the strategy's adversarial requirements unmet, so it is the fallback rather than the decision.
- **Run the class without Kata:** rejected. The suites target sandbox escape, and Kata is the only workload-boundary upgrade the platform already qualifies. Standard-profile dispatch for the class is prohibited rather than merely discouraged.

## Open questions

Each question names an owner and what it blocks.

### Platform contract and ownership

- Who owns and attests the class gateway, and in which mode — `managed` (driver-owned namespace lifecycle) or `operator` (a namespace controller outside the driver)? Under ADR 0002's [EvalHub-owned gateway option](0002-evaluation-sandboxes-through-internal-openshell-gateway.md#deployment-option-an-evalhub-owned-gateway) it is EvalHub's own gateway, so this is a deployment decision with Security sign-off rather than a platform amendment; the recorded mode and the namespace-controller owner are still required. Owner: EvalHub deployment and Security. Blocking for every class benchmark.
- Which controller holds namespace create, label, attest and delete authority for run namespaces, and what are its HA, cleanup-SLO and version-skew targets? Owner: platform. Blocking for decisions 3 and 7.
- Who owns the workspace allowlist in `operator` mode, and how is the create/allowlist/delete ordering made race-free and audited? Owner: platform. Blocking for `operator` mode.
- Where is the class contract published, who may write it, and how is a change authorised? Owner: platform and Security. Blocking for decision 9.

### Classification and catalog

- Which benchmarks are members beyond `inspect/cybench` and `inspect/cybergym`, and who may add or remove one? Owner: EvalHub, Security and QE. Blocking for provider coverage.
- Which provider-schema field carries the class, and in which release, given that `BenchmarkResource` has no capability or risk field today? Owner: EvalHub. Blocking for dispatch logic and for the plan's Phase 2 schema extension.

### Isolation, simulation and network

- Which simulation-profile shape does Security accept — every service as a sandbox, or in-namespace workloads — and with what policy, identity and teardown evidence? Owner: Security and platform. Blocking for decision 4 per provider.
- How is the driver's workload-egress fence replaced inside a run namespace without weakening it elsewhere, and on which CNI is that qualified? Owner: platform and Security. Blocking for every class benchmark.
- Is DNS answered entirely by the run's simulated resolver, or denied with explicit answers? Owner: Security and EvalHub. Blocking for the simulation profile.
- Does Security accept "a later run cannot tell that an earlier run happened" as a testable acceptance property? Owner: Security and QE. Blocking for decision 6's tests.

### Capacity, storage and cost

- What are the class-wide and per-tenant caps on concurrent run namespaces, and what is the namespace-churn rate the cluster can sustain? Owner: platform scheduling. Blocking for capacity claims.
- What quarantine retention and recovery procedure applies to a blocked export or a stuck namespace deletion? Owner: platform storage and Security. Blocking for decision 7's failure path.
- What is the measured cost and latency of creating, attesting and destroying a run namespace before provider coverage is committed? Owner: EvalHub and QE. Blocking for delivery dates.

## Consequences

The class becomes dispatchable at all. Today these benchmarks can only be pointed at the shared sandbox namespace, which this record prohibits, so without it the honest state is that the class stays blocked. Per-run namespaces and workspaces bound a failure to one run: a compromise can influence its own run, and nothing a later run reads unless the platform's own export path carried it. A stalled teardown becomes visible as a quarantine and an orphan record instead of being absorbed by a shared namespace, and the class gets a runtime requirement — Kata — that matches the capability it is trying to contain.

The cost is a second control plane. The class gateway, its contract, its qualification matrix, its upgrade and rollback path and its identity issuer are separate from the shared gateway's, and the platform must operate both. Per-run namespaces bring back the churn ADR 0002 deliberately removed: SCC/UID allocation and a namespace lifecycle for every run, a first-sandbox readiness barrier in `managed` mode or an externally driven allowlist in `operator` mode, a quarantine state to operate, and a quota that fragments across runs instead of one fleet quota. A simulated internet multiplies the per-run footprint by the number of declared services, each of which is a workload or a two-Pod sandbox, so concurrency per unit of capacity falls sharply. A provider's coverage is limited to the simulation profiles and CNIs proven in Phase 0, and the class cannot borrow the shared gateway's qualification.

Residual risk remains and is recorded rather than removed. Kata does not isolate the supervisor, the class gateway or the platform control plane. Simulated services in decision 4's second shape sit outside the driver's sandbox boundary. Namespace deletion can stall, and until it completes the namespace exists in quarantine. The tenant's durable result store and MLflow remain shared, authorized, and by design a destination: a run can exfiltrate into its own results, which is a governance and data-handling question ADR 0002's data-handling requirements already own, not a containment failure this record can close. A compromised class gateway reaches every class run on it, and the class gateway's blast radius is therefore smaller than the shared gateway's only because its tenant set is smaller, not because its isolation is different.

Failure behavior follows from the decisions above.

| Component failure | Behavior | User-visible condition | Retryable |
|---|---|---|---|
| Class contract absent or unqualified | No class benchmark is admitted | `AdversarialIsolationUnavailable` at onboarding and dispatch | No, until the platform qualifies it |
| Run namespace cannot be created or attested | The run is not dispatched; no workspace is created | `AdversarialIsolationUnavailable`; phase stays `Provisioning` with a sanitized reason | Yes, with bounded backoff |
| Run workspace not ready | Dispatch fails closed for that run only | Class-scoped degraded condition | Yes, after provisioning recovers |
| Simulation profile or CNI unqualified | Provider not enabled | `AdversarialIsolationUnavailable` at preflight | No, until qualification changes |
| Kata runtime unavailable | No admission for the class | `IsolationProfileUnavailable` | Yes, after the runtime is qualified, never by lowering the profile |
| Export blocked at teardown | Destruction blocked; namespace quarantined | Visible export condition plus orphan record | No, until an authorized disposition is recorded |
| Namespace deletion or PV reclamation stalls | Namespace stays in quarantine, counted against the cap | Orphan or reclamation condition | Yes, with bounded backoff and authorized recovery |
| Class gateway or identity issuer outage | New class runs fail; ordinary evaluations and shared-path sandboxes continue | Class-scoped degraded condition | Yes, after readiness |

## Acceptance gates

Approval of this record does not assert implementation readiness. A class benchmark is enabled only after every gate that applies to it passes; a failed gate blocks the benchmark rather than lowering the requirement. Each gate names the owner that must evidence it.

### Platform contract and amendment

- [ ] ADR 0002 carries the class carve-out: its decision summary, its platform-alignment outcomes and its isolation gates state that decisions 2, 4, 6 and 7 do not apply to this class, and exactly one class-scoped outcome is recorded (owner: OpenShift AI platform and Security).
- [ ] A class gateway exists with a recorded `workspaceMode`, a recorded namespace controller, and a qualification and attestation separate from the shared gateway's (owner: platform).
- [ ] The named approvers for EvalHub, TrustyAI Service Operator, the platform and Security are recorded (owner: the four decider organizations).
- [ ] The class contract is published, versioned, platform-writable only, and names the mode, the Kata runtime class, the namespace and workspace naming rules and the simulation-profile revision (owner: platform and Security).
- [ ] The upstream managed-mode or `operator`-mode requirements that PR 174 head records for the chosen mode are mapped to the supported OpenShell release, and every unimplemented row carries an owner (owner: OpenShell integration and platform).

### Classification and dispatch

- [ ] The class is carried by provider and benchmark onboarding metadata, `inspect/cybench` and `inspect/cybergym` are classified, and a negative test shows that no request, collection or tenant value can classify, declassify or retarget a benchmark (owner: EvalHub and Security).
- [ ] A class benchmark with no qualified class contract is rejected at onboarding and at dispatch with `AdversarialIsolationUnavailable`, and is never dispatched to the shared sandbox namespace or a plain Job (owner: EvalHub and QE).
- [ ] Local mode rejects the class regardless of configuration (owner: EvalHub and QE).
- [ ] The provider-schema field that carries the class is recorded and released before any dispatch logic depends on it (owner: EvalHub).
- [ ] Tiering holds in both directions: a non-class agent benchmark is admitted to the shared sandbox namespace and never to the class gateway, and a class benchmark is admitted to the class gateway and never to the shared namespace (owner: EvalHub and QE).

### Run isolation and simulation

- [ ] A run namespace and a run workspace are created per evaluation job, with fresh SCC-assigned UID/MCS ranges, and nothing is reused across jobs, tenants, instances or benchmark repeats (owner: platform and Security).
- [ ] The run namespace holds only that run's sandboxes and its declared simulation workloads; a negative test shows no shared namespace, workspace, template set or binding set is visible to it (owner: platform and Security).
- [ ] The declared simulation profile is reachable inside the run and unreachable from outside it, and the reverse holds for every non-approved destination: gateway, controller, adapter namespace, EvalHub API, Kubernetes API, metadata, other runs and the shared sandbox namespace (owner: platform and EvalHub).
- [ ] The workload-egress fence replacement is qualified on the supported CNI, with API-server, metadata, IPv4/IPv6, DNS, IP-literal and redirect tests, without weakening the fence for any other namespace (owner: platform and Security).
- [ ] The run's resolver answers only inside the run, by denial or by the simulated DNS, and the recorded choice is enforced (owner: Security and platform).
- [ ] Kata is enforced at admission for every class benchmark, and an unqualified runtime blocks the provider instead of lowering the profile (owner: platform and EvalHub).

### Cross-run contamination

- [ ] The decision 6 matrix is exercised: every listed surface is written by a test run and a subsequent run demonstrates it cannot read it, including annotated, logged and artifact-planted payloads (owner: Security and QE).
- [ ] A negative test shows the run holds no Kubernetes API credential and can write no annotation, label, ConfigMap, Secret or Event (owner: EvalHub and Security).
- [ ] Results reach the tenant's durable store as immutable, run-scoped output, and no later run can write to or read from a previous run's MLflow run or OCI prefix (owner: EvalHub and Security).
- [ ] A later run of the same benchmark cannot tell that an earlier run happened except through its authorized inputs (owner: Security and QE).

### Lifecycle, destruction and storage

- [ ] Teardown follows the recorded order and ends with the run namespace deleted and verified absent by UID, its PVCs and PVs reclaimed, and `RunIsolationDestroyed` recorded with the export disposition (owner: platform and EvalHub).
- [ ] `stop` or `suspend` is never recorded as deletion, and a run whose storage is retained is quarantined rather than reported clean (owner: platform storage and EvalHub).
- [ ] Export or a recorded authorized data-loss disposition precedes every deletion, and a blocked export quarantines the namespace (owner: EvalHub and Security).
- [ ] Quarantine is operated end to end: the namespace is de-listed from the allowlist, counted against the cap, reported as an orphan record, and cannot admit a later workspace (owner: platform).
- [ ] Neither the EvalHub operator nor TrustyAI holds namespace create, label or delete authority, verified by RBAC review and a negative test (owner: EvalHub and Security).
- [ ] The operator finalizer and the platform lifecycle controller are exercised together for cancel, deadline, export-blocked, stuck-deletion and orphan cases without double deletion or a lost record (owner: EvalHub and TrustyAI Service Operator).

### Capacity and cost

- [ ] Per-run quota and `LimitRange` are derived from the declared simulation profile at namespace creation, and admission reserves the whole run footprint before the namespace exists (owner: platform scheduling).
- [ ] Class-wide and per-tenant caps on concurrent run namespaces, the namespace-churn rate and the quarantine accounting are enforced, and parallel creates cannot oversubscribe (owner: platform scheduling and EvalHub).
- [ ] Measured namespace create/attest/delete latency and cost per run are recorded before provider coverage or a delivery date is committed (owner: EvalHub and QE).

### Enablement and operator

- [ ] With the class block absent or disabled, rendering is indistinguishable from a build without the feature and no class benchmark is admitted (owner: EvalHub and QE).
- [ ] Every disabling state produces its named class condition and user-visible error, with no endpoint, mode, runtime, profile or version inferred from reachability (owner: EvalHub and platform).
- [ ] Attestation withdrawal and an upgrade that invalidates it stop new class dispatch without an operator restart, and in-flight runs are reconciled to a terminal state and destroyed (owner: platform and Security).

## References

- [ADR 0002 — Evaluation sandboxes through the internal OpenShell gateway](0002-evaluation-sandboxes-through-internal-openshell-gateway.md), which this record amends for the class defined here.
- [OpenShell evaluation plan](../design/openshell-evaluation-plan.md), including [Agent-target classification](../design/openshell-evaluation-plan.md#agent-target-classification), [Pinned OpenShell release and Go SDK](../design/openshell-evaluation-plan.md#pinned-openshell-release-and-go-sdk) and [Framework compatibility](../design/openshell-evaluation-plan.md#framework-compatibility).
- [PR 174 at `97a0c4d`](https://github.com/opendatahub-io/architecture-decision-records/commit/97a0c4d59b34e0f9d1e6327aa79f026178b73d60), `ODH-ADR-Operator-0021-internal-openshell-gateway.md`, for the shared-gateway baseline, the `managed`/`operator` fallback and the required upstream changes.
- [OpenShell v0.1.10 Kubernetes driver](https://github.com/ai-openshell/openshell/blob/v0.1.10/crates/openshell-driver-kubernetes/README.md), [workspace authorization](https://github.com/ai-openshell/openshell/blob/v0.1.10/crates/openshell-server/src/auth/workspace_authz.rs) and [API schema](https://github.com/ai-openshell/openshell/blob/v0.1.10/proto/openshell.proto), for the workspace modes, the namespace-wide NetworkPolicy, the supervisor egress behaviour, the two-Pod footprint, the `request_id` replay window and the stop-versus-delete contract.
- [OpenShell v0.1.10 release](https://github.com/ai-openshell/openshell/releases/tag/v0.1.10) and the [Go SDK](https://github.com/ai-openshell/sdk-go), pinned together with the gateway.
- [EH-0002 multi-tenancy and authz](https://github.com/opendatahub-io/architecture-decision-records/blob/main/architecture-decision-records/eval-hub/ODH-ADR-EH-0002-multi-tenancy-and-authz.md) and [EH-0004 eval sidecar credential injection](https://github.com/opendatahub-io/architecture-decision-records/blob/main/architecture-decision-records/eval-hub/ODH-ADR-EH-0004-eval-sidecar-creds-injection.md), for the tenant and credential boundaries this record does not change.
- [`config/providers/inspect.yaml`](../../config/providers/inspect.yaml), for `inspect/cybench` and `inspect/cybergym`.
- The class's evaluation suites and the containment-incident reports named in the Context are cited by name only. This record deliberately does not restate unverified third-party incident details, and their primary sources must be linked when the record is reviewed.
