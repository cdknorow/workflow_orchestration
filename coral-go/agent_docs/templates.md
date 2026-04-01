# Templates API

Coral proxies the [davila7/claude-code-templates](https://github.com/davila7/claude-code-templates) GitHub repository to provide agent and command templates. Responses are cached in memory for 1 hour.

Templates are Markdown files with YAML frontmatter containing metadata (name, description, tools, model, etc.) and a body with the template content.

## Agent Templates

### List Agent Categories

```
GET /api/templates/agents
```

Lists top-level agent template directories from GitHub.

**Response:**
```json
{
  "categories": [
    { "name": "coding", "type": "dir" },
    { "name": "devops", "type": "dir" }
  ]
}
```

### List Agents in Category

```
GET /api/templates/agents/{category}
```

Lists agent template files (`.md`) in a specific category.

**Parameters:**
| Name | In | Description |
|------|-----|-------------|
| `category` | path | Category directory name |

**Response:**
```json
{
  "agents": [
    { "name": "code-reviewer", "filename": "code-reviewer.md" }
  ],
  "category": "coding"
}
```

### Get Agent Template

```
GET /api/templates/agents/{category}/{name}
```

Returns a specific agent template with parsed YAML frontmatter and markdown body.

**Parameters:**
| Name | In | Description |
|------|-----|-------------|
| `category` | path | Category directory name |
| `name` | path | Template name (with or without `.md` extension) |

**Response:**
```json
{
  "name": "Code Reviewer",
  "description": "Reviews code for bugs and style issues",
  "tools": "bash,grep,read",
  "model": "haiku",
  "body": "You are a code reviewer...",
  "category": "coding"
}
```

## Command Templates

### List Command Categories

```
GET /api/templates/commands
```

Lists top-level command template directories.

**Response:**
```json
{
  "categories": [
    { "name": "git", "type": "dir" },
    { "name": "testing", "type": "dir" }
  ]
}
```

### List Commands in Category

```
GET /api/templates/commands/{category}
```

Lists command template files in a category.

**Parameters:**
| Name | In | Description |
|------|-----|-------------|
| `category` | path | Category directory name |

**Response:**
```json
{
  "commands": [
    { "name": "commit-message", "filename": "commit-message.md" }
  ],
  "category": "git"
}
```

### Get Command Template

```
GET /api/templates/commands/{category}/{name}
```

Returns a specific command template with parsed frontmatter and body.

**Parameters:**
| Name | In | Description |
|------|-----|-------------|
| `category` | path | Category directory name |
| `name` | path | Template name (with or without `.md` extension) |

**Response:**
```json
{
  "name": "Commit Message",
  "description": "Generates a commit message from staged changes",
  "allowed_tools": "bash,grep",
  "argument_hint": "arg1 arg2",
  "body": "Generate a commit message...",
  "category": "git"
}
```

## Error Handling

All endpoints return HTTP 200 even on GitHub API failures, with an `error` field in the response:

```json
{
  "error": "GitHub API returned 403",
  "categories": []
}
```

This graceful degradation means the UI can always render (with an empty list) rather than showing a hard error.
