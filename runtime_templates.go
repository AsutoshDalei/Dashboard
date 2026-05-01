package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

func resolveRuntimeFilePath(envVar, defaultPath string) (string, error) {
	path := os.Getenv(envVar)
	if path == "" {
		path = defaultPath
	}

	candidates := []string{path}
	if !filepath.IsAbs(path) {
		candidates = append(candidates, filepath.Join("..", path))
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("%s not found. checked paths: %v", envVar, candidates)
}

func renderRuntimeTemplate(envVar, defaultPath string, data any) (string, error) {
	templatePath, err := resolveRuntimeFilePath(envVar, defaultPath)
	if err != nil {
		return "", err
	}

	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", templatePath, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render template %s: %w", templatePath, err)
	}

	return buf.String(), nil
}
