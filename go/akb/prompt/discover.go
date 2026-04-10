package prompt

import (
	"io/fs"
	"path/filepath"
	"strings"
)

const PromptSuffix = ".prompt.md"

// Discover walks dir recursively and returns a Definition for every *.prompt.md file.
// Each Definition's Name is the relative path from dir with the .prompt.md suffix stripped.
func Discover(dir string) ([]Definition, error) {
	var defs []Definition
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), PromptSuffix) {
			return nil
		}

		def, parseErr := ParseFile(path)
		if parseErr != nil {
			return nil // skip unparseable files
		}

		rel, _ := filepath.Rel(dir, path)
		def.Name = strings.TrimSuffix(rel, PromptSuffix)
		def.Name = filepath.ToSlash(def.Name)
		def.SourcePath = path
		defs = append(defs, def)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return defs, nil
}
