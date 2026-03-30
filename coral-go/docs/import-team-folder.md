# Import Team from Folder

Create a team of agents by organizing markdown files in a folder. Each file defines one agent with a name, description, and system prompt.

## Folder Structure

```
my-team/
  SKILL.md              # Orchestrator (required)
  agents/
    code-reviewer.md    # Worker agent
    test-writer.md      # Worker agent
    security-auditor.md # Worker agent
```

- **SKILL.md** at the root is the orchestrator agent
- **agents/*.md** are worker agents (one file per agent)
- The folder name (`my-team`) becomes the team/board name
- Only `.md` files are processed; subdirectories inside `agents/` are ignored

## File Format

Each `.md` file uses YAML frontmatter followed by the prompt body:

```markdown
---
name: Code Reviewer
description: Reviews pull requests for bugs and style issues
---

You are a code reviewer. When asked to review code, check for:
- Logic errors and edge cases
- Security vulnerabilities
- Code style and readability
- Missing error handling

Provide specific, actionable feedback with line references.
```

### Frontmatter Fields

| Field         | Required | Default                                |
|---------------|----------|----------------------------------------|
| `name`        | No       | Filename without `.md`, dashes to spaces (e.g. `code-reviewer.md` becomes "code reviewer"). For SKILL.md, defaults to "Orchestrator" |
| `description` | No       | Empty                                  |

### Prompt Body

Everything after the closing `---` becomes the agent's system prompt. Board coordination instructions are automatically appended when the team launches.

If the file has no frontmatter (no `---` block), the entire file content is used as the prompt.

## Example

### `my-dev-team/SKILL.md`

```markdown
---
name: Tech Lead
description: Coordinates the development team
---

You are a tech lead coordinating a small development team. Break down tasks,
assign work to team members, and review their output. Post status updates
to the message board so everyone stays aligned.
```

### `my-dev-team/agents/frontend-dev.md`

```markdown
---
name: Frontend Developer
description: Builds UI components and pages
---

You are a frontend developer. Build UI components, style pages, and ensure
a great user experience. Wait for instructions from the Tech Lead before
starting work.
```

### `my-dev-team/agents/backend-dev.md`

```markdown
---
name: Backend Developer
description: Builds APIs and services
---

You are a backend developer. Build APIs, database models, and server-side
logic. Wait for instructions from the Tech Lead before starting work.
```

## How to Import

1. Open Coral and click **New Team**
2. Click **Import from Folder**
3. Select the team folder
4. Review the parsed agents and adjust if needed
5. Click **Launch Team**

The import works from both the browser file picker (client-side parsing) and the server API (`POST /api/teams/import` with `{"path": "/absolute/path/to/folder"}`).
