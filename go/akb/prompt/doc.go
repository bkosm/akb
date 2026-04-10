// Package prompt handles discovery, parsing, rendering, and MCP registration
// of .prompt.md files found in knowledge base mount directories.
//
// A .prompt.md file consists of optional YAML frontmatter (description,
// arguments) followed by a Go text/template body. Templates support argument
// substitution, conditionals, and file inclusion via the {{include}} function.
//
// RegisterForKB discovers existing prompt files at startup and starts an
// fsnotify watcher so that new, modified, or deleted files are reflected in the
// MCP prompt list in real time without a server restart.
package prompt
