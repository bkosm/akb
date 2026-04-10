---
description: A hello-world example showing frontmatter and template syntax
arguments:
  - name: language
    required: true
    description: The programming language to greet in
  - name: focus
    required: false
    description: Optional area to focus on
---

Say hello to the world in {{.language}}.{{if .focus}} Pay special attention to: {{.focus}}.{{end}}
