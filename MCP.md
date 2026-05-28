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

## Testing that the MCP service is functioning

1. Set up a port forward to the MCP service

   ```shell
   oc port-forward svc/evalhub-mcp 8443:8443
   ```

2. Run the MCP inspector

   ```shell
   export NODE_TLS_REJECT_UNAUTHORIZED=0
   npx @modelcontextprotocol/inspector
   ```

In the `UI` enter `https://127.0.0.1:8443/sse` as the `URL` and add in the `Authentication` section a bearer token that was obtained by running `oc whoami -t`.

Note that we export `NODE_TLS_REJECT_UNAUTHORIZED` to avoid errors related to self-signed certificates.
