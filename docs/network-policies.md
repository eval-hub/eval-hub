# EvalHub evaluation Job NetworkPolicy

Kubernetes-backed evaluations receive a namespaced NetworkPolicy in the namespace where EvalHub creates the Job (typically a configured tenant namespace). EvalHub creates the policy before submitting the Job and uses a server-generated execution ID on the Job and Pod template to select only that execution. The policy is owned by the Job after creation so Kubernetes garbage collection can remove it with the Job and its TTL lifecycle. This policy does not establish or validate tenant-membership authorization; namespace-selection authority is a separate boundary.

## Ingress

The policy explicitly selects the Ingress direction and has no ingress allow rules. This denies peer-Pod network ingress to the evaluation Pod. The runtime sidecar's HTTP listener is used by containers in the same Pod (and its health probe); it is not a peer-Pod service, so no network-policy ingress exception is needed for that traffic.

## Egress is not restricted by this policy

This initial policy is ingress-only. It does not select Egress or define egress rules. Therefore it does **not** provide egress isolation or exfiltration protection. The Pod's effective egress remains governed by other selecting NetworkPolicies and platform controls; absent those controls, Kubernetes' default is to allow egress. NetworkPolicy permissions are additive, so evaluate the complete policy/control union.

Evaluation Jobs can need egress to configured model and judge endpoints, the EvalHub callback, S3, Hugging Face, Git, optional MLflow and OCI endpoints, and optional telemetry collectors. Data-fetch work may run in init containers; all containers in the Pod share the same NetworkPolicy boundary. Standard Kubernetes NetworkPolicy cannot select external destinations by hostname. The egress profile is intentionally unresolved: this policy does not silently choose either unrestricted egress or a potentially incompatible static endpoint allowlist. No claim of the platform's explicit-egress requirement being met is made by this ingress-only policy.

## Creation and lifecycle

- The operator grants the EvalHub Job-creation ServiceAccount permissions through RoleBindings in the namespaces it provisions for that EvalHub instance. The policy writer is EvalHub; the operator supplies only the delegated RBAC. The delegated NetworkPolicy verbs are create, get, and update; no NetworkPolicy delete permission is needed. These bindings do not by themselves prove that namespace membership or cross-namespace submission is securely authorized; tenant-label authority must be reviewed separately.
- Policy creation must succeed before Job creation. A policy API error prevents a new Job from being submitted. If Job creation is confirmed absent after an error, EvalHub cleans up the policy and related resources; if the outcome cannot be established, it preserves the policy rather than risk leaving a possibly created Job unprotected.
- At creation, the policy has a same-namespace owner reference to the already-created job-spec ConfigMap. Once the Job is created, EvalHub changes the policy owner to that Job. If Job creation is confirmed absent, deleting the ConfigMap lets Kubernetes garbage-collect the policy without granting NetworkPolicy delete. If the Job owner-reference update fails, the policy remains owned by the ConfigMap; the ConfigMap is then owned by the Job on the normal path, preserving garbage-collection lifecycle. The normal TTL/deletion path relies on Kubernetes garbage collection.
- API object ordering is not proof of when a CNI has programmed or enforced a policy. Runtime qualification must use allowed and denied traffic controls on the supported CNI, and record any delay or policy union.
