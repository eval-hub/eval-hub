# MLflow Workspaces REST API (`/api/3.0/mlflow/workspaces`)

> **Note:** These endpoints are part of the MLflow 3.x API and use the `/api/3.0/` prefix
> rather than the classic `/api/2.0/` prefix used by experiments, runs, etc.
> As of MLflow 3.16.0, the workspace endpoints do **not** appear in the
> [official REST API endpoint documentation](https://mlflow.org/docs/latest/api_reference/rest-api.html)
> (the page only lists them as Data Structure message types, not as REST endpoints
> with HTTP methods and paths). The endpoint paths and methods below are derived
> from the MLflow source code and the eval-hub client implementation.

---

## Feature Detection

### Get Server Info

| Endpoint                      | HTTP Method |
| ----------------------------- | ----------- |
| `3.0/mlflow/server-info`     | `GET`       |

Reports whether the MLflow server supports workspace-scoped APIs.
Returns `404 Not Found` on MLflow releases that pre-date workspace support.

#### Server Info Response

| Field Name           | Type | Description                                           |
| -------------------- | ---- | ----------------------------------------------------- |
| `workspaces_enabled` | BOOL | Whether workspace-scoped APIs are available on this server. |

---

## Workspace CRUD

All workspace endpoints require content type `application/json` for request bodies.

When workspaces are enabled, most other MLflow API calls accept an
`X-MLFLOW-WORKSPACE` header (type `STRING`) to scope the operation to a
specific workspace.

---

### Create Workspace

| Endpoint                       | HTTP Method |
| ------------------------------ | ----------- |
| `3.0/mlflow/workspaces`       | `POST`      |

Create a new workspace. This is a **global** operation — the `X-MLFLOW-WORKSPACE`
header is **not** sent with this request.

#### Create Workspace Request

| Field Name             | Type                                            | Description                                                        |
| ---------------------- | ----------------------------------------------- | ------------------------------------------------------------------ |
| `name`                 | STRING                                          | Workspace name to create. **Required.**                            |
| `description`          | STRING                                          | Optional workspace description.                                    |
| `default_artifact_root`| STRING                                          | Optional default artifact root override to apply at creation time. |
| `trace_archival_config`| [TraceArchivalConfig](#tracearchivalconfig)      | Optional trace archival settings to apply at creation time.        |

#### Create Workspace Response

| Field Name  | Type                        | Description                                |
| ----------- | --------------------------- | ------------------------------------------ |
| `workspace` | [Workspace](#workspace)     | Metadata describing the created workspace. |

#### Create Workspace Errors

| Error Code                | HTTP Status | Description                                        |
| ------------------------- | ----------- | -------------------------------------------------- |
| `RESOURCE_ALREADY_EXISTS` | 400         | A workspace with the given name already exists.    |

---

### Get Workspace

| Endpoint                                    | HTTP Method |
| ------------------------------------------- | ----------- |
| `3.0/mlflow/workspaces/{workspace_name}`   | `GET`       |

Retrieve metadata for a single workspace.

#### Get Workspace Path Parameters

| Parameter        | Type   | Description                                             |
| ---------------- | ------ | ------------------------------------------------------- |
| `workspace_name` | STRING | Name of the workspace to fetch. **Required.**           |

#### Get Workspace Response

| Field Name  | Type                        | Description                                  |
| ----------- | --------------------------- | -------------------------------------------- |
| `workspace` | [Workspace](#workspace)     | Metadata describing the requested workspace. |

#### Get Workspace Errors

| Error Code                  | HTTP Status | Description                             |
| --------------------------- | ----------- | --------------------------------------- |
| `RESOURCE_DOES_NOT_EXIST`   | 404         | No workspace with the given name exists.|

---

### Update Workspace

| Endpoint                                    | HTTP Method |
| ------------------------------------------- | ----------- |
| `3.0/mlflow/workspaces/{workspace_name}`   | `PATCH`     |

Update workspace metadata.

#### Update Workspace Path Parameters

| Parameter        | Type   | Description                                              |
| ---------------- | ------ | -------------------------------------------------------- |
| `workspace_name` | STRING | Name of the workspace to update. **Required.**           |

#### Update Workspace Request

| Field Name             | Type                                            | Description                                     |
| ---------------------- | ----------------------------------------------- | ----------------------------------------------- |
| `description`          | STRING                                          | Optional description update.                    |
| `default_artifact_root`| STRING                                          | Optional default artifact root override update. |
| `trace_archival_config`| [TraceArchivalConfig](#tracearchivalconfig)      | Optional trace archival settings update.        |

#### Update Workspace Response

| Field Name  | Type                        | Description                                |
| ----------- | --------------------------- | ------------------------------------------ |
| `workspace` | [Workspace](#workspace)     | Metadata describing the updated workspace. |

---

### Delete Workspace

| Endpoint                                    | HTTP Method |
| ------------------------------------------- | ----------- |
| `3.0/mlflow/workspaces/{workspace_name}`   | `DELETE`    |

Delete a workspace. By default the server uses **RESTRICT** semantics: the
request fails if the workspace contains any experiments, runs, or other
resources. Callers can override this by passing a `deletion_mode` field in
the request body:

- **`RESTRICT`** (default) — reject deletion when the workspace is non-empty.
- **`CASCADE`** — delete the workspace and all resources it contains.
- **`SET_DEFAULT`** — move all resources to the `default` workspace before
  deleting.

The reserved `default` workspace cannot be deleted regardless of the
deletion mode.

#### Delete Workspace Path Parameters

| Parameter        | Type   | Description                                              |
| ---------------- | ------ | -------------------------------------------------------- |
| `workspace_name` | STRING | Name of the workspace to delete. **Required.**           |

#### Delete Workspace Request

| Field Name      | Type   | Description                                                                                         |
| --------------- | ------ | --------------------------------------------------------------------------------------------------- |
| `deletion_mode` | STRING | Optional. One of `RESTRICT` (default), `CASCADE`, or `SET_DEFAULT`. Controls non-empty workspace handling. |

---

### List Workspaces

| Endpoint                       | HTTP Method |
| ------------------------------ | ----------- |
| `3.0/mlflow/workspaces`       | `GET`       |

List accessible workspaces. When authentication and workspaces are both
enabled, the response contains only the workspaces that the authenticated
caller is permitted to access.

#### List Workspaces Response

| Field Name   | Type                                | Description                      |
| ------------ | ----------------------------------- | -------------------------------- |
| `workspaces` | An array of [Workspace](#workspace) | Collection of workspace records. |

---

## Workspace-Scoped Requests

When workspaces are enabled on the MLflow server, most standard API calls
(experiments, runs, artifacts, traces, etc.) accept the following header to
scope the operation:

| Header Name          | Type            | Description                                              |
| -------------------- | --------------- | -------------------------------------------------------- |
| `X-MLFLOW-WORKSPACE` | STRING or NULL | Workspace name to scope the request to. When the header is omitted or set to NULL, no workspace header is sent and the server falls back to its configured default workspace (if one exists). When no default workspace is configured on the server, callers must provide an explicit workspace name. |

---

## Data Structures

### Workspace

Workspace metadata returned by workspace APIs.

| Field Name             | Type                                            | Description                                                 |
| ---------------------- | ----------------------------------------------- | ----------------------------------------------------------- |
| `name`                 | STRING                                          | The unique workspace name. **Required.**                    |
| `description`          | STRING                                          | Optional workspace description.                             |
| `default_artifact_root`| STRING                                          | Optional default artifact root override for this workspace. |
| `trace_archival_config`| [TraceArchivalConfig](#tracearchivalconfig)      | Optional trace archival settings for this workspace.        |

### TraceArchivalConfig

Trace archival settings accepted by workspace APIs and returned in workspace metadata.

| Field Name  | Type   | Description                                                                 |
| ----------- | ------ | --------------------------------------------------------------------------- |
| `location`  | STRING | Optional archival repository root override.                                 |
| `retention` | STRING | Optional archival retention override. Format: `<int><unit>`, e.g. `30d`.   |

---

## Usage in eval-hub

The eval-hub MLflow client (`pkg/mlflowclient/workspaces.go`) uses these endpoints as follows:

1. **`ProbeWorkspacesEnabled()`** — calls `GET /api/3.0/mlflow/server-info` to check
   whether the connected MLflow server supports workspaces. Returns `false` for
   older servers that respond with `404`.

2. **`GetWorkspace(name)`** — calls `GET /api/3.0/mlflow/workspaces/{name}` to
   retrieve a single workspace.

3. **`CreateWorkspace(req)`** — calls `POST /api/3.0/mlflow/workspaces` (without the
   `X-MLFLOW-WORKSPACE` header) to create a new workspace.

4. **`EnsureWorkspace()`** — idempotent helper that creates the client's active
   workspace if it does not already exist. Skips creation for the reserved
   `default` workspace. Handles the `RESOURCE_ALREADY_EXISTS` race condition
   from concurrent creators.
