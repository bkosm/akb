# AKB Prompt Authoring Reference

Prompts in AKB are MCP prompts that users invoke as slash commands. Each `.prompt.md`
file inside a KB mount is automatically discovered, registered, and kept in sync via a
file watcher — no server restart needed.

---

## Naming

A prompt's MCP name is derived from its path inside the KB:

```
<kb-name>/<relative-path-without-.prompt.md-suffix>
```

Examples:

| File path in KB            | MCP prompt name              |
|----------------------------|------------------------------|
| `code-review.prompt.md`    | `my-kb/code-review`          |
| `git/commit-msg.prompt.md` | `my-kb/git/commit-msg`       |

Dot-prefixed files (e.g. `.draft.prompt.md`) are ignored by the watcher.

---

## File format

A `.prompt.md` file has two parts:

1. **Optional YAML frontmatter** between `---` delimiters.
2. **Markdown body** — the prompt content, rendered as a Go text/template.

Minimal example (no frontmatter — body becomes a single user message):

```markdown
Summarise the key points from the text the user provides.
```

Full frontmatter schema:

```yaml
---
description: One-line description shown in the prompt list
arguments:
  - name: language      # referenced in the body as {{.language}}
    required: true
    description: The programming language to review
  - name: focus
    required: false
    description: Optional area to focus on
---
```

---

## Single-message vs multi-message

**Single-message** (default): body has no role headers — the entire body becomes one
`user` message after template rendering.

**Multi-message**: insert markdown headings that start with `@<role>` to split the body
into multiple messages. Each heading owns the text that follows it until the next role
heading. Any heading level works (`#`, `##`, `###`, …).

Supported roles: any word — `system`, `user`, `assistant`, etc.

```markdown
---
description: Code reviewer with a system persona
arguments:
  - name: language
    required: true
    description: Programming language
---

# @system

You are an expert {{.language}} code reviewer. Be concise and constructive.

## @user

Please review the code I am about to share.
```

---

## Template syntax

Bodies are rendered with Go `text/template` (`missingkey=zero` — missing arguments
expand to an empty string rather than an error).

| Construct                             | Effect                                      |
|---------------------------------------|---------------------------------------------|
| `{{.argname}}`                        | Substitute the argument value               |
| `{{if .argname}}…{{end}}`             | Conditional block (omit when empty)         |
| `{{if .argname}}…{{else}}…{{end}}`    | Conditional with fallback                   |
| `{{include "relative/path"}}`         | Inline content of another file (see below)  |

Example using conditionals:

```markdown
---
description: Explain code
arguments:
  - name: language
    required: true
    description: Programming language
  - name: audience
    required: false
    description: Target audience (e.g. junior, senior)
---

Explain the following {{.language}} code clearly.{{if .audience}} Tailor the explanation for a {{.audience}} developer.{{end}}
```

---

## The include function

`{{include "relative/path"}}` reads a file at a path relative to the prompt file's
directory and inlines its raw content before template rendering.

Use it to share fragments across multiple prompts:

```
prompts/
  _persona.md           ← shared fragment
  review.prompt.md      ← uses {{include "_persona.md"}}
  refactor.prompt.md    ← uses {{include "_persona.md"}}
```

> **Security note:** `include` is not sandboxed — a `../` traversal can read any file
> accessible to the server process. KB prompt files are treated as trusted content.

---

## Complete example

File: `my-kb/git/commit-msg.prompt.md`

```markdown
---
description: Generate a conventional commit message
arguments:
  - name: scope
    required: false
    description: Commit scope (e.g. auth, api)
---

# @system

You are an expert at writing clear, concise git commit messages following the
Conventional Commits specification.

## @user

Generate a commit message for the diff I will provide.{{if .scope}} The scope is "{{.scope}}".{{end}}
```

This prompt registers as `my-kb/git/commit-msg` and accepts an optional `scope` argument.
