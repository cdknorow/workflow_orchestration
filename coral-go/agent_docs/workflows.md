# Workflows API

Define reusable multi-step automations, trigger them on demand, and inspect step-by-step results.

Workflows are higher-level than one-shot jobs: each workflow stores a reusable definition with ordered `shell` and `agent` steps, optional default agent settings, and its own execution history.

---

## Workflow object

Workflow responses use this shape:

```json
{
  "id": 12,
  "name": "nightly-release",
  "description": "Build, test, and prepare release notes",
  "repo_path": "/home/user/project",
  "base_branch": "main",
  "max_duration_s": 3600,
  "cleanup_worktree": 1,
  "enabled": 1,
  "created_at": "2025-03-11T10:00:00+00:00",
  "updated_at": "2025-03-11T10:00:00+00:00",
  "steps": [
    {
      "name": "test",
      "type": "shell",
      "command": "go test ./..."
    },
    {
      "name": "summarize",
      "type": "agent",
      "prompt": "Summarize failures and likely fixes."
    }
  ],
  "default_agent": {
    "agent_type": "claude"
  },
  "step_count": 2
}
```

List responses also include `last_run` when available.

---

## List workflows

```
GET /api/workflows
```

### Response

```json
{
  "workflows": [
    {
      "id": 12,
      "name": "nightly-release",
      "description": "Build, test, and prepare release notes",
      "repo_path": "/home/user/project",
      "base_branch": "main",
      "max_duration_s": 3600,
      "cleanup_worktree": 1,
      "enabled": 1,
      "created_at": "2025-03-11T10:00:00+00:00",
      "updated_at": "2025-03-11T10:00:00+00:00",
      "steps": [{ "name": "test", "type": "shell", "command": "go test ./..." }],
      "default_agent": { "agent_type": "claude" },
      "step_count": 1,
      "last_run": {
        "id": 44,
        "status": "completed",
        "trigger_type": "api",
        "started_at": "2025-03-12T03:00:01+00:00",
        "finished_at": "2025-03-12T03:02:30+00:00"
      }
    }
  ]
}
```

---

## Get a workflow

```
GET /api/workflows/{workflowID}
GET /api/workflows/by-name/{name}
```

Returns one workflow definition.

Returns `{"error": "workflow not found"}, 404` if there is no matching workflow.

---

## Create a workflow

```
POST /api/workflows
```

### Request body

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | **required** | Unique name. Allowed characters: letters, numbers, `_`, `-`. |
| `description` | string | `""` | Optional description. |
| `steps` | array | **required** | Ordered list of 1 to 20 steps. |
| `default_agent` | object | `null` | Default agent config used by agent steps. |
| `repo_path` | string | `""` | Optional repo path. Must exist if provided. |
| `base_branch` | string | `"main"` | Base branch for runs. |
| `max_duration_s` | int | `3600` | Max run duration, 1 to 86400 seconds. |
| `cleanup_worktree` | int | `1` | Remove the worktree after the run finishes. |
| `enabled` | int | `1` | `1` to allow triggers, `0` to disable. |

### Step rules

- Every step needs a unique `name`.
- `type` must be `shell` or `agent`.
- `shell` steps require `command`.
- `agent` steps require `prompt`.
- `agent` steps also need an `agent_type`, either in the step-level `agent` object or inherited from `default_agent`.
- `timeout_s`, when set, must be between `1` and `86400`.

### Example

```bash
curl -X POST http://localhost:8420/api/workflows \
  -H "Content-Type: application/json" \
  -d '{
    "name": "nightly-release",
    "description": "Build, test, and prepare release notes",
    "repo_path": "/home/user/project",
    "steps": [
      {
        "name": "test",
        "type": "shell",
        "command": "go test ./..."
      },
      {
        "name": "summarize",
        "type": "agent",
        "prompt": "Summarize failures and likely fixes."
      }
    ],
    "default_agent": {
      "agent_type": "claude"
    }
  }'
```

### Response

Returns the created workflow object with hydrated `steps`, `default_agent`, and `step_count`.

### Errors

| Status | Body | Cause |
|---|---|---|
| 400 | `{"error": "invalid JSON"}` | Malformed request body. |
| 400 | `{"error": "name is required"}` | Missing name. |
| 400 | `{"error": "name contains invalid characters"}` | Name includes unsupported characters. |
| 400 | `{"error": "at least one step is required"}` | `steps` is empty. |
| 400 | `{"error": "maximum 20 steps allowed"}` | Too many steps. |
| 400 | `{"error": "repo_path does not exist"}` | Repo path is invalid. |
| 400 | `{"error": "invalid max_duration"}` | `max_duration_s` is out of range. |
| 400 | `{"error": "duplicate step name"}` | Two steps share the same name. |
| 400 | `{"error": "shell step missing command"}` | Shell step omitted `command`. |
| 400 | `{"error": "agent step missing prompt"}` | Agent step omitted `prompt`. |
| 400 | `{"error": "agent step missing agent_type"}` | No effective `agent_type` was provided. |
| 409 | `{"error": "workflow name already exists"}` | Name collision. |

---

## Update a workflow

```
PUT /api/workflows/{workflowID}
```

Updates only the fields you include. When updating `steps`, Coral re-validates the full step list using the existing `default_agent` unless you also send a new one.

### Example

```bash
curl -X PUT http://localhost:8420/api/workflows/12 \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": 0,
    "description": "Paused pending infra changes"
  }'
```

### Response

Returns the updated workflow object.

Returns `{"error": "workflow not found"}, 404` if the workflow ID does not exist.

---

## Delete a workflow

```
DELETE /api/workflows/{workflowID}
```

Deletes the workflow and its run history.

### Response

```json
{"ok": true}
```

---

## Trigger a workflow

```
POST /api/workflows/{workflowID}/trigger
POST /api/workflows/by-name/{name}/trigger
```

The request body is optional.

### Request body

| Field | Type | Default | Description |
|---|---|---|---|
| `trigger_type` | string | `"api"` | Stored on the run record. |
| `context` | any JSON | `null` | Arbitrary JSON stored as `trigger_context`. |

### Example

```bash
curl -X POST http://localhost:8420/api/workflows/by-name/nightly-release/trigger \
  -H "Content-Type: application/json" \
  -d '{
    "trigger_type": "manual",
    "context": {
      "requested_by": "release-bot",
      "ticket": "REL-42"
    }
  }'
```

### Response

```json
{
  "run_id": 44,
  "workflow_id": 12,
  "workflow_name": "nightly-release",
  "status": "pending",
  "trigger_type": "manual",
  "created_at": "2025-03-12T03:00:00+00:00"
}
```

If the workflow is disabled, returns `{"error": "workflow is disabled"}, 409`.

---

## List runs for one workflow

```
GET /api/workflows/{workflowID}/runs?limit=20&offset=0&status=running
```

### Parameters

| Parameter | Type | Default | Description |
|---|---|---|---|
| `limit` | int | `20` | Max results to return. |
| `offset` | int | `0` | Pagination offset. |
| `status` | string | `null` | Optional status filter. |

### Response

```json
{
  "runs": [
    {
      "id": 44,
      "workflow_id": 12,
      "trigger_type": "manual",
      "trigger_context": "{\"requested_by\":\"release-bot\"}",
      "status": "completed",
      "current_step": 2,
      "worktree_path": "/Users/user/.coral/workflows/runs/44/worktree",
      "started_at": "2025-03-12T03:00:01+00:00",
      "finished_at": "2025-03-12T03:02:30+00:00",
      "error_msg": null,
      "created_at": "2025-03-12T03:00:00+00:00",
      "steps": [
        { "name": "test", "status": "completed" },
        { "name": "summarize", "status": "completed" }
      ]
    }
  ]
}
```

---

## List recent workflow runs

```
GET /api/workflows/runs/recent?limit=20&offset=0&status=failed
```

Returns recent runs across all workflows.

Each run also includes `workflow_name`.

---

## Get one workflow run

```
GET /api/workflows/runs/{runID}
```

Returns the full run record plus parsed `steps` data.

Returns `{"error": "run not found"}, 404` if the run ID does not exist.

---

## Kill a workflow run

```
POST /api/workflows/runs/{runID}/kill
```

Kills a run when its status is `pending` or `running`.

### Response

```json
{
  "ok": true,
  "run_id": 44,
  "status": "killed"
}
```

### Errors

| Status | Body | Cause |
|---|---|---|
| 400 | `{"error": "invalid run ID"}` | Bad path parameter. |
| 404 | `{"error": "run not found"}` | Unknown run ID. |
| 409 | `{"error": "run is not active"}` | Run is already terminal. |

---

## Download a step file

```
GET /api/workflows/runs/{runID}/steps/{stepIndex}/files/*
```

Serves a file from the run artifact directory for one step.

### Example

```bash
curl http://localhost:8420/api/workflows/runs/44/steps/0/files/test-report.txt
```

### Errors

| Status | Body | Cause |
|---|---|---|
| 400 | `{"error": "invalid run ID"}` | Bad run ID. |
| 400 | `{"error": "invalid step index"}` | Bad step index. |
| 400 | `{"error": "file path required"}` | Missing wildcard path. |
| 400 | `{"error": "invalid file path"}` | Path traversal attempt or invalid path. |
| 404 | `{"error": "run not found"}` | Run does not exist. |
| 404 | `{"error": "workflow not found"}` | Parent workflow was removed. |
| 404 | `{"error": "file not found"}` | Artifact file is absent. |
