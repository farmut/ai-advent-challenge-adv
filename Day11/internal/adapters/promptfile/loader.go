// Package promptfile loads prompt templates from disk.
package promptfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agile-consultant/internal/usecase"
)

// Load reads a prompt template from path and validates it. The template name
// is the file's base name without the .txt extension.
func Load(path string) (usecase.PromptTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return usecase.PromptTemplate{}, fmt.Errorf("read prompt %s: %w", path, err)
	}
	tmpl := usecase.PromptTemplate{
		Name: strings.TrimSuffix(filepath.Base(path), ".txt"),
		Text: string(data),
	}
	if err := tmpl.Validate(); err != nil {
		return usecase.PromptTemplate{}, fmt.Errorf("load prompt %s: %w", path, err)
	}
	return tmpl, nil
}

// Resolve returns the template at path, or the built-in legacy template when
// path is empty.
func Resolve(path string) (usecase.PromptTemplate, error) {
	if path == "" {
		return usecase.LegacyTemplate(), nil
	}
	return Load(path)
}
