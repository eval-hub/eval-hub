# Development tips for MCP

## Creating a MCP service using the evalhub CR

This example shows the minimal config required to enable MCP in the evalhub CR:

```yaml
spec:
  mcp:
    enabled: true
    replicas: 1
```

Once the `evalhub` instance is created the pods should appear in the namespace, something like this:

```shell
NAME                           READY   STATUS    RESTARTS   AGE
evalhub-89f665dff-wk8d6        2/2     Running   0          17m
evalhub-mcp-78b9dff58b-njlcd   2/2     Running   0          17m
```

Note that there are 2 containers because each pod is running its own `kube-rbac-proxy`.

## Authentication

There are two separate authentication layers:

| Layer | What it protects | Configuration |
|-------|------------------|---------------|
| **Inbound (client → evalhub-mcp)** | Who may call the MCP server over HTTP | `auth_type` (see below) |
| **Outbound (evalhub-mcp → eval-hub API)** | Access to providers, jobs, etc. | `EVALHUB_TOKEN`, `EVALHUB_TENANT`, `EVALHUB_BASE_URL` |

Inbound auth applies to **HTTP transports only** (`http`, `http-sse`). The **stdio** transport has no HTTP headers; configure outbound credentials via environment variables on the MCP process instead.

The `/health` endpoint is always unauthenticated.

### `auth_type` values

Set `auth_type` in the evalhub-mcp config file or with environment variable `EVALHUB_AUTH_TYPE`:

| Value | Use case | Client requirement |
|-------|----------|-------------------|
| `none` (default) | Local development, open HTTP listener | No MCP-level auth |
| `rbac-proxy` | OpenShift deployment behind kube-rbac-proxy | `Authorization: Bearer <token>` to the proxy; proxy forwards `X-User` and `X-Tenant` |
| `oidc` | Standalone HTTP deployment with an external IdP | `Authorization: Bearer <JWT>` validated against OIDC issuer metadata |

### OpenShift: `rbac-proxy`

On cluster, `kube-rbac-proxy` sits in front of `evalhub-mcp` and validates the caller's bearer token. The operator should configure evalhub-mcp with:

```yaml
auth_type: rbac-proxy
```

`evalhub-mcp` then requires these headers on every MCP request (injected by kube-rbac-proxy after successful token review):

- `X-Tenant` — tenant namespace
- `X-User` — authenticated user identity

The `evalhub-mcp` process itself does not validate the bearer token; the sidecar does that before the request arrives.

### Standalone: `oidc`

For HTTP deployments without `kube-rbac-proxy`, set `auth_type: oidc` and configure OIDC issuer discovery:

```yaml
auth_type: oidc
transport: http
host: 0.0.0.0
port: 3001
oidc:
  issuer_url: https://auth.example.com/realms/evalhub # required
  audience: evalhub-mcp                               # optional; expected JWT aud claim
  scopes:                                             # optional; all listed scopes must be present in the token
    - read
base_url: https://eval-hub.example.com
tenant: my-tenant
token: <service-account-or-user-token-for-eval-hub-api>
```

Clients send a JWT access token (or OIDC ID token from the same issuer) on every MCP HTTP request:

```http
Authorization: Bearer eyJhbGciOi...
```

Validation uses [go-oidc](https://github.com/coreos/go-oidc) against the issuer's `.well-known/openid-configuration` and JWKS. Invalid, expired, or wrongly scoped tokens receive **401** / **403** with a plain-text body.

Set `insecure: true` only when the IdP uses a self-signed TLS certificate (development).

### Outbound eval-hub API credentials

Regardless of `auth_type`, `evalhub-mcp` needs credentials to call the eval-hub REST API when `base_url` is configured:

```bash
export EVALHUB_BASE_URL="https://<evalhub-api-host>"
export EVALHUB_TOKEN="<your-token>"
export EVALHUB_TENANT="<your-tenant>"
```

For stdio transport (Cursor, VS Code, Claude Code), pass these in the MCP client's `env` block rather than in HTTP headers.

### Configuration reference

YAML keys and environment variables (env overrides YAML):

| Setting | YAML key | Environment variable |
|---------|----------|----------------------|
| Auth mode | `auth_type` | `EVALHUB_AUTH_TYPE` |
| OIDC issuer | `oidc.issuer_url` | `EVALHUB_OIDC_ISSUER_URL` |
| OIDC audience | `oidc.audience` | `EVALHUB_OIDC_AUDIENCE` |
| Eval-hub API URL | `base_url` | `EVALHUB_BASE_URL` |
| Eval-hub token | `token` | `EVALHUB_TOKEN` |
| Eval-hub tenant | `tenant` | `EVALHUB_TENANT` |
| Transport | `transport` | `EVALHUB_TRANSPORT` |
| HTTP host / port | `host`, `port` | `EVALHUB_HOST`, `EVALHUB_PORT` |

Load a config file with `--config /path/to/config.yaml` or `~/.evalhub/config.yaml`.

## Testing that the MCP service is functioning

### OpenShift (kube-rbac-proxy)

1. Set up a port forward to the MCP service:

   ```shell
   oc port-forward svc/evalhub-mcp 8443:8443
   ```

2. Run the MCP inspector:

   ```shell
   export NODE_TLS_REJECT_UNAUTHORIZED=0
   npx @modelcontextprotocol/inspector
   ```

3. In the UI enter `https://127.0.0.1:8443/sse` as the URL (legacy SSE) or the service root for Streamable HTTP, and in the **Authentication** section add a bearer token from `oc whoami -t`.

   Export `NODE_TLS_REJECT_UNAUTHORIZED` to avoid errors related to self-signed certificates.

### Standalone OIDC

1. Start evalhub-mcp with an OIDC config (see above) and `--transport http`.

2. Obtain a JWT from your IdP (access token with the configured audience and scopes).

3. Connect with the MCP inspector or any Streamable HTTP client, setting **Authentication** to `Bearer <token>`.

### Local development (no inbound auth)

```shell
make build-mcp
EVALHUB_BASE_URL=http://localhost:8080 EVALHUB_TOKEN=token EVALHUB_TENANT=tenant \
  ./bin/evalhub-mcp --transport http --host localhost --port 3001
```

Default `auth_type` is `none`; no bearer token is required to reach the MCP endpoint.
