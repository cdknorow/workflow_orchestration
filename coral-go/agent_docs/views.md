# Views API

Custom views allow users to create additional dashboard tabs with custom HTML content and system prompts. Views are stored in the SQLite database.

## List Views

```
GET /api/views
```

Returns all custom views as a JSON array.

**Response:**
```json
[
  {
    "id": 1,
    "name": "Metrics Dashboard",
    "prompt": "You are a metrics assistant...",
    "html": "<div>...</div>",
    "tab_order": 0,
    "scope": "global"
  }
]
```

Returns `[]` when no views exist.

## Get View

```
GET /api/views/{id}
```

Returns a single view by ID.

**Parameters:**
| Name | In | Description |
|------|-----|-------------|
| `id` | path | Numeric view ID |

**Response:**
```json
{
  "id": 1,
  "name": "Metrics Dashboard",
  "prompt": "You are a metrics assistant...",
  "html": "<div>...</div>",
  "tab_order": 0,
  "scope": "global"
}
```

**Errors:**
- `400` — Invalid ID (not a number)
- `404` — View not found

## Create View

```
POST /api/views
```

Creates a new custom view. Returns HTTP 201 on success.

**Request Body:**
```json
{
  "name": "Metrics Dashboard",
  "prompt": "You are a metrics assistant...",
  "html": "<div>Custom view content</div>",
  "tab_order": 0,
  "scope": "global"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Display name for the view tab |
| `prompt` | string | no | System prompt for AI interactions within this view |
| `html` | string | no | Custom HTML content for the view |
| `tab_order` | int | no | Sort order for the tab (default: 0) |
| `scope` | string | no | Scope of the view: `"global"` (default) |

**Response (201):**
```json
{
  "id": 1,
  "name": "Metrics Dashboard",
  "prompt": "You are a metrics assistant...",
  "html": "<div>Custom view content</div>",
  "tab_order": 0,
  "scope": "global"
}
```

**Errors:**
- `400` — Invalid JSON or missing `name`

## Update View

```
PUT /api/views/{id}
```

Updates an existing view. All fields are replaced.

**Parameters:**
| Name | In | Description |
|------|-----|-------------|
| `id` | path | Numeric view ID |

**Request Body:** Same structure as Create.

**Response:**
```json
{
  "id": 1,
  "name": "Updated Dashboard",
  "prompt": "Updated prompt...",
  "html": "<div>Updated content</div>",
  "tab_order": 1,
  "scope": "global"
}
```

**Errors:**
- `400` — Invalid ID or invalid JSON

## Delete View

```
DELETE /api/views/{id}
```

Deletes a view.

**Parameters:**
| Name | In | Description |
|------|-----|-------------|
| `id` | path | Numeric view ID |

**Response:**
```json
{
  "ok": "true"
}
```

**Errors:**
- `400` — Invalid ID
