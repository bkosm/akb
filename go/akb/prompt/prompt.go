package prompt

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Definition is a parsed .prompt.md file.
type Definition struct {
	Name        string
	Description string
	Arguments   []Argument
	Messages    []Message
	SourcePath  string
}

type Argument struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty"`
}

type Message struct {
	Role    string
	Content string
}

type frontmatter struct {
	Description string     `yaml:"description"`
	Arguments   []Argument `yaml:"arguments"`
}

var roleHeaderRe = regexp.MustCompile(`(?m)^#{1,6}\s+@(\w+)\s*$`)

// ParseFile reads a .prompt.md file and returns a Definition.
// Name and SourcePath are not set by this function — the caller should fill them.
func ParseFile(path string) (Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, fmt.Errorf("read prompt file %q: %w", path, err)
	}
	return ParseBytes(data)
}

// ParseBytes parses .prompt.md content from raw bytes.
func ParseBytes(data []byte) (Definition, error) {
	fm, body, err := splitFrontmatter(string(data))
	if err != nil {
		return Definition{}, err
	}

	messages := splitMessages(body)

	return Definition{
		Description: fm.Description,
		Arguments:   fm.Arguments,
		Messages:    messages,
	}, nil
}

func splitFrontmatter(content string) (frontmatter, string, error) {
	var fm frontmatter

	trimmed := strings.TrimLeft(content, " \t\r\n")
	if !strings.HasPrefix(trimmed, "---") {
		return fm, content, nil
	}

	// Find end of frontmatter after the opening ---
	afterOpener := trimmed[3:]
	// Skip the rest of the opening --- line
	nlIdx := strings.IndexByte(afterOpener, '\n')
	if nlIdx < 0 {
		return fm, content, nil
	}
	afterOpener = afterOpener[nlIdx+1:]

	closeIdx := strings.Index(afterOpener, "---")
	if closeIdx < 0 {
		return fm, content, nil
	}

	yamlBlock := afterOpener[:closeIdx]
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return fm, "", fmt.Errorf("parse frontmatter: %w", err)
	}

	rest := afterOpener[closeIdx+3:]
	// Skip the rest of the closing --- line
	if nlIdx := strings.IndexByte(rest, '\n'); nlIdx >= 0 {
		rest = rest[nlIdx+1:]
	}

	return fm, rest, nil
}

// splitMessages splits the body into messages by @role headers.
// If no @role headers are found, the entire body is a single user message.
func splitMessages(body string) []Message {
	locs := roleHeaderRe.FindAllStringSubmatchIndex(body, -1)
	if len(locs) == 0 {
		text := strings.TrimSpace(body)
		if text == "" {
			return nil
		}
		return []Message{{Role: "user", Content: text}}
	}

	var msgs []Message
	for i, loc := range locs {
		role := body[loc[2]:loc[3]]

		contentStart := loc[1]
		var contentEnd int
		if i+1 < len(locs) {
			contentEnd = locs[i+1][0]
		} else {
			contentEnd = len(body)
		}

		text := strings.TrimSpace(body[contentStart:contentEnd])
		if text == "" {
			continue
		}
		msgs = append(msgs, Message{Role: role, Content: text})
	}
	return msgs
}
