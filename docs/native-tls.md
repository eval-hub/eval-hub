# Native TLS contract (API and MCP)

EvalHub supports two distinct topologies:

- **Managed/proxy terminated:** kube-rbac-proxy owns public TLS, authentication and
  authorization; the API/MCP backend uses private HTTP. Leave native serving
  certificates and `TLS_*` controls unset. HTTP does not itself violate TLS-profile
  adherence. Keep backend reachability restricted to trusted callers.
- **Native HTTPS:** API and either MCP HTTP transport use the same validated
  `internal/servingtls` implementation. Supply a certificate/key and generic controls
  below. This is supported in standalone or otherwise explicitly configured
  deployments, but TLS alone does **not** implement caller authentication.

The API in cluster mode and MCP with `auth_type: rbac-proxy` trust forwarded identity
headers. Do not expose those listeners directly to untrusted clients merely because
TLS is enabled: retain a trusted authenticating proxy/access boundary. MCP `none`
authentication is intentionally unauthenticated. These controls do not add mTLS.

## Serving material and generic controls

Existing certificate sources are unchanged:

| Listener | Certificate/key configuration |
|---|---|
| API | `service.tls_cert_file` / `service.tls_key_file`; stock `config/config.yaml` maps `TLS_CERT_FILE` / `TLS_KEY_FILE` to these fields. |
| MCP HTTP or legacy HTTP+SSE | `tls_cert_file` / `tls_key_file` in MCP YAML, `EVALHUB_TLS_CERT_FILE` / `EVALHUB_TLS_KEY_FILE`, or `--tls-cert-file` / `--tls-key-file` (existing precedence applies). |

Both binaries read these **process environment** controls independently of YAML/Viper:

| Variable | Meaning | Default |
|---|---|---|
| `TLS_REQUIRED` | Boolean; require native HTTPS and refuse absent serving material. | `false` |
| `TLS_MIN_VERSION` | Exactly `1.2` or `1.3`. | `1.2` |
| `TLS_CIPHERS` | Comma-separated Go/IANA names from `tls.CipherSuites()`, excluding TLS 1.3 suites; configures **TLS 1.2 only**. | Go defaults |
| `TLS_CURVES` | Comma-separated supported named groups (below). | Go defaults |

Groups: `P256`, `P384`, `P521`, `X25519`, `X25519MLKEM768`,
`SecP256r1MLKEM768`, `SecP384r1MLKEM1024` (using the repository's pinned Go runtime).
List membership restricts allowed groups; Go controls preference order. Hybrid-only
lists require `TLS_MIN_VERSION=1.3` because they cannot serve TLS 1.2.

Unknown versions/names, duplicate or explicitly empty lists, invalid booleans, partial
certificate pairs, unreadable/malformed/mismatched material fail **before opening the
API/MCP listener**. No self-signed or plaintext fallback occurs for invalid native TLS.
Setting any version/cipher/group control without certificates is also an error even
when `TLS_REQUIRED=false`, preventing silently ignored policy settings. Unset controls
and no certificates retain intentional HTTP. A supplied certificate pair is loaded
and checked even with `TLS_REQUIRED=false`.

Explicit ALPN is `h2`, `http/1.1`; existing `net/http` protocol behavior applies.
The serving pair is preloaded at startup. Certificate/config changes require process
restart; no hot-reload watcher is claimed.

### Examples

Native API, with stock API configuration loaded:

```sh
TLS_CERT_FILE=/certs/tls.crt TLS_KEY_FILE=/certs/tls.key \
TLS_REQUIRED=true TLS_MIN_VERSION=1.3 TLS_CURVES=X25519MLKEM768,P256 \
./bin/eval-hub
```

Native MCP HTTP:

```sh
TLS_REQUIRED=true TLS_MIN_VERSION=1.2 \
TLS_CIPHERS=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 TLS_CURVES=P256,P384 \
./bin/evalhub-mcp --transport http --tls-cert-file /certs/tls.crt --tls-key-file /certs/tls.key
```

Choose suites compatible with the certificate key type (RSA in the second example).
A valid configuration is not evidence that an arbitrary peer supports those algorithms.
MCP stdio has no TLS listener; these serving controls apply only to HTTP transports.

## Platform integration and representability

This upstream package has **no OpenShift API reader, adherence resolver, watcher or
platform dependency**. Deployment tooling must resolve effective profile/adherence,
convert to supported generic inputs for **both** native listeners, and restart them
when effective settings change. Legacy/no-opinion behavior remains the deployer's
explicit decision, not a guessed process-wide cluster policy. Operator propagation
is separate work (planned operator work package 7); current managed HTTP remains valid.

Go does not expose TLS 1.3 cipher-suite selection through `tls.Config.CipherSuites`.
TLS 1.3 suite names are rejected in `TLS_CIPHERS`; specifying TLS 1.2 suites together
with a TLS-1.3-only minimum is also rejected as ineffective. Do not claim arbitrary
TLS 1.3 suite filtering or group preference ordering. Profiles requiring settings
outside this contract must use a qualified capable TLS terminator or fail deployment,
not silently discard constraints. OpenSSL aliases and numeric group IDs are not
accepted; the deployer must translate recognized inputs or reject them.

Go runtime FIPS settings can further restrict algorithms. These controls do not
activate FIPS mode or certify algorithm/module compliance. Test the shipped runtime,
certificate type and actual negotiation; local Go tests do not qualify an image.

## Runtime sidecar and outbound verification

The evaluation-job runtime sidecar is an **HTTP** reverse proxy; its former server
`TLSConfig` never enabled TLS and has been removed. It currently binds all pod
interfaces. Keep job-pod ingress private using the deployment/network boundary; no
loopback-only or UDS guarantee is introduced here. Dedicated metrics remain unchanged
HTTP as well; neither endpoint is automatically required to become HTTPS by this work.

Outbound HTTPS is a separate client concern. EvalHub, Model, MLflow and OCI client
constructors verify certificates by default and append configured private CAs to
system roots. Outside FIPS, existing explicit EvalHub/Model verification bypasses
remain local/test escape hatches, not production recommendations. Under host FIPS or
Go `crypto/fips140` mode those clients reject bypass configuration at construction.
MLflow/OCI do not offer a bypass. The former API **server** check of
`InsecureSkipVerify` was unreachable and did not enforce any outbound trust boundary;
it is removed in favor of the actual client-path enforcement and tests.

## Verification boundary

Unit tests exercise invalid inputs, optional HTTP, required TLS, serving-material
preflight, and both API/MCP startup rejection paths. Real local TLS handshakes verify
TLS 1.2 cipher/group selection and TLS 1.3 hybrid negotiation, ALPN and rejection of
disallowed versions/groups. Real client-constructor HTTPS tests verify trusted versus
untrusted CA handling; a FIPS-enabled subprocess verifies bypass rejection.

These are not target-container, OpenShift profile-transition, service-ca rotation,
Prometheus-scrape or live-cluster acceptance tests.
