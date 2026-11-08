// Package promptfile loads prompt templates from disk.
package promptfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agile-consultant/internal/usecase"
)

// Load reads a prompt template from path, parses its role sections and
// validates it. The template name is the file's base name without the .txt
// extension.
func Load(path string) (usecase.PromptTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return usecase.PromptTemplate{}, fmt.Errorf("read prompt %s: %w", path, err)
	}
	name := strings.TrimSuffix(filepath.Base(path), ".txt")
	tmpl, err := usecase.ParsePromptTemplate(name, string(data))
	if err != nil {
		return usecase.PromptTemplate{}, fmt.Errorf("load prompt %s: %w", path, err)
	}
	return tmpl, nil
}
