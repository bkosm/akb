package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBytes_singleUserMessage(t *testing.T) {
	t.Parallel()
	input := []byte("Hello, this is a simple prompt.")
	def, err := ParseBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(def.Messages))
	}
	if def.Messages[0].Role != "user" {
		t.Fatalf("Role = %q, want user", def.Messages[0].Role)
	}
	if def.Messages[0].Content != "Hello, this is a simple prompt." {
		t.Fatalf("Content = %q", def.Messages[0].Content)
	}
}

func TestParseBytes_frontmatter(t *testing.T) {
	t.Parallel()
	input := []byte(`---
description: Review code
arguments:
  - name: language
    required: true
    description: The programming language
  - name: focus
---

Review the {{.language}} code.`)

	def, err := ParseBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	if def.Description != "Review code" {
		t.Fatalf("Description = %q", def.Description)
	}
	if len(def.Arguments) != 2 {
		t.Fatalf("len(Arguments) = %d, want 2", len(def.Arguments))
	}
	if def.Arguments[0].Name != "language" || !def.Arguments[0].Required {
		t.Fatalf("Arg[0] = %+v", def.Arguments[0])
	}
	if def.Arguments[1].Name != "focus" || def.Arguments[1].Required {
		t.Fatalf("Arg[1] = %+v", def.Arguments[1])
	}
	if len(def.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(def.Messages))
	}
	if def.Messages[0].Role != "user" {
		t.Fatalf("Role = %q", def.Messages[0].Role)
	}
}

func TestParseBytes_multiMessage(t *testing.T) {
	t.Parallel()
	input := []byte(`---
description: Multi-turn
---

### @system

You are a concise writer.

### @user

Summarize: {{.content}}

### @assistant

Here is my summary.`)

	def, err := ParseBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(def.Messages))
	}
	roles := []string{"system", "user", "assistant"}
	for i, want := range roles {
		if def.Messages[i].Role != want {
			t.Fatalf("Messages[%d].Role = %q, want %q", i, def.Messages[i].Role, want)
		}
	}
	if def.Messages[0].Content != "You are a concise writer." {
		t.Fatalf("Messages[0].Content = %q", def.Messages[0].Content)
	}
}

func TestParseBytes_differentHeadingLevels(t *testing.T) {
	t.Parallel()
	input := []byte(`# @system

System msg.

###### @user

User msg.`)

	def, err := ParseBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(def.Messages))
	}
	if def.Messages[0].Role != "system" {
		t.Fatalf("Messages[0].Role = %q", def.Messages[0].Role)
	}
	if def.Messages[1].Role != "user" {
		t.Fatalf("Messages[1].Role = %q", def.Messages[1].Role)
	}
}

func TestParseBytes_textBeforeFirstRoleDiscarded(t *testing.T) {
	t.Parallel()
	input := []byte(`Some preamble text.

## @user

Actual message.`)

	def, err := ParseBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(def.Messages))
	}
	if def.Messages[0].Content != "Actual message." {
		t.Fatalf("Content = %q", def.Messages[0].Content)
	}
}

func TestParseBytes_emptyBody(t *testing.T) {
	t.Parallel()
	input := []byte(`---
description: Empty
---
`)
	def, err := ParseBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Messages) != 0 {
		t.Fatalf("len(Messages) = %d, want 0", len(def.Messages))
	}
}

func TestParseBytes_noFrontmatter(t *testing.T) {
	t.Parallel()
	input := []byte(`Just a simple prompt.`)
	def, err := ParseBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	if def.Description != "" {
		t.Fatalf("Description = %q, want empty", def.Description)
	}
	if len(def.Messages) != 1 {
		t.Fatalf("len(Messages) = %d", len(def.Messages))
	}
}

func TestParseBytes_realHeadersNotTreatedAsRoles(t *testing.T) {
	t.Parallel()
	input := []byte(`## @user

## Overview

This is a heading in the prompt content.

Some more text.`)

	def, err := ParseBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(def.Messages))
	}
	if def.Messages[0].Role != "user" {
		t.Fatalf("Role = %q", def.Messages[0].Role)
	}
	want := "## Overview\n\nThis is a heading in the prompt content.\n\nSome more text."
	if def.Messages[0].Content != want {
		t.Fatalf("Content = %q, want %q", def.Messages[0].Content, want)
	}
}

func TestParseFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.prompt.md")
	if err := os.WriteFile(path, []byte("Hello from file."), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Messages) != 1 {
		t.Fatalf("len(Messages) = %d", len(def.Messages))
	}
	if def.Messages[0].Content != "Hello from file." {
		t.Fatalf("Content = %q", def.Messages[0].Content)
	}
}

func TestParseFile_notFound(t *testing.T) {
	t.Parallel()
	_, err := ParseFile("/nonexistent/path.prompt.md")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
